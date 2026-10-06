package apiclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
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
	history *historyStore
	client  *http.Client

	// OnChanged is told after any write to a request, collection,
	// environment or the globals, whoever made it — so the window follows an
	// agent creating a collection over MCP without being refreshed by hand.
	// Set during startup wiring; nil is fine.
	OnChanged func()
}

func NewService(items ItemStore, envs EnvLookup) *Service {
	s := &Service{
		envs: envs,
		client: &http.Client{
			Timeout:       DefaultTimeout,
			CheckRedirect: noRedirectToAnotherOrigin,
		},
	}
	// Every write in this package goes through the item store, so watching
	// the store is the one place that cannot miss a method added later.
	s.items = notifyingStore{ItemStore: items, changed: s.changed}
	return s
}

func (s *Service) changed() {
	if s.OnChanged != nil {
		s.OnChanged()
	}
}

// notifyingStore reports each successful write to the store it wraps.
type notifyingStore struct {
	ItemStore
	changed func()
}

func (n notifyingStore) Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error) {
	record, err := n.ItemStore.Create(itemType, keyID, metadata, secret)
	if err == nil {
		n.changed()
	}
	return record, err
}

func (n notifyingStore) UpdateMetadata(id string, metadata json.RawMessage) (vaultitem.Record, error) {
	record, err := n.ItemStore.UpdateMetadata(id, metadata)
	if err == nil {
		n.changed()
	}
	return record, err
}

func (n notifyingStore) Update(id, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error) {
	record, err := n.ItemStore.Update(id, keyID, metadata, secret)
	if err == nil {
		n.changed()
	}
	return record, err
}

