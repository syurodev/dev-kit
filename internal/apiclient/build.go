package apiclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// userAgent is sent when the request names none. Go's own default reads as a
// script, and a gateway that rate-limits scripts is a 429 nobody can explain.
const userAgent = "DevKit-API-Client/1"

// contentTypeFor is the Content-Type a body mode goes out with when the
// request does not say. Without it a JSON body reaches Spring or Nest as
// text/plain and comes back 415 — the first thing "Try in API Client" did.
func contentTypeFor(mode BodyMode) string {
	switch mode {
	case BodyJSON:
		return "application/json"
	case BodyXML:
		return "application/xml"
	case BodyText:
		return "text/plain; charset=utf-8"
	case BodyURLEncoded:
		return "application/x-www-form-urlencoded"
	}
	return ""
}

// withScheme puts a scheme on an address typed without one, the way every
// client does for `localhost:8080/users`. This machine gets http — a local
// server almost never has a certificate — and anything else https.
func withScheme(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.Contains(trimmed, "://") || strings.HasPrefix(trimmed, "{{") {
		return trimmed
	}
	host := trimmed
	if cut := strings.IndexAny(host, "/?#"); cut >= 0 {
		host = host[:cut]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if isLoopback(strings.Trim(host, "[]")) {
		return "http://" + trimmed
	}
	return "https://" + trimmed
}

func isLoopback(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// targetURL is the address that goes out: variables filled, a scheme where
// none was typed, :path variables replaced, and the query rows appended in
// their own order after whatever query the URL already carried.
func targetURL(req Request, vars map[string]string) (*url.URL, error) {
	parsed, err := url.Parse(withScheme(Interpolate(req.URL, vars)))
	if err != nil {
		return nil, ErrInvalid
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrSchemeInvalid
	}
	if parsed.Host == "" {
		return nil, ErrInvalid
	}
	fillPathVars(parsed, req.PathVars, vars)
	if extra := encodeQuery(req.Query, vars); extra != "" {
		if parsed.RawQuery != "" {
			parsed.RawQuery += "&" + extra
		} else {
			parsed.RawQuery = extra
		}
	}
	return parsed, nil
}

// PathVarNames lists the :name segments of a URL's path, in order — what an
// editor shows a row for.
func PathVarNames(rawURL string) []string {
	path := rawURL
	if at := strings.Index(path, "://"); at >= 0 {
		path = path[at+3:]
		if slash := strings.IndexByte(path, '/'); slash >= 0 {
			path = path[slash:]
		} else {
			return nil
		}
	}
	if cut := strings.IndexAny(path, "?#"); cut >= 0 {
		path = path[:cut]
	}
	var names []string
	seen := map[string]bool{}
	for _, segment := range strings.Split(path, "/") {
		if len(segment) > 1 && segment[0] == ':' && !seen[segment[1:]] {
			names = append(names, segment[1:])
			seen[segment[1:]] = true
		}
	}
	return names
}

// fillPathVars replaces each whole :name segment that has a value. One with
// none stays as written, for the same reason Interpolate leaves {{name}}: a
// visible :id says what is missing and an empty segment does not.
func fillPathVars(u *url.URL, rows KVList, vars map[string]string) {
	if len(rows) == 0 {
		return
	}
	values := map[string]string{}
	for _, row := range rows.Active() {
		values[row.Key] = Interpolate(row.Value, vars)
	}
	segments := strings.Split(u.EscapedPath(), "/")
	changed := false
	for i, segment := range segments {
		if len(segment) < 2 || segment[0] != ':' {
			continue
		}
		if value, ok := values[segment[1:]]; ok && value != "" {
			segments[i] = url.PathEscape(value)
			changed = true
		}
	}
	if !changed {
		return
	}
	escaped := strings.Join(segments, "/")
	if path, err := url.PathUnescape(escaped); err == nil {
		u.Path, u.RawPath = path, escaped
	}
}

// encodeBody renders the body for its mode. The content type is returned
// separately because only the caller knows whether the request already set
// one — except multipart, whose boundary the request cannot know, so its
// type always wins.
func encodeBody(req Request, vars map[string]string) (io.Reader, string, error) {
	switch req.BodyMode {
	case BodyNone:
		return nil, "", nil
	case BodyURLEncoded:
		encoded := encodeQuery(req.Form, vars)
		if encoded == "" {
			return nil, contentTypeFor(BodyURLEncoded), nil
		}
		return strings.NewReader(encoded), contentTypeFor(BodyURLEncoded), nil
	case BodyMultipart:
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		for _, row := range req.Form.Active() {
			if err := writer.WriteField(Interpolate(row.Key, vars), Interpolate(row.Value, vars)); err != nil {
				return nil, "", err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, "", err
		}
		return &buf, writer.FormDataContentType(), nil
	default:
		body := Interpolate(req.Body, vars)
		if body == "" {
			return nil, "", nil
		}
		return strings.NewReader(body), contentTypeFor(req.BodyMode), nil
	}
}

// buildHTTP is the request as it leaves, credential aside: applyAuth adds that
// after the gate, so nothing here ever holds one.
func buildHTTP(ctx context.Context, req Request, target *url.URL, vars map[string]string) (*http.Request, error) {
	body, contentType, err := encodeBody(req, vars)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, target.String(), body)
	if err != nil {
		return nil, ErrInvalid
	}
	for _, row := range req.Headers.Active() {
		// Add, not Set: two rows with one name are two header lines, which is
		// what a person who typed two of them meant.
		httpReq.Header.Add(Interpolate(row.Key, vars), Interpolate(row.Value, vars))
	}
	if contentType != "" && (req.BodyMode == BodyMultipart || httpReq.Header.Get("Content-Type") == "") {
		httpReq.Header.Set("Content-Type", contentType)
	}
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", userAgent)
	}
	return httpReq, nil
}

// checkScheme decides what may go over plain http.
//
// https always. http to this machine always — a local server has no
// certificate, and nothing leaves. http to anywhere else is a dev server on
// the LAN or in a container, which is ordinary, so it is allowed with two
// exceptions: a request carrying a bound credential (the account would cross
// the network in the clear), and an environment whose tag asks before every
// send (if it matters enough to ask, it matters enough to encrypt).
func checkScheme(u *url.URL, gated, withCredential bool) error {
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
		if gated || withCredential {
			return ErrPlainHTTP
		}
		return nil
	default:
		return ErrSchemeInvalid
	}
}

