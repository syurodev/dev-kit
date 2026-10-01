package apiclient

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNotPostman is a file that is not a Postman v2.0/v2.1 collection or a
// Postman environment export.
var ErrNotPostman = errors.New("apiclient.notPostman")

// MaxImportBytes bounds an import. A collection is text; one this size is
// already thousands of requests.
const MaxImportBytes = 20 << 20

// ImportResult says what an import made and what it could not carry over.
type ImportResult struct {
	CollectionID  string `json:"collectionId,omitempty"`
	EnvironmentID string `json:"environmentId,omitempty"`
	Name          string `json:"name"`
	Folders       int    `json:"folders"`
	Requests      int    `json:"requests"`
	Variables     int    `json:"variables"`
	// Warnings are sentences, one per kind of thing left behind, with a count.
	Warnings []string `json:"warnings"`
}

// ---- the Postman v2 collection format, as much of it as is read ------

type pmKV struct {
	Key      string `json:"key"`
	Value    any    `json:"value"`
	Type     string `json:"type"`
	Disabled bool   `json:"disabled"`
	Src      any    `json:"src"`
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

type pmAuth struct {
	Type   string `json:"type"`
	Bearer []pmKV `json:"bearer"`
	Basic  []pmKV `json:"basic"`
	APIKey []pmKV `json:"apikey"`
}

type pmURL struct {
	Raw      string `json:"raw"`
	Query    []pmKV `json:"query"`
	Variable []pmKV `json:"variable"`
}

type pmBody struct {
	Mode       string `json:"mode"`
	Raw        string `json:"raw"`
	URLEncoded []pmKV `json:"urlencoded"`
	FormData   []pmKV `json:"formdata"`
	Options    struct {
		Raw struct {
			Language string `json:"language"`
		} `json:"raw"`
	} `json:"options"`
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
		Name   string `json:"name"`
		Schema string `json:"schema"`
	} `json:"info"`
	Item     []pmItem `json:"item"`
	Auth     *pmAuth  `json:"auth"`
	Event    []any    `json:"event"`
	Variable []pmKV   `json:"variable"`
}

type pmEnvironment struct {
	Name   string `json:"name"`
	Values []struct {
		Key     string `json:"key"`
		Value   any    `json:"value"`
		Type    string `json:"type"`
		Enabled *bool  `json:"enabled"`
	} `json:"values"`
	Scope string `json:"_postman_variable_scope"`
}

// importer counts as it goes, so the result can say what was left behind.
type importer struct {
	svc    *Service
	result ImportResult
	// skipped counts by reason; the warnings are written from it at the end.
	skipped map[string]int
}

func (im *importer) skip(reason string) { im.skipped[reason]++ }

// ImportPostmanCollection reads a Postman v2.0 or v2.1 collection export into
// a new collection: folders become folders, requests requests, collection
// variables collection variables.
//
// Auth is the one thing that does not carry over as it was: Postman keeps the
// token on the request, DevKit binds an account. A bearer or API-key auth
// becomes the header it would have sent — with whatever {{variable}} it used
// — so the request still works, and binding an account is one click later.
// Scripts are not run here and are counted, not copied.
func (s *Service) ImportPostmanCollection(data string) (ImportResult, error) {
	if len(data) > MaxImportBytes {
		return ImportResult{}, ErrInvalid
	}
	var pm pmCollection
	if err := json.Unmarshal([]byte(data), &pm); err != nil || pm.Info.Name == "" {
		return ImportResult{}, ErrNotPostman
	}
	if pm.Info.Schema != "" && !strings.Contains(pm.Info.Schema, "v2.") {
		return ImportResult{}, ErrNotPostman
	}
	im := &importer{svc: s, skipped: map[string]int{}, result: ImportResult{Name: pm.Info.Name}}

	root, err := s.SaveCollection(Collection{Name: pm.Info.Name})
	if err != nil {
		return ImportResult{}, err
	}
	im.result.CollectionID = root.ID
	if err := im.variables(root.ID, pm.Variable); err != nil {
		return im.result, err
	}
	if len(pm.Event) > 0 {
		im.skip("script")
	}
	if err := im.items(root.ID, pm.Item, pm.Auth); err != nil {
		return im.result, err
	}
	im.result.Warnings = im.warnings()
	return im.result, nil
}

func (im *importer) variables(collectionID string, rows []pmKV) error {
	update := VarsUpdate{Vars: map[string]string{}, Secrets: map[string]string{}}
	for _, row := range rows {
		if row.Disabled || row.Key == "" {
			continue
		}
		if row.Type == "secret" {
			update.Secrets[row.Key] = row.text()
		} else {
			update.Vars[row.Key] = row.text()
		}
		im.result.Variables++
	}
	if len(update.Vars)+len(update.Secrets) == 0 {
		return nil
	}
	_, err := im.svc.SetCollectionVariables(collectionID, update)
	return err
}

