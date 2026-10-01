// Package apiclient sends HTTP requests, Postman-shaped but not Postman.
//
// Two differences carry the design. Requests are not typed from scratch — an
// endpoint Code Graph already indexed becomes a draft with its method, path
// and parameters filled in. And auth is a binding to an account rather than a
// pasted string: the request stores an id, internal/accounts resolves the
// value inside Go at the moment of sending, and rotating the credential is a
// one-place edit that leaves every request working.
//
// Requests go out through net/http in Go rather than the webview's fetch: no
// CORS to work around, the system's own proxy and TLS, and a timeout and a
// cancel that actually reach the socket.
package apiclient

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound      = errors.New("apiclient.notFound")
	ErrInvalid       = errors.New("apiclient.invalid")
	ErrNotAllowed    = errors.New("apiclient.notAllowed")
	ErrSchemeInvalid = errors.New("apiclient.schemeInvalid")
	// ErrCollectionNotEmpty refuses to take a folder's requests with it.
	// Tidying is not the same action as losing saved work, and a vault item
	// has no undo.
	ErrCollectionNotEmpty = errors.New("apiclient.collectionNotEmpty")
	ErrEnvRequired        = errors.New("apiclient.environmentRequired")
	// ErrPlainHTTP is http:// to a host that is not this machine, refused
	// because of what would travel in the clear: a bound credential, or
	// anything at all on an environment that asks before sending.
	ErrPlainHTTP = errors.New("apiclient.plainHTTP")
)

// Limits are the ceilings. Display gets the larger one; an agent gets the
// smaller, because a response is context and context is not free.
const (
	MaxDisplayBytes = 5 << 20
	MaxAgentBytes   = 256 << 10
	DefaultTimeout  = 30 * time.Second
)

// AuthKind is how a credential is presented.
type AuthKind string

const (
	AuthNone   AuthKind = ""
	AuthBearer AuthKind = "bearer"
	AuthBasic  AuthKind = "basic"
	AuthHeader AuthKind = "header"
)

// Auth binds a request to an account. There is no field for a value: the
// value never lives here, which is what makes Copy as cURL safe and rotation
// a one-place edit.
type Auth struct {
	Kind AuthKind `json:"kind"`
	// AccountID names the account in internal/accounts.
	AccountID string `json:"accountId"`
	// Header is the header name for AuthHeader (X-API-Key, and so on).
	Header string `json:"header,omitempty"`
}

// BodyMode is what the body is, which decides how it is encoded and the
// Content-Type it goes out with when the request does not set one itself.
type BodyMode string

const (
	BodyNone BodyMode = "none"
	BodyJSON BodyMode = "json"
	BodyText BodyMode = "text"
	BodyXML  BodyMode = "xml"
	// BodyURLEncoded and BodyMultipart read Form, not Body.
	BodyURLEncoded BodyMode = "urlencoded"
	BodyMultipart  BodyMode = "multipart"
)

// RequestInput is a request as an editor holds it: everything but the
// timestamps, which are the store's to write. It is what the bridge takes,
// because a bound method with a time in its arguments fails in the binding
// layer unless the frontend invents an RFC3339 stamp.
type RequestInput struct {
	ID           string `json:"id"`
	CollectionID string `json:"collectionId"`
	Name         string `json:"name"`
	Method       string `json:"method"`
	// URL is the address without its query: the query is Query, so the two
	// can never disagree about what goes out. A URL that still carries one
	// (an agent's, an old request's) keeps it, and Query is appended after.
	URL     string `json:"url"`
	Query   KVList `json:"query,omitempty"`
	Headers KVList `json:"headers,omitempty"`
	// PathVars fill the :name segments of the path — the value of THIS call,
	// such as the id of the refund being looked at, which belongs to the
	// request rather than to any environment.
	PathVars KVList   `json:"pathVars,omitempty"`
	BodyMode BodyMode `json:"bodyMode,omitempty"`
	Body     string   `json:"body,omitempty"`
	// Form is the body for urlencoded and multipart.
	Form KVList `json:"form,omitempty"`
	Auth Auth   `json:"auth"`
	// OriginRef points back at the Code Graph endpoint this was drafted from.
	OriginRef string `json:"originRef,omitempty"`
}

