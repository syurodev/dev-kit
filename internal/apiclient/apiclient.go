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
	// ErrRedirectHost is a redirect leaving the host the request was aimed
	// at. Following it would send the credential somewhere else.
	ErrRedirectHost = errors.New("apiclient.redirectHost")
	ErrEnvRequired  = errors.New("apiclient.environmentRequired")
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

// Request is one saved request.
type Request struct {
	ID           string            `json:"id"`
	CollectionID string            `json:"collectionId"`
	Name         string            `json:"name"`
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	Query        map[string]string `json:"query,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`
	Auth         Auth              `json:"auth"`
	// OriginRef points back at the Code Graph endpoint this was drafted from.
	OriginRef string    `json:"originRef,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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

// Collection groups requests.
type Collection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
	Order    int    `json:"order"`
}

// Response is what came back, already bounded.
type Response struct {
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	Truncated  bool              `json:"truncated"`
	Bytes      int               `json:"bytes"`
	DurationMS int64             `json:"durationMs"`
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
// user would not want in plaintext metadata.
type requestPayload struct {
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Auth    Auth              `json:"auth"`
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

// Key ids, distinct so an envelope written as one cannot open as another.
const (
	KeyIDCollection  = "api-collection"
	KeyIDRequest     = "api-request"
	KeyIDEnvironment = "api-environment"
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
// comes back.
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
		return ErrRedirectHost
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
