package git

import (
	"net/url"
	"strings"
)

// Transport is how git reaches a remote, which decides what can authenticate
// it: an HTTPS remote asks for a username and password (so DevKit can answer
// through GIT_ASKPASS), an SSH remote is answered by a key and never asks.
type Transport string

const (
	// TransportSSH is ssh://host/path and the scp-like git@host:path.
	TransportSSH Transport = "ssh"
	// TransportHTTP is http:// and https://.
	TransportHTTP Transport = "http"
	// TransportLocal is a path or file:// — a remote on this machine.
	TransportLocal Transport = "local"
	// TransportOther is anything else (git://, a custom helper). Nothing in
	// DevKit authenticates it.
	TransportOther Transport = "other"
)

// transportOf classifies a remote URL the way git does.
//
// The scp-like form has no scheme at all, so it is told apart by its shape:
// a colon before the first slash. A single letter before the colon is a
// Windows drive (C:\repo), not a host.
func transportOf(raw string) Transport {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return TransportOther
	}
	if scheme, _, ok := strings.Cut(raw, "://"); ok {
		switch strings.ToLower(scheme) {
		case "ssh", "git+ssh", "ssh+git":
			return TransportSSH
		case "http", "https":
			return TransportHTTP
		case "file":
			return TransportLocal
		default:
			return TransportOther
		}
	}
	colon := strings.Index(raw, ":")
	slash := strings.Index(raw, "/")
	if colon > 1 && (slash == -1 || colon < slash) {
		return TransportSSH
	}
	return TransportLocal
}

// redactURL drops the password from a URL that carries one, so an address
// built with a user and a token in its authority part can be named in an error without naming
// the token. Anything that is not a URL is returned as it came.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, has := u.User.Password(); !has {
		return raw
	}
	u.User = url.User(u.User.Username())
	return u.String()
}

// Address is a remote URL read into the parts that decide which account fits:
// the host it lives on and who owns the repository there.
type Address struct {
	Transport Transport
	// Host is the lower-cased hostname, without user or port. Empty for a
	// local path.
	Host string
	// Owner is the first path segment — the user or organisation, or a
	// GitLab top-level group. Two accounts on one host usually split by it.
	// Lower-cased: those namespaces are case-insensitive. Empty when the URL
	// has no owner to speak of (a single segment, or a local path).
	Owner string
	// Repo is the last path segment without a trailing .git.
	Repo string
}

// ParseAddress reads a remote URL in any of the forms git accepts: https://,
// ssh://, the scp-like git@host:owner/repo.git, or a local path.
func ParseAddress(raw string) Address {
	raw = strings.TrimSpace(raw)
	address := Address{Transport: transportOf(raw)}
	var path string
	switch address.Transport {
	case TransportHTTP, TransportSSH:
		if strings.Contains(raw, "://") {
			u, err := url.Parse(raw)
			if err != nil {
				return address
			}
			address.Host, path = u.Hostname(), u.Path
		} else {
			// scp-like: [user@]host:path. The path is relative to the home
			// directory on the server, and may or may not start with a slash.
			authority, rest, _ := strings.Cut(raw, ":")
			if at := strings.LastIndex(authority, "@"); at >= 0 {
				authority = authority[at+1:]
			}
			address.Host, path = authority, rest
		}
	default:
		return address
	}
	address.Host = strings.ToLower(strings.TrimSuffix(address.Host, "."))
	var segments []string
	for _, segment := range strings.Split(path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) > 0 {
		address.Repo = strings.TrimSuffix(segments[len(segments)-1], ".git")
	}
	if len(segments) >= 2 {
		address.Owner = strings.ToLower(segments[0])
	}
	return address
}