func (im *importer) items(parentID string, items []pmItem, inherited *pmAuth) error {
	for order, item := range items {
		auth := inherited
		if item.Auth != nil && item.Auth.Type != "inherit" {
			auth = item.Auth
		}
		if len(item.Event) > 0 {
			im.skip("script")
		}
		if len(item.Request) == 0 || item.Item != nil {
			folder, err := im.svc.SaveCollection(Collection{Name: nonEmpty(item.Name, "Folder"), ParentID: parentID, Order: order})
			if err != nil {
				return err
			}
			im.result.Folders++
			if err := im.variables(folder.ID, item.Variable); err != nil {
				return err
			}
			if err := im.items(folder.ID, item.Item, auth); err != nil {
				return err
			}
			continue
		}
		req, ok := im.request(item, auth)
		if !ok {
			continue
		}
		req.CollectionID = parentID
		if _, err := im.svc.SaveRequest(req); err != nil {
			return err
		}
		im.result.Requests++
	}
	return nil
}

func (im *importer) request(item pmItem, inherited *pmAuth) (Request, bool) {
	var pr pmRequest
	// A request can be a bare URL string in v2.
	var bare string
	if json.Unmarshal(item.Request, &bare) == nil {
		pr.URL, _ = json.Marshal(bare)
	} else if err := json.Unmarshal(item.Request, &pr); err != nil {
		im.skip("unreadable request")
		return Request{}, false
	}
	req := Request{Name: nonEmpty(item.Name, "Request"), Method: nonEmpty(strings.ToUpper(pr.Method), "GET")}

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
		im.skip("request without a URL")
		return Request{}, false
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
			switch strings.ToLower(pr.Body.Options.Raw.Language) {
			case "json":
				req.BodyMode = BodyJSON
			case "xml", "html":
				req.BodyMode = BodyXML
			default:
				req.BodyMode = ""
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
					im.skip("file upload field")
					continue
				}
				req.Form = append(req.Form, KV{Key: row.Key, Value: row.text(), Disabled: row.Disabled})
			}
		case "", "none":
		default:
			// graphql, file, binary.
			im.skip(pr.Body.Mode + " body")
		}
	}

	auth := inherited
	if pr.Auth != nil && pr.Auth.Type != "inherit" {
		auth = pr.Auth
	}
	im.applyAuth(&req, auth)
	return req, true
}

// applyAuth turns a Postman auth into the header it would have sent.
func (im *importer) applyAuth(req *Request, auth *pmAuth) {
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
			im.skip("basic auth built from variables")
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
		im.skip(auth.Type + " auth")
	}
}

func (im *importer) warnings() []string {
	var out []string
	for reason, count := range im.skipped {
		switch reason {
		case "script":
			out = append(out, fmt.Sprintf("%d pre-request/test script block(s) were not imported — DevKit does not run scripts.", count))
		default:
			out = append(out, fmt.Sprintf("%d × %s not imported.", count, reason))
		}
	}
	sort.Strings(out)
	return out
}

// ImportPostmanEnvironment reads a Postman environment export into a new
// environment under envTag — Postman has no tags, so the person picks one —
// or, for a Postman globals export, into the global variables, existing ones
// kept.
func (s *Service) ImportPostmanEnvironment(data, envTag string) (ImportResult, error) {
	if len(data) > MaxImportBytes {
		return ImportResult{}, ErrInvalid
	}
	var pm pmEnvironment
	if err := json.Unmarshal([]byte(data), &pm); err != nil || pm.Values == nil {
		return ImportResult{}, ErrNotPostman
	}
	vars, secrets := map[string]string{}, map[string]string{}
	for _, row := range pm.Values {
		if row.Key == "" || (row.Enabled != nil && !*row.Enabled) {
			continue
		}
		value := pmKV{Value: row.Value}.text()
		if row.Type == "secret" {
			secrets[row.Key] = value
		} else {
			vars[row.Key] = value
		}
	}
	result := ImportResult{Name: nonEmpty(pm.Name, "Imported"), Variables: len(vars) + len(secrets)}

	if pm.Scope == "globals" {
		current, err := s.Globals()
		if err != nil {
			return ImportResult{}, err
		}
		update := VarsUpdate{Vars: current.Vars, Secrets: secrets, KeepSecrets: current.SecretVars}
		if update.Vars == nil {
			update.Vars = map[string]string{}
		}
		for key, value := range vars {
			update.Vars[key] = value
		}
		if _, err := s.SetGlobals(update); err != nil {
			return ImportResult{}, err
		}
		result.Name = "Globals"
		return result, nil
	}

	env, err := s.CreateEnvironment(result.Name, envTag, vars, secrets)
	if err != nil {
		return ImportResult{}, err
	}
	result.EnvironmentID = env.ID
	return result, nil
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
