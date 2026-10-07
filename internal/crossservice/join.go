// Package crossservice joins the HTTP calls one service makes to the routes
// another serves, across a workspace.
//
// Each service's index says only what is true of that service: the routes it
// serves, the calls it makes. The edge between two services exists in neither,
// so it is computed here, when asked, from their snapshots. What cannot be
// joined is not dropped: it carries a status that says why (see the Status
// constants), because a guessed edge is worse than an honest "unknown".
package crossservice

import (
	"sort"
	"strings"

	"devkit/internal/codegraph"
	"devkit/internal/workspace"
)

// Join statuses. linked is the only one that names a route; the rest say why
// an edge stops short.
const (
	StatusLinked           = "linked"
	StatusEndpointGone     = "endpoint-gone"     // the service is known, no route of it matches
	StatusDynamicURL       = "dynamic-url"       // the host (or the whole URL) is only known at run time
	StatusUnknownService   = "unknown-service"   // a host that looks internal, in no service of the workspace
	StatusAmbiguousService = "ambiguous-service" // the host matches more than one service
	StatusAmbiguousPath    = "ambiguous-path"    // no host to go by, and several services serve the route
	StatusNotIndexed       = "not-indexed"       // the service is known, its index is not there to read
	StatusExternal         = "external"          // a host outside the workspace
)

// How a link was reached, weakest last. The first four are workspace.Provenance*;
// path-unique is this package's: no host said where, but exactly one service in
// the workspace serves a route that specific.
const ProvenancePathUnique = "path-unique"

// minLiteralSegments is how specific a route must be to identify a service by
// itself. /health is every service's; /v1/internal/api-access/{org}/api/{api} is one's.
const minLiteralSegments = 2

// Service is one service of the workspace and what is known about it.
type Service struct {
	Name string
	Path string
	// Config is its Spring config, which folds the placeholders in ITS calls' URLs.
	Config workspace.SpringConfig
	// Snapshot is what its index offers; a zero Snapshot reads as not indexed.
	Snapshot codegraph.ServiceSnapshot
}

