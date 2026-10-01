package apiclient

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// postmanFormat reads and writes Postman's v2.0/v2.1 collection files and its
// environment and globals exports. Postman keeps those in different files, so
// a collection exports as one file and an environment as another.
type postmanFormat struct{}

const postmanSchema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

func (postmanFormat) Info() FormatInfo {
	return FormatInfo{
		ID: "postman", Label: "Postman (v2.1)", Extension: "json",
		Exports: []ExportKind{ExportCollection, ExportEnvironment, ExportGlobals},
	}
}

// ---- reading ---------------------------------------------------------

type pmKV struct {
	Key      string `json:"key"`
	Value    any    `json:"value"`
	Type     string `json:"type,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Src      any    `json:"src,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

func (kv pmKV) text() string {
	switch v := kv.Value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}

// off reads both ways a row says it is switched off: `disabled` in a
// collection, `enabled: false` in an environment.
func (kv pmKV) off() bool { return kv.Disabled || (kv.Enabled != nil && !*kv.Enabled) }

type pmAuth struct {
	Type   string `json:"type"`
	Bearer []pmKV `json:"bearer,omitempty"`
	Basic  []pmKV `json:"basic,omitempty"`
	APIKey []pmKV `json:"apikey,omitempty"`
}

type pmURL struct {
	Raw      string   `json:"raw"`
	Protocol string   `json:"protocol,omitempty"`
	Host     []string `json:"host,omitempty"`
	Port     string   `json:"port,omitempty"`
	Path     []string `json:"path,omitempty"`
	Query    []pmKV   `json:"query,omitempty"`
	Variable []pmKV   `json:"variable,omitempty"`
}

type pmRawOptions struct {
	Raw struct {
		Language string `json:"language"`
	} `json:"raw"`
}

type pmBody struct {
	Mode       string        `json:"mode"`
	Raw        string        `json:"raw,omitempty"`
	URLEncoded []pmKV        `json:"urlencoded,omitempty"`
	FormData   []pmKV        `json:"formdata,omitempty"`
	Options    *pmRawOptions `json:"options,omitempty"`
}

type pmRequest struct {
	Method string          `json:"method"`
	Header json.RawMessage `json:"header"`
	URL    json.RawMessage `json:"url"`
	Body   *pmBody         `json:"body"`
	Auth   *pmAuth         `json:"auth"`
}

type pmItem struct {
	Name     string          `json:"name"`
	Item     []pmItem        `json:"item"`
	Request  json.RawMessage `json:"request"`
	Auth     *pmAuth         `json:"auth"`
	Event    []any           `json:"event"`
	Variable []pmKV          `json:"variable"`
}

type pmCollection struct {
	Info struct {
		Name      string `json:"name"`
		Schema    string `json:"schema"`
		PostmanID string `json:"_postman_id"`
	} `json:"info"`
	Item     []pmItem `json:"item"`
	Auth     *pmAuth  `json:"auth"`
	Event    []any    `json:"event"`
	Variable []pmKV   `json:"variable"`
}

type pmEnvironment struct {
	Name   string `json:"name"`
	Values []pmKV `json:"values"`
	Scope  string `json:"_postman_variable_scope"`
}

// pmProbe is just enough to tell the two kinds of Postman file apart.
type pmProbe struct {
	Info *struct {
		Schema    string `json:"schema"`
		PostmanID string `json:"_postman_id"`
	} `json:"info"`
	Item   json.RawMessage `json:"item"`
	Values json.RawMessage `json:"values"`
	Scope  string          `json:"_postman_variable_scope"`
}

func probePostman(data []byte) (collection, environment bool) {
	var probe pmProbe
	if json.Unmarshal(bytes.TrimSpace(data), &probe) != nil {
		return false, false
	}
	if probe.Info != nil && probe.Item != nil &&
		(strings.Contains(probe.Info.Schema, "getpostman.com") || probe.Info.PostmanID != "") {
		return true, false
	}
	if probe.Values != nil && (probe.Scope == "environment" || probe.Scope == "globals" || probe.Scope == "") {
		return false, probe.Info == nil && probe.Item == nil
	}
	return false, false
}

func (postmanFormat) Recognises(data []byte) bool {
	collection, environment := probePostman(data)
	return collection || environment
}

func (postmanFormat) Decode(data []byte, report *Report) (Bundle, error) {
	collection, environment := probePostman(data)
	switch {
	case collection:
		return decodePostmanCollection(data, report)
	case environment:
		return decodePostmanEnvironment(data)
	}
	return Bundle{}, ErrNotRecognised
}

func decodePostmanCollection(data []byte, report *Report) (Bundle, error) {
	var pm pmCollection
	if err := json.Unmarshal(data, &pm); err != nil || pm.Info.Name == "" {
		return Bundle{}, ErrNotRecognised
	}
	if pm.Info.Schema != "" && !strings.Contains(pm.Info.Schema, "v2.") {
		// v1 is a different shape altogether, and Postman itself stopped
		// writing it years ago.
		return Bundle{}, ErrNotRecognised
	}
	if len(pm.Event) > 0 {
		report.Skip("pre-request/test script")
	}
	root := BundleCollection{Name: pm.Info.Name, Vars: pmVars(pm.Variable)}
	decodePostmanItems(&root, pm.Item, pm.Auth, report)
	return Bundle{Collections: []BundleCollection{root}}, nil
}

func pmVars(rows []pmKV) []BundleVar {
	var out []BundleVar
	for _, row := range rows {
		if row.Key == "" {
			continue
		}
		out = append(out, BundleVar{Key: row.Key, Value: row.text(), Secret: row.Type == "secret", Disabled: row.off()})
	}
	return out
}

func decodePostmanItems(into *BundleCollection, items []pmItem, inherited *pmAuth, report *Report) {
	for _, item := range items {
		auth := inherited
		if item.Auth != nil && item.Auth.Type != "inherit" {
			auth = item.Auth
		}
		if len(item.Event) > 0 {
			report.Skip("pre-request/test script")
		}
		if len(item.Request) == 0 || item.Item != nil {
			folder := BundleCollection{Name: nonEmpty(item.Name, "Folder"), Vars: pmVars(item.Variable)}
			decodePostmanItems(&folder, item.Item, auth, report)
			into.Folders = append(into.Folders, folder)
			continue
		}
		if req, ok := decodePostmanRequest(item, auth, report); ok {
			into.Requests = append(into.Requests, BundleRequest{Request: req})
		}
	}
}

func decodePostmanRequest(item pmItem, inherited *pmAuth, report *Report) (RequestInput, bool) {
	var pr pmRequest
	// A request can be a bare URL string in v2.
	var bare string
	if json.Unmarshal(item.Request, &bare) == nil {
		pr.URL, _ = json.Marshal(bare)
	} else if err := json.Unmarshal(item.Request, &pr); err != nil {
		report.Skip("unreadable request")
		return RequestInput{}, false
	}
	req := RequestInput{Name: nonEmpty(item.Name, "Request"), Method: nonEmpty(strings.ToUpper(pr.Method), "GET")}

	var u pmURL
	var rawURL string
	if json.Unmarshal(pr.URL, &rawURL) == nil {
		u.Raw = rawURL
	} else {
		_ = json.Unmarshal(pr.URL, &u)
	}
	base, query := splitQuery(u.Raw)
	if u.Query != nil {
		query = nil
		for _, row := range u.Query {
			if row.Key == "" && row.text() == "" {
				continue
			}
			query = append(query, KV{Key: row.Key, Value: row.text(), Disabled: row.Disabled})
		}
	}
	if strings.TrimSpace(base) == "" {
		report.Skip("request without a URL")
		return RequestInput{}, false
	}
	req.URL, req.Query = base, query
	for _, row := range u.Variable {
		req.PathVars = append(req.PathVars, KV{Key: row.Key, Value: row.text()})
	}

	// Headers are a list in v2.1 and may be a "k: v\n" string in v2.0.
	var headers []pmKV
	var headerText string
	if json.Unmarshal(pr.Header, &headerText) == nil {
		for _, line := range strings.Split(headerText, "\n") {
			if key, value, ok := strings.Cut(line, ":"); ok {
				headers = append(headers, pmKV{Key: strings.TrimSpace(key), Value: strings.TrimSpace(value)})
			}
		}
	} else {
		_ = json.Unmarshal(pr.Header, &headers)
	}
	for _, row := range headers {
		req.Headers = append(req.Headers, KV{Key: row.Key, Value: row.text(), Disabled: row.Disabled})
	}

	if pr.Body != nil {
		switch pr.Body.Mode {
		case "raw":
			req.Body = pr.Body.Raw
			language := ""
			if pr.Body.Options != nil {
				language = strings.ToLower(pr.Body.Options.Raw.Language)
			}
			switch language {
			case "json":
				req.BodyMode = BodyJSON
			case "xml", "html":
				req.BodyMode = BodyXML
			case "text":
				req.BodyMode = BodyText
			}
		case "urlencoded":
			req.BodyMode = BodyURLEncoded
			for _, row := range pr.Body.URLEncoded {
				req.Form = append(req.Form, KV{Key: row.Key, Value: row.text(), Disabled: row.Disabled})
			}
		case "formdata":
			req.BodyMode = BodyMultipart
			for _, row := range pr.Body.FormData {
				if row.Type == "file" {
					report.Skip("file upload field")
					continue
				}
				req.Form = append(req.Form, KV{Key: row.Key, Value: row.text(), Disabled: row.Disabled})
			}
		case "", "none":
		default:
			// graphql, file, binary.
			report.Skip(pr.Body.Mode + " body")
		}
	}

	auth := inherited
	if pr.Auth != nil && pr.Auth.Type != "inherit" {
		auth = pr.Auth
	}
	decodePostmanAuth(&req, auth, report)
	return req, true
}

// decodePostmanAuth turns a Postman auth into the header it would have sent.
// Postman keeps the token on the request; DevKit binds an account, and an
// account is something this vault has to be told about, not something a file
// can name. The header keeps the request working meanwhile.
func decodePostmanAuth(req *RequestInput, auth *pmAuth, report *Report) {
	if auth == nil {
		return
	}
	get := func(rows []pmKV, key string) string {
		for _, row := range rows {
			if row.Key == key {
				return row.text()
			}
		}
		return ""
	}
	switch auth.Type {
	case "", "noauth":
	case "bearer":
		req.Headers = append(req.Headers, KV{Key: "Authorization", Value: "Bearer " + get(auth.Bearer, "token")})
	case "basic":
		user, pass := get(auth.Basic, "username"), get(auth.Basic, "password")
		if strings.Contains(user+pass, "{{") {
			// Base64 of a variable is not the variable's base64, and DevKit
			// cannot run a script to compute it at send time.
			report.Skip("basic auth built from variables")
			return
		}
		req.Headers = append(req.Headers, KV{
			Key: "Authorization", Value: "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass)),
		})
	case "apikey":
		key, value := get(auth.APIKey, "key"), get(auth.APIKey, "value")
		if get(auth.APIKey, "in") == "query" {
			req.Query = append(req.Query, KV{Key: key, Value: value})
		} else {
			req.Headers = append(req.Headers, KV{Key: key, Value: value})
		}
	default:
		report.Skip(auth.Type + " auth")
	}
}

func decodePostmanEnvironment(data []byte) (Bundle, error) {
	var pm pmEnvironment
	if err := json.Unmarshal(data, &pm); err != nil || pm.Values == nil {
		return Bundle{}, ErrNotRecognised
	}
	vars := pmVars(pm.Values)
	if pm.Scope == "globals" {
		if vars == nil {
			vars = []BundleVar{}
		}
		return Bundle{Globals: vars}, nil
	}
	// An environment DevKit exported carries its tag in its name — Postman
	// has nowhere else to keep it — and it comes back as the tag.
	name, tag := pm.Name, ""
	if m := taggedName.FindStringSubmatch(pm.Name); m != nil {
		name, tag = m[1], m[2]
	}
	return Bundle{Environments: []BundleEnvironment{{Name: nonEmpty(name, "Imported"), Tag: tag, Vars: vars}}}, nil
}

var taggedName = regexp.MustCompile(`^(.*\S) \[([A-Za-z0-9_.-]+)\]$`)

// ---- writing ---------------------------------------------------------

type pmOutItem struct {
	Name     string      `json:"name"`
	Item     []pmOutItem `json:"item,omitempty"`
	Request  *pmOutReq   `json:"request,omitempty"`
	Variable []pmKV      `json:"variable,omitempty"`
}

type pmOutReq struct {
	Method string  `json:"method"`
	Header []pmKV  `json:"header"`
	URL    pmURL   `json:"url"`
	Body   *pmBody `json:"body,omitempty"`
	Auth   *pmAuth `json:"auth,omitempty"`
}

type pmOutCollection struct {
	Info struct {
		PostmanID string `json:"_postman_id"`
		Name      string `json:"name"`
		Schema    string `json:"schema"`
	} `json:"info"`
	Item     []pmOutItem `json:"item"`
	Variable []pmKV      `json:"variable,omitempty"`
}

type pmOutEnvironment struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Values     []pmKV `json:"values"`
	Scope      string `json:"_postman_variable_scope"`
	ExportedAt string `json:"_postman_exported_at"`
	ExportedBy string `json:"_postman_exported_using"`
}

