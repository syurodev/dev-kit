package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// verifyTimeout bounds the probe. A credential check that hangs is a New
// Account dialog that hangs.
const verifyTimeout = 10 * time.Second

// verifyBodyLimit is how much of the response is read. Identity endpoints
// answer in bytes; anything larger is not an answer to this question.
const verifyBodyLimit = 64 << 10

// ErrNotVerifiable is a provider with no way to check itself — the connection
// providers, whose real check is the owning module's Test Connection.
var ErrNotVerifiable = errors.New("accounts.notVerifiable")

// Verify asks the provider who this credential belongs to.
//
// It is the difference between a credential store and an account manager: an
// account is not "saved", it is "verified as syurodev with scopes repo and
// read:org". A failure is recorded rather than thrown away — a token that
// comes back 401 is revoked, and saying so is the whole point.
func (s *Service) Verify(ctx context.Context, id string) (Account, error) {
	record, err := s.items.Get(id)
	if err != nil {
		return Account{}, ErrNotFound
	}
	account, err := decodeMetadata(record)
	if err != nil {
		return Account{}, err
	}
	provider, ok := s.providers.Get(account.Provider)
	if !ok || !provider.Verifiable() {
		return s.decorate(account), ErrNotVerifiable
	}
	if account.Kind == KindExternal {
		// There is nothing here to check: the row records that an identity
		// exists somewhere DevKit deliberately does not hold it.
		return s.decorate(account), ErrNotVerifiable
	}
	plaintext, err := s.items.OpenPayload(record, KeyID)
	if err != nil {
		return Account{}, err
	}
	user, secret := splitCredential(string(plaintext))

	result, probeErr := probe(ctx, s.verifyClient(), provider.Verify, user, secret)
	m := metadataOf(account)
	switch {
	case probeErr == nil:
		m.Subject = result.subject
		m.Scopes = result.scopes
		m.State = string(StateValid)
		m.VerifiedAt = formatTime(time.Now())
		if !result.expiresAt.IsZero() {
			m.ExpiresAt = formatTime(result.expiresAt)
		}
	case errors.Is(probeErr, errRejected):
		// The provider says no. That is an answer, and a useful one.
		m.State = string(StateRevoked)
		m.VerifiedAt = formatTime(time.Now())
	default:
		// The network, not the credential. Nothing is learned, so nothing is
		// recorded: calling an unreachable provider "revoked" would be a lie
		// that persists after the wifi comes back.
		return s.decorate(account), probeErr
	}

	raw, err := json.Marshal(m)
	if err != nil {
		return Account{}, err
	}
	if _, err := s.items.UpdateMetadata(id, raw); err != nil {
		return Account{}, err
	}
	return s.decorate(m.account(id, record.CreatedAt, time.Now())), nil
}

// errRejected is the provider refusing the credential, as opposed to the
// request failing to get there.
var errRejected = errors.New("accounts.rejected")

type probeResult struct {
	subject   string
	scopes    []string
	expiresAt time.Time
}

func probe(ctx context.Context, client *http.Client, spec VerifySpec, user, secret string) (probeResult, error) {
	target, err := url.Parse(spec.URL)
	if err != nil {
		return probeResult{}, ErrInvalid
	}
	// https only, and no loopback exception: a verify endpoint is a real
	// provider's, and a credential is not worth sending in the clear.
	if target.Scheme != "https" {
		return probeResult{}, ErrInvalid
	}
	if client == nil {
		return probeResult{}, ErrInvalid
	}

	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return probeResult{}, err
	}
	if spec.Header != "" {
		value := secret
		if spec.HeaderFormat != "" {
			value = fmt.Sprintf(spec.HeaderFormat, secret)
		}
		req.Header.Set(spec.Header, value)
	}
	if user != "" && spec.Header == "" {
		req.SetBasicAuth(user, secret)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return probeResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return probeResult{}, errRejected
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return probeResult{}, fmt.Errorf("accounts: verify returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, verifyBodyLimit))
	if err != nil {
		return probeResult{}, err
	}
	out := probeResult{}
	if spec.SubjectField != "" {
		var fields map[string]any
		if err := json.Unmarshal(body, &fields); err == nil {
			if value, ok := fields[spec.SubjectField].(string); ok {
				out.subject = value
			}
		}
	}
	if spec.ScopesHeader != "" {
		if raw := resp.Header.Get(spec.ScopesHeader); raw != "" {
			for _, scope := range strings.Split(raw, ",") {
				if scope = strings.TrimSpace(scope); scope != "" {
					out.scopes = append(out.scopes, scope)
				}
			}
		}
	}
	// A provider that reports an expiry in a header saves the user typing it.
	if raw := resp.Header.Get("GitHub-Authentication-Token-Expiration"); raw != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05 MST", "2006-01-02"} {
			if parsed, err := time.Parse(layout, raw); err == nil {
				out.expiresAt = parsed
				break
			}
		}
	}
	return out, nil
}

// defaultVerifyClient refuses a redirect that leaves the provider's host: it
// would carry the credential somewhere the registry never named.
func defaultVerifyClient() *http.Client {
	return &http.Client{
		Timeout: verifyTimeout,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) > 0 && r.URL.Hostname() != via[0].URL.Hostname() {
				return errors.New("accounts: verify redirected to another host")
			}
			return nil
		},
	}
}

// verifyClient is the client probes go through. SetVerifyClient replaces it so
// a test can point a provider at an httptest TLS server without the package
// growing a "skip TLS" flag that could ever be true in production.
func (s *Service) verifyClient() *http.Client {
	if s.http != nil {
		return s.http
	}
	return defaultVerifyClient()
}

// SetVerifyClient overrides the HTTP client used for verification.
func (s *Service) SetVerifyClient(client *http.Client) { s.http = client }
