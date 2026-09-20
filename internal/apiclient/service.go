package apiclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"devkit/internal/vaultitem"
)

// ItemStore is the vault-item surface, declared locally.
type ItemStore interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	UpdateMetadata(id string, metadata json.RawMessage) (vaultitem.Record, error)
	Update(id, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	Delete(id string) error
}

// EnvLookup is the environment registry as this package needs it.
type EnvLookup interface {
	Exists(name string) bool
	RequiresConfirmation(name string) bool
}

// Credentials resolves an account at the moment of sending. It is
// internal/accounts behind a local interface, so the value never crosses a
// package boundary it does not need to.
type Credentials interface {
	ResolveForRequest(ctx context.Context, accountID, env string) (user, secret string, err error)
}

// Confirmer is the interaction gate. Nil is headless, which is a refusal.
type Confirmer interface {
	Confirm(ctx context.Context, title, detail string) (bool, error)
}

// Service is the module.
type Service struct {
	items   ItemStore
	envs    EnvLookup
	creds   Credentials
	confirm Confirmer
	client  *http.Client
}

func NewService(items ItemStore, envs EnvLookup) *Service {
	return &Service{
		items: items,
		envs:  envs,
		client: &http.Client{
			Timeout:       DefaultTimeout,
			CheckRedirect: noRedirectToAnotherOrigin,
		},
	}
}

func (s *Service) SetCredentials(c Credentials) { s.creds = c }
func (s *Service) SetConfirmer(c Confirmer)     { s.confirm = c }

// ---- environments ----------------------------------------------------

// CreateEnvironment makes an environment. The env tag is required and must
// exist: it is what decides whether sending asks first, so an environment
// without one would be an ungated send with no way to notice.
func (s *Service) CreateEnvironment(name, env string, vars map[string]string, secrets map[string]string) (Environment, error) {
	if strings.TrimSpace(name) == "" || env == "" {
		return Environment{}, ErrInvalid
	}
	if s.envs != nil && !s.envs.Exists(env) {
		return Environment{}, ErrInvalid
	}
	meta := environmentMetadata{Name: name, Env: env, Vars: vars}
	raw, err := json.Marshal(meta)
	if err != nil {
		return Environment{}, err
	}
	payload, err := json.Marshal(secrets)
	if err != nil {
		return Environment{}, err
	}
	record, err := s.items.Create(vaultitem.ItemTypeAPIEnvironment, KeyIDEnvironment, raw, payload)
	if err != nil {
		return Environment{}, err
	}
	return Environment{ID: record.ID, Name: name, Env: env, Vars: vars, SecretVars: sortedKeys(secrets)}, nil
}