func (postmanFormat) Encode(kind ExportKind, bundle Bundle, report *Report) ([]byte, error) {
	var out any
	switch kind {
	case ExportCollection:
		if len(bundle.Collections) != 1 {
			return nil, ErrInvalid
		}
		root := bundle.Collections[0]
		c := pmOutCollection{Variable: pmOutVars(root.Vars, false), Item: pmOutItems(root)}
		c.Info.PostmanID, c.Info.Name, c.Info.Schema = newUUID(), root.Name, postmanSchema
		out = c
	case ExportEnvironment:
		if len(bundle.Environments) != 1 {
			return nil, ErrInvalid
		}
		env := bundle.Environments[0]
		// Postman has no env tag; the name keeps it visible to whoever imports
		// the file back, because the tag is what decided whether to ask.
		name := env.Name
		if env.Tag != "" {
			name += " [" + env.Tag + "]"
		}
		out = pmEnv(name, "environment", env.Vars)
	case ExportGlobals:
		out = pmEnv("Globals", "globals", bundle.Globals)
	default:
		return nil, ErrFormatCannot
	}
	return json.MarshalIndent(out, "", "  ")
}

func pmEnv(name, scope string, vars []BundleVar) pmOutEnvironment {
	return pmOutEnvironment{
		ID: newUUID(), Name: name, Values: pmOutVars(vars, true), Scope: scope,
		ExportedAt: time.Now().UTC().Format(time.RFC3339), ExportedBy: "DevKit",
	}
}

