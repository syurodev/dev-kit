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
	"strconv"
	"strings"
	"time"
)

const (
	githubAPI         = "https://api.github.com"
	maxResponseBytes  = 4 << 20
	maxPages          = 3
	maxFailingChecks  = 10
	summaryClip       = 240
	defaultConcurrent = 4
)

// Stats counts what the HTTP layer did, so a test (and a diagnosis) can see
// that polling an unchanged change request was answered with 304s.
type Stats struct {
	Requests    int
	NotModified int
}

// get is a conditional GET decoded into out. The second result is the Link
// header's next page, "" when there is none.
func (g *GitHub) get(ctx context.Context, c Credential, path string, query url.Values, out any) (string, error) {
	target := g.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return g.getURL(ctx, c, target, out)
}

func (g *GitHub) getURL(ctx context.Context, c Credential, target string, out any) (string, error) {
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
		return "", httpError(resp, body)
	}
	next := nextLink(resp.Header.Get("Link"))
	if etag := resp.Header.Get("ETag"); etag != "" {
		g.cache.put(etagEntry{key: key, etag: etag, body: body, next: next})
	}
	return next, decode(body, out)
}

// send is a write: POST, PATCH, PUT or DELETE with a JSON body, never cached.
func (g *GitHub) send(ctx context.Context, c Credential, method, path string, in, out any) error {
	var payload io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return &Error{Kind: ErrInvalid, Message: err.Error()}
		}
		payload = bytes.NewReader(raw)
	}
	req, err := g.newRequest(ctx, c, method, g.base+path, payload)
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
		return httpError(resp, body)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent || len(body) == 0 {
		return nil
	}
	return decode(body, out)
}

func (g *GitHub) newRequest(ctx context.Context, c Credential, method, target string, body io.Reader) (*http.Request, error) {
	if strings.TrimSpace(c.Token) == "" {
		return nil, &Error{Kind: ErrUnauthorized, Message: "no token"}
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, &Error{Kind: ErrInvalid, Message: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "DevKit")
	return req, nil
}

// do runs a request under the concurrency limit and reads a bounded body. The
// error it returns never contains the token: net/http puts the URL in its
// errors, and the URL has none.
func (g *GitHub) do(req *http.Request) (*http.Response, []byte, error) {
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	case <-req.Context().Done():
		return nil, nil, req.Context().Err()
	}
	resp, err := g.client.Do(req)
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
	return resp, body, nil
}

func decode(body []byte, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &Error{Kind: ErrUnavailable, Message: "the host's answer was not understood"}
	}
	return nil
}

// httpError turns a failed response into an *Error with the host's own words.
func httpError(resp *http.Response, body []byte) error {
	var payload struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Field   string `json:"field"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(body, &payload)
	message := strings.TrimSpace(payload.Message)
	for _, detail := range payload.Errors {
		extra := detail.Message
		if extra == "" && detail.Code != "" {
			extra = detail.Field + " " + detail.Code
		}
		if extra != "" {
			message += " (" + strings.TrimSpace(extra) + ")"
		}
	}
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	e := &Error{Status: resp.StatusCode, Message: message}
	lower := strings.ToLower(message)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		e.Kind = ErrUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0",
		resp.StatusCode == http.StatusForbidden && strings.Contains(lower, "rate limit"):
		e.Kind = ErrRateLimited
		e.RetryAt = retryAt(resp)
	case resp.StatusCode == http.StatusForbidden:
		e.Kind = ErrForbidden
		if sso := resp.Header.Get("X-GitHub-SSO"); sso != "" {
			e.Message += " — the token needs to be authorised for this organisation's SAML SSO"
		}
	case resp.StatusCode == http.StatusNotFound:
		e.Kind = ErrNotFound
	case resp.StatusCode == http.StatusUnprocessableEntity,
		resp.StatusCode == http.StatusMethodNotAllowed,
		resp.StatusCode == http.StatusConflict:
		e.Kind = ErrValidation
	case resp.StatusCode >= 500:
		e.Kind = ErrUnavailable
	default:
		e.Kind = ErrUnavailable
	}
	return e
}

// retryAt is when a limit lifts: Retry-After (seconds) wins over the reset time.
func retryAt(resp *http.Response) time.Time {
	if after := resp.Header.Get("Retry-After"); after != "" {
		if seconds, err := strconv.Atoi(after); err == nil {
			return time.Now().Add(time.Duration(seconds) * time.Second)
		}
	}
	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		if unix, err := strconv.ParseInt(reset, 10, 64); err == nil {
			return time.Unix(unix, 0)
		}
	}
	return time.Time{}
}

// nextLink reads rel="next" out of a Link header.
func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		segments := strings.Split(part, ";")
		if len(segments) < 2 {
			continue
		}
		for _, attr := range segments[1:] {
			if strings.TrimSpace(attr) == `rel="next"` {
				return strings.Trim(strings.TrimSpace(segments[0]), "<>")
			}
		}
	}
	return ""
}

func clip(text string, n int) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) <= n {
		return text
	}
	return string([]rune(text)[:n]) + "…"
}

func repoPath(repo Repo) string {
	owner, name, _ := strings.Cut(repo.Path, "/")
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

func validRepo(repo Repo) error {
	owner, name, ok := strings.Cut(repo.Path, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return &Error{Kind: ErrInvalid, Message: "not a GitHub repository path: " + repo.Path}
	}
	return nil
}

func numberPath(number int) (string, error) {
	if number <= 0 {
		return "", &Error{Kind: ErrInvalid, Message: fmt.Sprintf("not a pull request number: %d", number)}
	}
	return strconv.Itoa(number), nil
}
