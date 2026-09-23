package workspace

import (
	"net/url"
	"strings"
)

// OutboundTarget is what the analyzer could read of where one outbound HTTP
// call goes, before anything is known about the other services — the
// "(http method, path, target service)" workspace.mdx § Outbound edge asks
// for, still in source form.
type OutboundTarget struct {
	// ServiceName is @FeignClient's name/value: a logical service name the
	// client's load balancer looks up. May hold ${...} placeholders.
	ServiceName string
	// URL is the call's URL as far as the analyzer could fold it: literals
	// and constants joined, @Value fields as "${key}", and "{expr}" for a
	// part it could not follow (a method parameter, a runtime value).
	// For a Feign client, its url attribute joined with the method path.
	URL string
}

// TargetService is one service of the workspace, as the resolver needs it.
type TargetService struct {
	Name string
	Path string
	// Port is the service's own server.port ("8080" by default, "" when it
	// could not be folded) — what a localhost URL in a caller's dev config
	// is matched against.
	Port string
}

// Resolution statuses. The first is the only one naming a service; the rest
// are the service-level half of the join statuses in workspace.mdx § Join.
const (
	TargetResolved         = "resolved"
	TargetDynamicURL       = "dynamic-url"
	TargetUnknownService   = "unknown-service"
	TargetAmbiguousService = "ambiguous-service"
	TargetExternal         = "external"
)

// Provenance says how a resolved target was reached, so a reader can weigh
// it: a Feign name or a literal URL is what the code says, a property is
// what this machine's copy of the config says, and a port match is a
// guess from a dev setup.
const (
	ProvenanceFeignName = "feign-name"
	ProvenanceLiteral   = "literal"
	ProvenanceProperty  = "property"
	ProvenancePort      = "port"
)

// TargetResolution is where an OutboundTarget goes, or why that is unknown.
type TargetResolution struct {
	Status string
	// Service is the matched service's path — its identity in the registry
	// (names are not unique; see workspace.mdx § Data model). Set only when
	// Status is TargetResolved.
	Service string
	// Candidates are every service's path the host matched, when Status is
	// TargetAmbiguousService.
	Candidates []string
	// Host is the host the target named, after folding — what the UI shows
	// for unknown-service and external.
	Host string
	// Path is the route on the target, without query string; "{...}" holes
	// the analyzer left stay in it for the route matcher.
	Path       string
	Provenance string
}

// ResolveTarget works out which workspace service an outbound call reaches,
// folding placeholders against the CALLER's own config.
//
// Order of evidence, strongest first: a URL that folds to a host wins over a
// Feign name, because Feign itself lets url override name; a Feign name is
// then matched as it is; a host is matched by service name — exactly, or by
// its first DNS label so "billing.prod.svc.cluster.local" and
// "billing:8080" reach "billing" — and a loopback host by port against each
// service's server.port. Nothing is guessed past that: a host part that does
// not fold is dynamic-url, never the nearest-looking service.
func ResolveTarget(t OutboundTarget, caller SpringConfig, services []TargetService) TargetResolution {
	if strings.TrimSpace(t.URL) != "" {
		folded, complete := caller.Resolve(t.URL)
		provenance := ProvenanceLiteral
		if folded != t.URL {
			provenance = ProvenanceProperty
		}
		host, port, path, ok := splitTargetURL(folded, complete)
		if ok {
			return resolveHost(host, port, path, provenance, services)
		}
		if t.ServiceName == "" {
			return TargetResolution{Status: TargetDynamicURL, Path: pathOnly(folded)}
		}
		// A Feign client whose url= did not fold still has its name.
	}
	if name := strings.TrimSpace(t.ServiceName); name != "" {
		folded, complete := caller.Resolve(name)
		if !complete {
			return TargetResolution{Status: TargetDynamicURL}
		}
		provenance := ProvenanceFeignName
		if folded != name {
			provenance = ProvenanceProperty
		}
		path := ""
		if t.URL != "" {
			path = pathOnly(t.URL)
		}
		r := matchByName(strings.ToLower(strings.TrimSpace(folded)), services)
		r.Path, r.Provenance, r.Host = path, provenance, folded
		if r.Status == TargetExternal {
			// A Feign name is a service-discovery name by definition, never
			// an outside host.
			r.Status = TargetUnknownService
		}
		return r
	}
	return TargetResolution{Status: TargetDynamicURL}
}