func pmOutVars(vars []BundleVar, environment bool) []pmKV {
	out := make([]pmKV, 0, len(vars))
	for _, v := range vars {
		row := pmKV{Key: v.Key, Value: v.Value, Type: "default"}
		if v.Secret {
			row.Type = "secret"
		}
		if environment {
			enabled := !v.Disabled
			row.Enabled = &enabled
		} else {
			row.Disabled = v.Disabled
		}
		out = append(out, row)
	}
	return out
}

func pmOutItems(c BundleCollection) []pmOutItem {
	out := make([]pmOutItem, 0, len(c.Folders)+len(c.Requests))
	for _, folder := range c.Folders {
		out = append(out, pmOutItem{Name: folder.Name, Item: pmOutItems(folder), Variable: pmOutVars(folder.Vars, false)})
	}
	for _, br := range c.Requests {
		out = append(out, pmOutItem{Name: br.Request.Name, Request: pmOutRequest(br)})
	}
	return out
}

func pmRows(rows KVList) []pmKV {
	out := make([]pmKV, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Key) == "" {
			continue
		}
		out = append(out, pmKV{Key: row.Key, Value: row.Value, Type: "text", Disabled: row.Disabled})
	}
	return out
}

func pmOutRequest(br BundleRequest) *pmOutReq {
	req := br.Request.Request().normalized()
	out := &pmOutReq{Method: req.Method, Header: pmRows(req.Headers), URL: pmURLFor(req)}
	switch req.BodyMode {
	case BodyJSON, BodyXML, BodyText:
		options := &pmRawOptions{}
		options.Raw.Language = map[BodyMode]string{BodyJSON: "json", BodyXML: "xml", BodyText: "text"}[req.BodyMode]
		out.Body = &pmBody{Mode: "raw", Raw: req.Body, Options: options}
	case BodyURLEncoded:
		out.Body = &pmBody{Mode: "urlencoded", URLEncoded: pmRows(req.Form)}
	case BodyMultipart:
		out.Body = &pmBody{Mode: "formdata", FormData: pmRows(req.Form)}
	}
	if c := br.Credential; c != nil {
		switch c.Kind {
		case AuthBasic:
			out.Auth = &pmAuth{Type: "basic", Basic: []pmKV{
				{Key: "username", Value: "{{" + c.UserVar + "}}", Type: "string"},
				{Key: "password", Value: "{{" + c.PasswordVar + "}}", Type: "string"},
			}}
		case AuthHeader:
			out.Auth = &pmAuth{Type: "apikey", APIKey: []pmKV{
				{Key: "key", Value: nonEmpty(c.Header, "X-API-Key"), Type: "string"},
				{Key: "value", Value: "{{" + c.TokenVar + "}}", Type: "string"},
				{Key: "in", Value: "header", Type: "string"},
			}}
		default:
			out.Auth = &pmAuth{Type: "bearer", Bearer: []pmKV{{Key: "token", Value: "{{" + c.TokenVar + "}}", Type: "string"}}}
		}
	}
	return out
}