// Side is one end of an edge: a service and the method there.
type Side struct {
	Service string `json:"service"` // the service's name
	Path    string `json:"path"`    // and its folder, which is its identity
	NodeID  string `json:"nodeId,omitempty"`
	Label   string `json:"label,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
}

// Callee is the route a call lands on.
type Callee struct {
	Side
	EntryPointID string `json:"entryPointId"`
	Verb         string `json:"verb"`
	Target       string `json:"target"`
}

// Edge is one outbound call, and where it goes as far as the workspace can tell.
type Edge struct {
	Status string `json:"status"`
	// Provenance says how a linked edge was reached; empty otherwise.
	Provenance string `json:"provenance,omitempty"`
	Caller     Side   `json:"caller"`
	Client     string `json:"client"`
	HTTPMethod string `json:"httpMethod"`
	URL        string `json:"url"`
	// Callee is the route, when linked. More than one when several routes of the
	// service match equally well (Matches carries the rest).
	Callee  *Callee  `json:"callee,omitempty"`
	Matches []Callee `json:"matches,omitempty"`
	// Candidates are the services an ambiguous edge could reach.
	Candidates []string `json:"candidates,omitempty"`
	// Host is the host the call named, for the statuses that stop at one.
	Host string `json:"host,omitempty"`
	// CalleeState and CalleeIndexedAt say how fresh the other end's data is: a service
	// nothing keeps watching can have drifted from its index without anything saying so.
	CalleeState     string `json:"calleeState,omitempty"`
	CalleeIndexedAt int64  `json:"calleeIndexedAt,omitempty"`
}

// Join computes every outbound call of every service in the workspace and where it goes.
func Join(services []Service) []Edge {
	targets := make([]workspace.TargetService, 0, len(services))
	byPath := map[string]*Service{}
	for i := range services {
		s := &services[i]
		byPath[s.Path] = s
		targets = append(targets, workspace.TargetService{Name: s.Name, Path: s.Path, Port: s.Config.ServerPort()})
	}

	var edges []Edge
	for i := range services {
		caller := &services[i]
		for _, call := range caller.Snapshot.Outbound {
			edges = append(edges, joinCall(caller, call, services, byPath, targets))
		}
	}
	sort.SliceStable(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.Caller.Path != b.Caller.Path {
			return a.Caller.Path < b.Caller.Path
		}
		if a.Caller.File != b.Caller.File {
			return a.Caller.File < b.Caller.File
		}
		return a.Caller.Line < b.Caller.Line
	})
	return edges
}

func joinCall(caller *Service, call codegraph.SnapshotCall, all []Service, byPath map[string]*Service, targets []workspace.TargetService) Edge {
	edge := Edge{
		Caller: Side{Service: caller.Name, Path: caller.Path, NodeID: call.From, Label: call.Label, File: call.File, Line: call.Line},
		Client: call.Client, HTTPMethod: call.HTTPMethod, URL: call.URL,
	}
	resolution := workspace.ResolveTarget(workspace.OutboundTarget{ServiceName: call.ServiceName, URL: call.URL}, caller.Config, targets)
	path := resolution.Path
	if path == "" || !strings.HasPrefix(path, "/") {
		path = callPath(call.URL)
	}
	edge.Host = resolution.Host

	switch resolution.Status {
	case workspace.TargetResolved:
		return linkTo(edge, byPath[resolution.Service], call.HTTPMethod, path, resolution.Provenance)
	case workspace.TargetExternal:
		edge.Status = StatusExternal
		return edge
	case workspace.TargetAmbiguousService:
		// The host narrowed it to a few; the route may narrow it to one.
		var candidates []*Service
		for _, p := range resolution.Candidates {
			if s := byPath[p]; s != nil && s != caller {
				candidates = append(candidates, s)
			}
		}
		if linked, ok := byRoute(edge, candidates, call.HTTPMethod, path); ok {
			return linked
		}
		edge.Status = StatusAmbiguousService
		edge.Candidates = resolution.Candidates
		return edge
	}

	// No host to go by (dynamic-url), or a host in no service (unknown-service):
	// the route alone, when it is specific enough to mean one service.
	var others []*Service
	for i := range all {
		if &all[i] != caller {
			others = append(others, &all[i])
		}
	}
	if literalSegments(path) >= minLiteralSegments {
		if linked, ok := byRoute(edge, others, call.HTTPMethod, path); ok {
			return linked
		}
	}
	edge.Status = resolution.Status
	if edge.Status == "" {
		edge.Status = StatusDynamicURL
	}
	return edge
}

// linkTo links an edge to a route of the one service the call was resolved to.
func linkTo(edge Edge, callee *Service, verb, path, provenance string) Edge {
	if callee == nil {
		edge.Status = StatusUnknownService
		return edge
	}
	edge.CalleeState, edge.CalleeIndexedAt = callee.Snapshot.State, callee.Snapshot.IndexedAt
	if callee.Snapshot.State != codegraph.SnapshotIndexed {
		edge.Status = StatusNotIndexed
		if edge.CalleeState == "" {
			edge.CalleeState = codegraph.SnapshotNotIndexed
		}
		edge.Callee = &Callee{Side: Side{Service: callee.Name, Path: callee.Path}}
		return edge
	}
	matches := routesOf(callee, verb, path)
	if len(matches) == 0 {
		edge.Status = StatusEndpointGone
		edge.Callee = &Callee{Side: Side{Service: callee.Name, Path: callee.Path}}
		return edge
	}
	edge.Status, edge.Provenance = StatusLinked, provenance
	edge.Callee = &matches[0]
	if len(matches) > 1 {
		edge.Matches = matches[1:]
	}
	return edge
}

// byRoute links an edge by the route alone: exactly one of the services serves
// it. More than one is ambiguous-path, said with the services; none is no link.
func byRoute(edge Edge, services []*Service, verb, path string) (Edge, bool) {
	if path == "" {
		return edge, false
	}
	var serving []*Service
	for _, s := range services {
		if s.Snapshot.State == codegraph.SnapshotIndexed && len(routesOf(s, verb, path)) > 0 {
			serving = append(serving, s)
		}
	}
	switch len(serving) {
	case 0:
		return edge, false
	case 1:
		return linkTo(edge, serving[0], verb, path, ProvenancePathUnique), true
	}
	edge.Status = StatusAmbiguousPath
	for _, s := range serving {
		edge.Candidates = append(edge.Candidates, s.Path)
	}
	sort.Strings(edge.Candidates)
	return edge, true
}

// routesOf is the routes of a service a call can land on, best match first —
// the most literal segments, then the order the service lists them in. Only
// the best-scoring ones are returned.
func routesOf(s *Service, verb, path string) []Callee {
	best := -1
	var out []Callee
	for _, e := range s.Snapshot.Endpoints {
		if !methodMatches(verb, e.Verb) {
			continue
		}
		score, ok := match(path, withContextPath(s.Snapshot.ContextPath, e.Target))
		if !ok || score < best {
			continue
		}
		if score > best {
			best, out = score, out[:0]
		}
		out = append(out, Callee{
			Side:         Side{Service: s.Name, Path: s.Path, NodeID: e.EntryNode, Label: e.Label, File: e.File, Line: e.Line},
			EntryPointID: e.ID, Verb: e.Verb, Target: e.Target,
		})
	}
	return out
}

// methodMatches: ANY on either side (an unknown method, or a mapping with no
// method) matches everything.
func methodMatches(call, endpoint string) bool {
	return call == "" || endpoint == "" || strings.EqualFold(call, "ANY") || strings.EqualFold(endpoint, "ANY") || strings.EqualFold(call, endpoint)
}
