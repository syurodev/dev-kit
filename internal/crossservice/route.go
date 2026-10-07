package crossservice

import "strings"

// A route is compared segment by segment. A variable segment — {id}, {*}, :id —
// stands for any one segment, on either side: the endpoint's {org} matches a
// literal the caller wrote, and the caller's {org} (a value only its runtime
// knows) matches whatever the endpoint wants there. Two literals must be equal.

// segments splits a path into its segments, dropping the empty ones a leading,
// trailing or doubled slash leaves.
func segments(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isVariable(segment string) bool {
	return strings.Contains(segment, "{") || strings.HasPrefix(segment, ":") || strings.Contains(segment, "${")
}

// isCatchAll is the endpoint segment that swallows the rest: {*path}, **, {path:.*}.
func isCatchAll(segment string) bool {
	return segment == "**" || segment == "*" || strings.HasPrefix(segment, "{*") || strings.Contains(segment, ":.*}") || strings.Contains(segment, ":.+}")
}

// match reports whether a call to callPath can land on endpointPath, and how
// specifically: score is the number of literal segments that matched exactly,
// so /api/users/me beats /api/users/{id} for the call /api/users/me.
func match(callPath, endpointPath string) (score int, ok bool) {
	call, endpoint := segments(callPath), segments(endpointPath)
	for i, e := range endpoint {
		if isCatchAll(e) {
			return score, i <= len(call)
		}
		if i >= len(call) {
			return 0, false
		}
		c := call[i]
		switch {
		case isVariable(c) || isVariable(e):
			// Either side leaves it open.
		case c == e:
			score++
		default:
			return 0, false
		}
	}
	return score, len(call) == len(endpoint)
}

// literalSegments counts the segments of a path that are fixed text. A path
// of fewer than two says too little to identify a service on its own: "/health"
// is every service's.
func literalSegments(path string) int {
	n := 0
	for _, s := range segments(path) {
		if !isVariable(s) {
			n++
		}
	}
	return n
}

// callPath is the route a call's URL asks for: the part after the host, query
// and fragment cut. A URL that opens with a hole — {conf.access_url}/v1/x, the
// base URL a plugin's config supplies — has its host unknown and its path read
// from the first slash after the hole. "" when no path can be told.
func callPath(url string) string {
	u := url
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[j:]
		}
		return ""
	}
	if strings.HasPrefix(u, "/") {
		return u
	}
	if strings.HasPrefix(u, "{") || strings.HasPrefix(u, "${") {
		depth := 0
		for i := 0; i < len(u); i++ {
			switch u[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					rest := u[i+1:]
					if strings.HasPrefix(rest, "/") {
						return rest
					}
					return ""
				}
			}
		}
	}
	return ""
}

// withContextPath prefixes an endpoint's route with its service's context path.
func withContextPath(contextPath, target string) string {
	if contextPath == "" {
		return target
	}
	return strings.TrimRight(contextPath, "/") + "/" + strings.TrimLeft(target, "/")
}