// Environments lists them without any secret value.
func (s *Service) Environments() ([]Environment, error) {
	records, err := s.items.ListByType(vaultitem.ItemTypeAPIEnvironment)
	if err != nil {
		return nil, err
	}
	out := make([]Environment, 0, len(records))
	for _, record := range records {
		var meta environmentMetadata
		if decodeInto(record.Metadata, &meta) != nil {
			continue
		}
		env := Environment{ID: record.ID, Name: meta.Name, Env: meta.Env, Vars: meta.Vars}
		if secrets, err := s.secretsOf(record); err == nil {
			env.SecretVars = sortedKeys(secrets)
		}
		out = append(out, env)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Service) secretsOf(record vaultitem.Record) (map[string]string, error) {
	payload, err := s.items.OpenPayload(record, KeyIDEnvironment)
	if err != nil {
		return nil, err
	}
	secrets := map[string]string{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &secrets); err != nil {
			return nil, err
		}
	}
	return secrets, nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// EnvUsage lets DeleteTag see which environment tags the API client holds, the
// same way ssh/database/kafka profiles are counted.
func (s *Service) EnvUsage(tag string) []string {
	envs, err := s.Environments()
	if err != nil {
		return nil
	}
	var out []string
	for _, env := range envs {
		if env.Env == tag {
			out = append(out, "API environment "+env.Name)
		}
	}
	return out
}

// ---- requests --------------------------------------------------------

// SaveRequest creates or replaces a request.
func (s *Service) SaveRequest(req Request) (Request, error) {
	if strings.TrimSpace(req.Name) == "" || req.Method == "" || req.URL == "" {
		return Request{}, ErrInvalid
	}
	meta := requestMetadata{
		CollectionID: req.CollectionID, Name: req.Name,
		Method: strings.ToUpper(req.Method), URLTemplate: req.URL,
		AccountID: req.Auth.AccountID, OriginRef: req.OriginRef,
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return Request{}, err
	}
	payload, err := json.Marshal(requestPayload{
		Query: req.Query, Headers: req.Headers, Body: req.Body, Auth: req.Auth,
	})
	if err != nil {
		return Request{}, err
	}
	var record vaultitem.Record
	if req.ID == "" {
		record, err = s.items.Create(vaultitem.ItemTypeAPIRequest, KeyIDRequest, raw, payload)
	} else {
		record, err = s.items.Update(req.ID, KeyIDRequest, raw, payload)
	}
	if err != nil {
		return Request{}, err
	}
	req.ID = record.ID
	req.Method = meta.Method
	req.CreatedAt, req.UpdatedAt = record.CreatedAt, record.UpdatedAt
	return req, nil
}

// Requests lists summaries: method, URL template and name, with no body and
// no headers, because a sidebar does not need a decryption per row.
func (s *Service) Requests() ([]Request, error) {
	records, err := s.items.ListByType(vaultitem.ItemTypeAPIRequest)
	if err != nil {
		return nil, err
	}
	out := make([]Request, 0, len(records))
	for _, record := range records {
		var meta requestMetadata
		if decodeInto(record.Metadata, &meta) != nil {
			continue
		}
		out = append(out, Request{
			ID: record.ID, CollectionID: meta.CollectionID, Name: meta.Name,
			Method: meta.Method, URL: meta.URLTemplate, OriginRef: meta.OriginRef,
			Auth:      Auth{AccountID: meta.AccountID},
			CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Request returns one request in full.
func (s *Service) Request(id string) (Request, error) {
	record, err := s.items.Get(id)
	if err != nil {
		return Request{}, ErrNotFound
	}
	var meta requestMetadata
	if err := decodeInto(record.Metadata, &meta); err != nil {
		return Request{}, err
	}
	req := Request{
		ID: record.ID, CollectionID: meta.CollectionID, Name: meta.Name,
		Method: meta.Method, URL: meta.URLTemplate, OriginRef: meta.OriginRef,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
	payload, err := s.items.OpenPayload(record, KeyIDRequest)
	if err != nil {
		return req, err
	}
	var body requestPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &body); err != nil {
			return req, err
		}
	}
	req.Query, req.Headers, req.Body, req.Auth = body.Query, body.Headers, body.Body, body.Auth
	return req, nil
}

func (s *Service) DeleteRequest(id string) error { return s.items.Delete(id) }

// ---- sending ---------------------------------------------------------

// SendInput is one send.
type SendInput struct {
	Request Request
	// EnvironmentID is required: the gate needs an environment to read, so a
	// send with none is a send nobody could have gated.
	EnvironmentID string
	// ForAgent bounds the response harder and is what the MCP path passes.
	ForAgent bool
}

// Send resolves variables, resolves the credential, gates on the environment
// and makes the request.
func (s *Service) Send(ctx context.Context, in SendInput) (Response, error) {
	if in.EnvironmentID == "" {
		return Response{}, ErrEnvRequired
	}
	record, err := s.items.Get(in.EnvironmentID)
	if err != nil {
		return Response{}, ErrNotFound
	}
	var envMeta environmentMetadata
	if err := decodeInto(record.Metadata, &envMeta); err != nil {
		return Response{}, err
	}
	secrets, err := s.secretsOf(record)
	if err != nil {
		return Response{}, err
	}
	vars := map[string]string{}
	for key, value := range envMeta.Vars {
		vars[key] = value
	}
	for key, value := range secrets {
		vars[key] = value
	}

	target := Interpolate(in.Request.URL, vars)
	parsed, err := url.Parse(target)
	if err != nil {
		return Response{}, ErrInvalid
	}
	if err := checkScheme(parsed); err != nil {
		return Response{}, err
	}

	// The gate comes before anything leaves: env wins, every time, with no
	// allow-always — the same rule the MCP confirmation policy already keeps.
	if s.envs != nil && s.envs.RequiresConfirmation(envMeta.Env) {
		if s.confirm == nil {
			return Response{}, ErrNotAllowed
		}
		allowed, err := s.confirm.Confirm(ctx,
			strings.ToUpper(in.Request.Method)+" "+parsed.Host+" ("+envMeta.Env+")",
			"Send "+strings.ToUpper(in.Request.Method)+" "+parsed.Path+"?")
		if err != nil {
			return Response{}, err
		}
		if !allowed {
			return Response{}, ErrNotAllowed
		}
	}

	query := parsed.Query()
	for key, value := range in.Request.Query {
		query.Set(Interpolate(key, vars), Interpolate(value, vars))
	}
	parsed.RawQuery = query.Encode()

	body := Interpolate(in.Request.Body, vars)
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, strings.ToUpper(in.Request.Method), parsed.String(), reader)
	if err != nil {
		return Response{}, err
	}
	for key, value := range in.Request.Headers {
		httpReq.Header.Set(Interpolate(key, vars), Interpolate(value, vars))
	}
	if err := s.applyAuth(ctx, httpReq, in.Request.Auth, envMeta.Env); err != nil {
		return Response{}, err
	}

	started := time.Now()
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	limit := int64(MaxDisplayBytes)
	if in.ForAgent {
		limit = MaxAgentBytes
	}
	// Read one byte past the limit so "exactly at the limit" is not reported
	// as truncated.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return Response{}, err
	}
	truncated := int64(len(raw)) > limit
	if truncated {
		raw = raw[:limit]
	}
	headers := map[string]string{}
	for key := range resp.Header {
		headers[key] = resp.Header.Get(key)
	}
	return Response{
		Status: resp.StatusCode, StatusText: resp.Status, Headers: headers,
		Body: string(raw), Truncated: truncated, Bytes: len(raw),
		DurationMS: time.Since(started).Milliseconds(),
	}, nil
}

// applyAuth resolves the bound account and builds the header. This is the only
// place a credential exists, and it exists for the length of one call.
func (s *Service) applyAuth(ctx context.Context, req *http.Request, auth Auth, env string) error {
	if auth.Kind == AuthNone || auth.AccountID == "" {
		return nil
	}
	if s.creds == nil {
		return ErrNotAllowed
	}
	user, secret, err := s.creds.ResolveForRequest(ctx, auth.AccountID, env)
	if err != nil {
		return err
	}
	switch auth.Kind {
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+secret)
	case AuthBasic:
		req.Header.Set("Authorization", "Basic "+
			base64.StdEncoding.EncodeToString([]byte(user+":"+secret)))
	case AuthHeader:
		name := auth.Header
		if name == "" {
			name = "X-API-Key"
		}
		req.Header.Set(name, secret)
	default:
		return ErrInvalid
	}
	return nil
}

// checkScheme allows https, and http only on loopback — the same rule the
// sync client keeps for a local mock server.
func checkScheme(u *url.URL) error {
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return ErrSchemeInvalid
	default:
		return ErrSchemeInvalid
	}
}

// CurlFor renders a request as cURL with the credential left as a shell
// variable. A command with the real token in it is a command that ends up in
// a chat, a ticket and a shell history.
func CurlFor(req Request, vars map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "curl -X %s", strings.ToUpper(req.Method))
	for key, value := range req.Headers {
		fmt.Fprintf(&b, " \\\n  -H %q", Interpolate(key, vars)+": "+Interpolate(value, vars))
	}
	switch req.Auth.Kind {
	case AuthBearer:
		fmt.Fprintf(&b, " \\\n  -H %q", "Authorization: Bearer $DEVKIT_TOKEN")
	case AuthBasic:
		fmt.Fprintf(&b, " \\\n  -u %q", "$DEVKIT_USER:$DEVKIT_PASSWORD")
	case AuthHeader:
		name := req.Auth.Header
		if name == "" {
			name = "X-API-Key"
		}
		fmt.Fprintf(&b, " \\\n  -H %q", name+": $DEVKIT_TOKEN")
	}
	if req.Body != "" {
		fmt.Fprintf(&b, " \\\n  --data %q", Interpolate(req.Body, vars))
	}
	fmt.Fprintf(&b, " \\\n  %q", Interpolate(req.URL, vars))
	return b.String()
}