func (n notifyingStore) Delete(id string) error {
	err := n.ItemStore.Delete(id)
	if err == nil {
		n.changed()
	}
	return err
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

// UpdateEnvironment renames an environment, moves it to another tag and
// replaces its variables. The tag is checked like a new one: it is what
// decides whether sending asks first.
func (s *Service) UpdateEnvironment(id, name, env string, update VarsUpdate) (Environment, error) {
	if strings.TrimSpace(name) == "" || env == "" {
		return Environment{}, ErrInvalid
	}
	if s.envs != nil && !s.envs.Exists(env) {
		return Environment{}, ErrInvalid
	}
	record, err := s.items.Get(id)
	if err != nil || record.Type != vaultitem.ItemTypeAPIEnvironment {
		return Environment{}, ErrNotFound
	}
	current, err := s.secretsOf(record)
	if err != nil {
		return Environment{}, err
	}
	vars, secrets, err := update.apply(current)
	if err != nil {
		return Environment{}, err
	}
	raw, err := json.Marshal(environmentMetadata{Name: name, Env: env, Vars: vars})
	if err != nil {
		return Environment{}, err
	}
	payload, err := json.Marshal(secrets)
	if err != nil {
		return Environment{}, err
	}
	if _, err := s.items.Update(id, KeyIDEnvironment, raw, payload); err != nil {
		return Environment{}, err
	}
	return Environment{ID: id, Name: name, Env: env, Vars: vars, SecretVars: sortedKeys(secrets)}, nil
}

// DuplicateEnvironment copies an environment, secrets included — inside Go,
// so the copy is made without any value crossing the bridge.
func (s *Service) DuplicateEnvironment(id, name string) (Environment, error) {
	record, err := s.items.Get(id)
	if err != nil || record.Type != vaultitem.ItemTypeAPIEnvironment {
		return Environment{}, ErrNotFound
	}
	var meta environmentMetadata
	if err := decodeInto(record.Metadata, &meta); err != nil {
		return Environment{}, err
	}
	secrets, err := s.secretsOf(record)
	if err != nil {
		return Environment{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = meta.Name + " copy"
	}
	return s.CreateEnvironment(name, meta.Env, meta.Vars, secrets)
}

// DeleteEnvironment removes an environment. Requests do not point at one —
// the environment is picked per send — so nothing is left dangling.
func (s *Service) DeleteEnvironment(id string) error {
	record, err := s.items.Get(id)
	if err != nil || record.Type != vaultitem.ItemTypeAPIEnvironment {
		return ErrNotFound
	}
	return s.items.Delete(id)
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
	return openSecrets(s.items, record, KeyIDEnvironment)
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
	req = req.normalized()
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.URL) == "" {
		return Request{}, ErrInvalid
	}
	meta := requestMetadata{
		CollectionID: req.CollectionID, Name: req.Name,
		Method: req.Method, URLTemplate: req.URL,
		AccountID: req.Auth.AccountID, OriginRef: req.OriginRef,
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return Request{}, err
	}
	payload, err := json.Marshal(requestPayload{
		Query: req.Query, Headers: req.Headers, PathVars: req.PathVars,
		BodyMode: req.BodyMode, Body: req.Body, Form: req.Form, Auth: req.Auth,
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
	if err != nil || record.Type != vaultitem.ItemTypeAPIRequest {
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
	req.Query, req.Headers, req.PathVars = body.Query, body.Headers, body.PathVars
	req.BodyMode, req.Body, req.Form, req.Auth = body.BodyMode, body.Body, body.Form, body.Auth
	return req.normalized(), nil
}

// DeleteRequest removes a request and what this machine remembers sending it.
func (s *Service) DeleteRequest(id string) error {
	if err := s.items.Delete(id); err != nil {
		return err
	}
	if id != "" {
		_ = s.ClearHistory(id)
	}
	return nil
}

// ---- sending ---------------------------------------------------------

// SendInput is one send.
type SendInput struct {
	Request Request
	// EnvironmentID is required: the gate needs an environment to read, so a
	// send with none is a send nobody could have gated.
	EnvironmentID string
	// ForAgent bounds the response harder and is what the MCP path passes.
	ForAgent bool
	// Adhoc is a request an agent wrote itself rather than one a person
	// saved. Its address is the agent's choice, so a credential or a secret
	// variable in it is a person's call every time, on any environment.
	Adhoc bool
}

// Send resolves variables, resolves the credential, gates on the environment
// and makes the request.
//
// The request is whatever the caller holds — a saved one, or an editor's
// unsaved state — and variables come from its collection, so an edit is sent
// as typed without saving first.
func (s *Service) Send(ctx context.Context, in SendInput) (Response, error) {
	if in.EnvironmentID == "" {
		return Response{}, ErrEnvRequired
	}
	req := in.Request.normalized()
	scope, err := s.resolve(req.CollectionID, in.EnvironmentID)
	if err != nil {
		return Response{}, err
	}
	vars := scope.vars

	target, err := targetURL(req, vars)
	if err != nil {
		return Response{}, err
	}
	gated := s.envs != nil && s.envs.RequiresConfirmation(scope.envTag)
	withCredential := req.Auth.Kind != AuthNone && req.Auth.AccountID != ""
	if err := checkScheme(target, gated, withCredential); err != nil {
		return Response{}, err
	}

	// From here the URL is known, so every outcome gets a line — including the
	// two where nothing leaves. A history that lists only what succeeded
	// cannot answer "did I actually send that".
	sentAt := time.Now()
	note := func(status, bytes int, duration int64, failure string) {
		s.record(HistoryEntry{
			RequestID: req.ID, EnvironmentID: in.EnvironmentID,
			Method: req.Method, URL: target.String(),
			Status: status, Bytes: bytes, DurationMS: duration,
			Failure: failure, SentAt: sentAt,
		})
	}

	// An agent's own request carrying a secret: the agent chose the host, so
	// "send the token there" is not something the environment's tag can have
	// already answered. Saved requests are the person's, and keep the tag's rule.
	detail := "Send " + req.Method + " " + target.Path + "?"
	ask := gated
	if in.Adhoc {
		var carries []string
		if withCredential {
			carries = append(carries, "the account "+req.Auth.AccountID)
		}
		for _, name := range secretsUsed(req, scope) {
			carries = append(carries, "the secret {{"+name+"}}")
		}
		if len(carries) > 0 {
			ask = true
			detail = "An agent wants to send " + strings.Join(carries, ", ") + " to " + target.Host + " — " + req.Method + " " + target.Path + ". Allow?"
		}
	}

	// The gate comes before anything leaves: env wins, every time, with no
	// allow-always — the same rule the MCP confirmation policy already keeps.
	if ask {
		if s.confirm == nil {
			return Response{}, ErrNotAllowed
		}
		allowed, err := s.confirm.Confirm(ctx,
			req.Method+" "+target.Host+" ("+scope.envTag+")",
			detail)
		if err != nil {
			return Response{}, err
		}
		if !allowed {
			note(0, 0, 0, "refused at the confirmation")
			return Response{}, ErrNotAllowed
		}
	}

	httpReq, err := buildHTTP(ctx, req, target, vars)
	if err != nil {
		return Response{}, err
	}
	if err := s.applyAuth(ctx, httpReq, req.Auth, scope.envTag); err != nil {
		return Response{}, err
	}

	started := time.Now()
	resp, err := s.client.Do(httpReq)
	if err != nil {
		failure := err.Error()
		if ctx.Err() != nil {
			failure = "cancelled"
		}
		note(0, 0, time.Since(started).Milliseconds(), failure)
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
	duration := time.Since(started).Milliseconds()
	note(resp.StatusCode, len(raw), duration, "")
	out := Response{
		Status: resp.StatusCode, StatusText: resp.Status,
		Body: string(raw), Truncated: truncated, Bytes: len(raw),
		DurationMS: duration,
	}
	out.Headers, out.HeaderList = responseHeaders(resp.Header)
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if location, locErr := resp.Location(); locErr == nil && originOf(location) != originOf(resp.Request.URL) {
			out.RedirectBlocked = location.String()
		}
	}
	if in.ForAgent {
		out = out.ForAgent()
	}
	return out, nil
}

// secretsUsed names the secret variables a request would fill in, anywhere it
// can hold one.
func secretsUsed(req Request, scope resolvedScope) []string {
	texts := []string{req.URL, req.Body}
	for _, rows := range []KVList{req.Query, req.Headers, req.PathVars, req.Form} {
		for _, row := range rows.Active() {
			texts = append(texts, row.Key, row.Value)
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, text := range texts {
		for _, match := range varPattern.FindAllStringSubmatch(text, -1) {
			name := match[1]
			if scope.secret[name] && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// responseHeaders is both shapes of the response's headers.
func responseHeaders(header http.Header) (map[string]string, KVList) {
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	sort.Strings(names)
	joined := make(map[string]string, len(names))
	var list KVList
	for _, name := range names {
		values := header.Values(name)
		joined[name] = strings.Join(values, ", ")
		for _, value := range values {
			list = append(list, KV{Key: name, Value: value})
		}
	}
	return joined, list
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
