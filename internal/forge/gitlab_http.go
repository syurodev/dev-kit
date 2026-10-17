package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// get is a GET decoded into out. The second result is the next page's address
// ("" when there is none).
//
// It is conditional the same way GitHub's is: an If-None-Match goes out only
// when the host itself sent an ETag for this address and token before, so a
// host that never honours it costs nothing but a cache entry, and one that does
// answers 304 for an unchanged body. Whether gitlab.com does for each endpoint
// is not verified — the poll budget in prwatch does not depend on it.
func (g *GitLab) get(ctx context.Context, c Credential, repo Repo, path string, query url.Values, out any) (string, error) {
	target := g.api(repo) + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return g.getURL(ctx, c, repo, target, out)
}

func (g *GitLab) getURL(ctx context.Context, c Credential, repo Repo, target string, out any) (string, error) {
	if err := g.sameHost(repo, target); err != nil {
		return "", err
	}
	key := etagKey(c.Token, target)
	cached, have := g.cache.get(key)
	req, err := g.newRequest(ctx, c, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	if have && cached.etag != "" {
		req.Header.Set("If-None-Match", cached.etag)
	}
	resp, body, err := g.do(req)
	if err != nil {
		return "", err
	}
	g.mu.Lock()
	g.stats.Requests++
	if resp.StatusCode == http.StatusNotModified {
		g.stats.NotModified++
	}
	g.mu.Unlock()
	if resp.StatusCode == http.StatusNotModified {
		if !have {
			return "", &Error{Kind: ErrUnavailable, Status: resp.StatusCode, Message: "the host answered 304 to a request that was not conditional"}
		}
		return cached.next, decode(cached.body, out)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", g.httpError(c, resp, body)
	}
	next := nextLink(resp.Header.Get("Link"))
	if next == "" {
		if page := resp.Header.Get("X-Next-Page"); page != "" {
			next = withPage(target, page)
		}
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		g.cache.put(etagEntry{key: key, etag: etag, body: body, next: next})
	}
	return next, decode(body, out)
}

// withPage is target with its page parameter set: GitLab's X-Next-Page
// without a Link header (it sends both, but a proxy may drop one).
func withPage(target, page string) string {
	u, err := url.Parse(target)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("page", page)
	u.RawQuery = q.Encode()
	return u.String()
}

// send is a write: POST, PUT or DELETE with a JSON body, never cached.
func (g *GitLab) send(ctx context.Context, c Credential, repo Repo, method, path string, in, out any) error {
	return g.sendTo(ctx, c, method, g.api(repo)+path, in, out)
}

func (g *GitLab) sendTo(ctx context.Context, c Credential, method, target string, in, out any) error {
	var payload io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return &Error{Kind: ErrInvalid, Message: err.Error()}
		}
		payload = bytes.NewReader(raw)
	}
	req, err := g.newRequest(ctx, c, method, target, payload)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, body, err := g.do(req)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.stats.Requests++
	g.mu.Unlock()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return g.httpError(c, resp, body)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent || len(body) == 0 {
		return nil
	}
	return decode(body, out)
}

func (g *GitLab) newRequest(ctx context.Context, c Credential, method, target string, body io.Reader) (*http.Request, error) {
	if strings.TrimSpace(c.Token) == "" {
		return nil, &Error{Kind: ErrUnauthorized, Message: "no token"}
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, &Error{Kind: ErrInvalid, Message: "not a usable address"}
	}
	req.Header.Set("PRIVATE-TOKEN", c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "DevKit")
	return req, nil
}

// do runs a request under the concurrency limit and reads a bounded body.
//
// A redirect is never followed: Go forwards a custom header such as
// PRIVATE-TOKEN to the redirect's target (it strips only Authorization and
// cookies), so a host that redirected an API call elsewhere would be handed the
// token. The 3xx comes back as an error instead. The errors this returns never
// contain the token: net/http puts the URL in its errors, and the URL has none.
func (g *GitLab) do(req *http.Request) (*http.Response, []byte, error) {
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	case <-req.Context().Done():
		return nil, nil, req.Context().Err()
	}
	client := *g.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, err
		}
		return nil, nil, &Error{Kind: ErrUnavailable, Message: "could not reach " + req.URL.Host}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, &Error{Kind: ErrUnavailable, Message: "the connection to " + req.URL.Host + " broke while reading"}
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.StatusCode != http.StatusNotModified {
		return nil, nil, &Error{Kind: ErrUnavailable, Status: resp.StatusCode,
			Message: req.URL.Host + " redirected the request; DevKit does not follow redirects with a token"}
	}
	return resp, body, nil
}

// httpError turns a failed response into an *Error with the host's own words.
// GitLab answers {"message": "..."}, {"message": {"field": ["..."]}},
// {"error": "..."} or a plain string, depending on the endpoint.
func (g *GitLab) httpError(c Credential, resp *http.Response, body []byte) error {
	message := glMessage(body)
	if len(c.Token) >= 8 {
		// A host that echoes the credential back must not get it into a log or
		// an agent's context through us.
		message = strings.ReplaceAll(message, c.Token, "[token]")
	}
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	e := &Error{Status: resp.StatusCode, Message: clip(message, 400)}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		e.Kind = ErrUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && resp.Header.Get("RateLimit-Remaining") == "0":
		e.Kind = ErrRateLimited
		e.RetryAt = glRetryAt(resp)
	case resp.StatusCode == http.StatusForbidden:
		e.Kind = ErrForbidden
	case resp.StatusCode == http.StatusNotFound:
		e.Kind = ErrNotFound
	case resp.StatusCode == http.StatusBadRequest,
		resp.StatusCode == http.StatusMethodNotAllowed, // merge: not mergeable now (draft, unresolved threads, pipeline)
		resp.StatusCode == http.StatusNotAcceptable,    // merge: the branch cannot be merged
		resp.StatusCode == http.StatusConflict,         // merge: the sha no longer matches; create: already exists
		resp.StatusCode == http.StatusUnprocessableEntity:
		e.Kind = ErrValidation
	default:
		e.Kind = ErrUnavailable
	}
	return e
}

// glMessage reads the host's explanation out of whichever shape it came in.
func glMessage(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		// Not JSON (a proxy's page, a plain string): nothing safe to quote.
		var s string
		if json.Unmarshal(body, &s) == nil {
			return strings.TrimSpace(s)
		}
		return ""
	}
	var parts []string
	for _, key := range []string{"message", "error", "error_description"} {
		if v, ok := payload[key]; ok {
			if text := flatten(v); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, " — ")
}

func flatten(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []any:
		var parts []string
		for _, item := range t {
			if text := flatten(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			if text := flatten(t[k]); text != "" {
				parts = append(parts, k+" "+text)
			}
		}
		return strings.Join(parts, "; ")
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

// glRetryAt is when a limit lifts: Retry-After (seconds) wins over RateLimit-Reset
// (a Unix time).
func glRetryAt(resp *http.Response) time.Time {
	if after := resp.Header.Get("Retry-After"); after != "" {
		if seconds, err := strconv.Atoi(after); err == nil {
			return time.Now().Add(time.Duration(seconds) * time.Second)
		}
	}
	if reset := resp.Header.Get("RateLimit-Reset"); reset != "" {
		if unix, err := strconv.ParseInt(reset, 10, 64); err == nil {
			return time.Unix(unix, 0)
		}
	}
	return time.Time{}
}