// Request is one saved request.
type Request struct {
	ID           string   `json:"id"`
	CollectionID string   `json:"collectionId"`
	Name         string   `json:"name"`
	Method       string   `json:"method"`
	URL          string   `json:"url"`
	Query        KVList   `json:"query,omitempty"`
	Headers      KVList   `json:"headers,omitempty"`
	PathVars     KVList   `json:"pathVars,omitempty"`
	BodyMode     BodyMode `json:"bodyMode,omitempty"`
	Body         string   `json:"body,omitempty"`
	Form         KVList   `json:"form,omitempty"`
	Auth         Auth     `json:"auth"`
	// OriginRef points back at the Code Graph endpoint this was drafted from.
	OriginRef string    `json:"originRef,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Request turns an editor's request into one, timestamps empty.
func (in RequestInput) Request() Request {
	return Request{
		ID: in.ID, CollectionID: in.CollectionID, Name: in.Name, Method: in.Method,
		URL: in.URL, Query: in.Query, Headers: in.Headers, PathVars: in.PathVars,
		BodyMode: in.BodyMode, Body: in.Body, Form: in.Form, Auth: in.Auth,
		OriginRef: in.OriginRef,
	}
}

// normalized fills what an older or a hand-written request leaves out: the
// method's case and the body mode. A body saved before there were modes is
// JSON when it parses as JSON — which is what the person meant every time the
// draft came from Code Graph — and text otherwise.
func (r Request) normalized() Request {
	r.Method = strings.ToUpper(strings.TrimSpace(r.Method))
	if r.Method == "" {
		r.Method = http.MethodGet
	}
	switch r.BodyMode {
	case BodyNone, BodyJSON, BodyText, BodyXML, BodyURLEncoded, BodyMultipart:
	default:
		switch {
		case strings.TrimSpace(r.Body) != "" && json.Valid([]byte(r.Body)):
			r.BodyMode = BodyJSON
		case strings.TrimSpace(r.Body) != "":
			r.BodyMode = BodyText
		case len(r.Form.Active()) > 0:
			r.BodyMode = BodyURLEncoded
		default:
			r.BodyMode = BodyNone
		}
	}
	return r
}

// Environment is a set of variables and the env tag they run under.
type Environment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Env is the environment tag, required: it is what decides whether a send
	// asks first.
	Env string `json:"env"`
	// Vars are the non-secret variables, plaintext so the picker can show
	// them without opening the vault.
	Vars map[string]string `json:"vars"`
	// SecretVars are the names of the encrypted ones; their values are never
	// returned.
	SecretVars []string `json:"secretVars"`
}

// Collection groups requests. A collection with no parent is what Postman
// calls a collection; one with a parent is a folder in it. Both carry
// variables, and a nearer folder's win over its parents'.
type Collection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
	Order    int    `json:"order"`
	// Vars are the plain variables; SecretVars name the sealed ones, whose
	// values are never returned.
	Vars       map[string]string `json:"vars"`
	SecretVars []string          `json:"secretVars"`
}

// EndpointDraft is a request drafted from a Code Graph endpoint.
//
// It is not saved: a draft becomes a request only when the person saves it,
// and an agent that asked for one gets what a person sees rather than a row
// appearing in their sidebar.
//
// The type lives here because it is a request; the function that builds one
// lives in the composition root, because building it means reading the Code
// Graph index and this package must not depend on that.
type EndpointDraft struct {
	Request
	// BodyInferred says the body skeleton came from the handler's own declared
	// type. False is not a failure — a Lua endpoint declares no body at all —
	// which is why Note carries the reason.
	BodyInferred bool `json:"bodyInferred"`
	// Note is the one sentence to show about the body. Empty when the skeleton
	// was read from the code.
	Note string `json:"note"`
}

// The notes, as sentences rather than codes: each is shown verbatim, and an
// empty {} for all three would read as "the DTO is empty" — the one thing none
// of them means.
const (
	NoteNoBodyDeclared  = "This endpoint declares no request body."
	NoteBodyNotReadable = "The handler takes a body, but its type is not in this project's source — fill it in by hand."
	NoteNoDeclaredTypes = "This language does not declare parameter types, so the body cannot be read from the code."
)

// Response is what came back, already bounded.
type Response struct {
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	// Headers is one value per name, repeated values joined by ", ". It is the
	// shape tool-result cards and agents already read.
	Headers map[string]string `json:"headers"`
	// HeaderList is every header line as it came, in name order — the only
	// place two Set-Cookie lines stay two.
	HeaderList KVList `json:"headerList"`
	Body       string `json:"body"`
	Truncated  bool   `json:"truncated"`
	Bytes      int    `json:"bytes"`
	DurationMS int64  `json:"durationMs"`
	// RedirectBlocked is where a redirect wanted to go when it was not
	// followed because it left the origin. The 3xx is the response.
	RedirectBlocked string `json:"redirectBlocked,omitempty"`
}

// varPattern is {{name}}.
var varPattern = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)

// Interpolate replaces {{name}} from vars. An unknown name is left as written
// rather than replaced with nothing: a URL with a visible {{baseUrl}} in it
// says what went wrong, and one silently missing a host does not.
func Interpolate(template string, vars map[string]string) string {
	return varPattern.ReplaceAllStringFunc(template, func(match string) string {
		name := varPattern.FindStringSubmatch(match)[1]
		if value, ok := vars[name]; ok {
			return value
		}
		return match
	})
}

// MissingVars lists the {{names}} in a template that vars cannot fill.
func MissingVars(template string, vars map[string]string) []string {
	var missing []string
	seen := map[string]bool{}
	for _, match := range varPattern.FindAllStringSubmatch(template, -1) {
		name := match[1]
		if _, ok := vars[name]; !ok && !seen[name] {
			missing = append(missing, name)
			seen[name] = true
		}
	}
	return missing
}

// requestMetadata is a request's plaintext half.
type requestMetadata struct {
	CollectionID string `json:"collection_id"`
	Name         string `json:"name"`
	Method       string `json:"method"`
	URLTemplate  string `json:"url_template"`
	AccountID    string `json:"account_id,omitempty"`
	OriginRef    string `json:"origin_ref,omitempty"`
}

// requestPayload is the encrypted half: everything that can carry a value a
// user would not want in plaintext metadata. Query and headers were maps once;
// KVList reads both.
type requestPayload struct {
	Query    KVList   `json:"query,omitempty"`
	Headers  KVList   `json:"headers,omitempty"`
	PathVars KVList   `json:"path_vars,omitempty"`
	BodyMode BodyMode `json:"body_mode,omitempty"`
	Body     string   `json:"body,omitempty"`
	Form     KVList   `json:"form,omitempty"`
	Auth     Auth     `json:"auth"`
}

type environmentMetadata struct {
	Name string            `json:"name"`
	Env  string            `json:"env"`
	Vars map[string]string `json:"vars,omitempty"`
}

type collectionMetadata struct {
	Name     string `json:"name"`
	ParentID string `json:"parent_id,omitempty"`
	Order    int    `json:"order"`
}

// collectionPayload is a collection's variables, both halves, sealed.
//
// Plain ones too, unlike an environment's: a folder's metadata is checked
// field by field by every device that syncs it, and a DevKit from before
// collection variables would refuse the whole pull over one unknown field.
// The payload is opaque to it. A collection sealed before variables existed
// holds an empty payload, which reads as none.
type collectionPayload struct {
	Vars    map[string]string `json:"vars,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

// globalsMetadata is the plain half of the global variables.
type globalsMetadata struct {
	Vars map[string]string `json:"vars,omitempty"`
}

// Key ids, distinct so an envelope written as one cannot open as another.
const (
	KeyIDCollection  = "api-collection"
	KeyIDRequest     = "api-request"
	KeyIDEnvironment = "api-environment"
	KeyIDGlobals     = "api-globals"
)

func decodeInto(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrInvalid
	}
	return nil
}

// noRedirectToAnotherOrigin is the redirect policy. A redirect within the
// same origin is ordinary; one that leaves it would carry the Authorization
// header somewhere the user never aimed at, so it stops and the 3xx is what
// comes back — http.ErrUseLastResponse hands the redirect itself to the
// caller rather than an error, and Send names where it pointed.
//
// The comparison is the whole origin — scheme, host AND port — not just the
// hostname. Two services on one machine share a hostname and differ only by
// port, and https://api.example redirecting to http://api.example is a
// downgrade that must not carry a credential either.
func noRedirectToAnotherOrigin(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if originOf(req.URL) != originOf(via[0].URL) {
		return http.ErrUseLastResponse
	}
	if len(via) >= 10 {
		return errors.New("apiclient: too many redirects")
	}
	return nil
}

// originOf canonicalises scheme://host:port, filling in the default port so
// https://example.com and https://example.com:443 are one origin.
func originOf(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return u.Scheme + "://" + u.Hostname() + ":" + port
}