// pmURLFor is the address in Postman's own shape: raw, plus the pieces
// Postman's importer reads — a host that is a {{variable}} stays one piece,
// so `{{baseUrl}}/orders/:id` round-trips as Postman writes it itself.
func pmURLFor(req Request) pmURL {
	u := pmURL{Query: pmRows(req.Query)}
	for _, row := range req.PathVars {
		u.Variable = append(u.Variable, pmKV{Key: row.Key, Value: row.Value})
	}
	raw := req.URL
	var enabled []string
	for _, row := range req.Query.Active() {
		enabled = append(enabled, row.Key+"="+row.Value)
	}
	if len(enabled) > 0 {
		sep := "?"
		if strings.Contains(raw, "?") {
			sep = "&"
		}
		raw += sep + strings.Join(enabled, "&")
	}
	u.Raw = raw

	rest := req.URL
	if cut := strings.IndexAny(rest, "?#"); cut >= 0 {
		rest = rest[:cut]
	}
	if at := strings.Index(rest, "://"); at >= 0 && !strings.Contains(rest[:at], "{{") {
		u.Protocol, rest = rest[:at], rest[at+3:]
	}
	host, path, _ := strings.Cut(rest, "/")
	if strings.HasPrefix(host, "{{") {
		u.Host = []string{host}
	} else {
		if h, port, ok := strings.Cut(host, ":"); ok {
			host, u.Port = h, port
		}
		if host != "" {
			u.Host = strings.Split(host, ".")
		}
	}
	if path != "" {
		u.Path = strings.Split(path, "/")
	}
	return u
}

// newUUID is a random v4 UUID — the id Postman expects on what it imports.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