// shellQuote quotes for a POSIX shell. Single quotes take everything
// literally — `$`, backticks, a newline — which is what a body needs; Go's %q
// does not, and its `\n` reaches the server as a backslash and an n.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CurlFor renders a request as cURL with the credential left as a shell
// variable. A command with the real token in it is a command that ends up in
// a chat, a ticket and a shell history.
//
// vars are the plain variables only; a secret one stays {{name}} in the
// output, for the same reason.
func CurlFor(req Request, vars map[string]string) string {
	req = req.normalized()
	var b strings.Builder
	target := Interpolate(req.URL, vars)
	if parsed, err := targetURL(req, vars); err == nil {
		target = parsed.String()
	}
	fmt.Fprintf(&b, "curl -X %s %s", req.Method, shellQuote(target))

	hasContentType := false
	for _, row := range req.Headers.Active() {
		key := Interpolate(row.Key, vars)
		if strings.EqualFold(key, "Content-Type") {
			hasContentType = true
		}
		fmt.Fprintf(&b, " \\\n  -H %s", shellQuote(key+": "+Interpolate(row.Value, vars)))
	}
	if ct := contentTypeFor(req.BodyMode); ct != "" && !hasContentType && req.BodyMode != BodyURLEncoded {
		fmt.Fprintf(&b, " \\\n  -H %s", shellQuote("Content-Type: "+ct))
	}
	// Double quotes on purpose: these are the one place the shell should
	// expand, so the person exports the value and the command never holds it.
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
	switch req.BodyMode {
	case BodyURLEncoded:
		for _, row := range req.Form.Active() {
			fmt.Fprintf(&b, " \\\n  --data-urlencode %s",
				shellQuote(Interpolate(row.Key, vars)+"="+Interpolate(row.Value, vars)))
		}
	case BodyMultipart:
		for _, row := range req.Form.Active() {
			// -F reads a leading @ or < as a file; --form-string never does.
			fmt.Fprintf(&b, " \\\n  --form-string %s",
				shellQuote(Interpolate(row.Key, vars)+"="+Interpolate(row.Value, vars)))
		}
	case BodyNone:
	default:
		if req.Body != "" {
			fmt.Fprintf(&b, " \\\n  --data-raw %s", shellQuote(Interpolate(req.Body, vars)))
		}
	}
	return b.String()
}