// splitTargetURL takes a folded URL apart. ok is false when the host part is
// not known: a hole or an unresolved placeholder before the path, or no
// scheme at all — a relative "/refunds" means the base URL lives in a
// builder or a rootUri the analyzer did not see.
func splitTargetURL(folded string, complete bool) (host, port, path string, ok bool) {
	schemeEnd := strings.Index(folded, "://")
	if schemeEnd <= 0 {
		return "", "", "", false
	}
	rest := folded[schemeEnd+3:]
	authorityEnd := strings.IndexAny(rest, "/?#")
	authority, remainder := rest, ""
	if authorityEnd >= 0 {
		authority, remainder = rest[:authorityEnd], rest[authorityEnd:]
	}
	if authority == "" || strings.ContainsAny(authority, "{}$") {
		return "", "", "", false
	}
	if !complete && strings.Contains(folded[:schemeEnd], "$") {
		return "", "", "", false
	}
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		authority = authority[at+1:]
	}
	host = authority
	if u, err := url.Parse("x://" + authority); err == nil {
		host, port = u.Hostname(), u.Port()
	}
	return strings.ToLower(host), port, pathOnly(remainder), true
}

func pathOnly(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := strings.Index(s, "/"); j >= 0 {
			s = s[j:]
		} else {
			s = ""
		}
	}
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

func resolveHost(host, port, path, provenance string, services []TargetService) TargetResolution {
	if isLoopback(host) {
		if port == "" {
			return TargetResolution{Status: TargetUnknownService, Host: host, Path: path}
		}
		var matches []string
		for _, s := range services {
			if s.Port == port {
				matches = append(matches, s.Path)
			}
		}
		r := byMatches(matches, host+":"+port, path)
		if r.Status == TargetExternal {
			r.Status = TargetUnknownService // loopback is never outside
		}
		r.Provenance = ProvenancePort
		return r
	}
	r := matchByName(host, services)
	r.Path, r.Provenance = path, provenance
	return r
}

func matchByName(host string, services []TargetService) TargetResolution {
	var exact, label []string
	firstLabel := host
	if i := strings.Index(host, "."); i > 0 {
		firstLabel = host[:i]
	}
	for _, s := range services {
		name := strings.ToLower(s.Name)
		switch {
		case name == host:
			exact = append(exact, s.Path)
		case name == firstLabel:
			label = append(label, s.Path)
		}
	}
	if len(exact) > 0 {
		return byMatches(exact, host, "")
	}
	if len(label) > 0 {
		return byMatches(label, host, "")
	}
	r := TargetResolution{Status: TargetExternal, Host: host}
	if looksInternal(host) {
		r.Status = TargetUnknownService
	}
	return r
}

func byMatches(matches []string, host, path string) TargetResolution {
	switch len(matches) {
	case 0:
		return TargetResolution{Status: TargetExternal, Host: host, Path: path}
	case 1:
		return TargetResolution{Status: TargetResolved, Service: matches[0], Host: host, Path: path}
	default:
		return TargetResolution{Status: TargetAmbiguousService, Candidates: matches, Host: host, Path: path}
	}
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "::1" || strings.HasPrefix(host, "127.") || host == "host.docker.internal"
}

// looksInternal is a host that can only be a name inside some cluster or
// service registry: no dot at all, or a cluster-local suffix. Those are
// unknown-service (a service this workspace does not hold) rather than
// external (the public internet).
func looksInternal(host string) bool {
	if !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range []string{".svc", ".svc.cluster.local", ".cluster.local", ".internal", ".local", ".consul"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
