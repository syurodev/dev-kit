package crossservice

import (
	"errors"
	"fmt"
	"time"

	"devkit/internal/codegraph"
)

// Directions of a Query.
const (
	Outbound = "outbound"
	Inbound  = "inbound"
	Both     = "both"
)

const (
	// DefaultLimit and MaxLimit bound the edges listed per direction.
	DefaultLimit = 100
	MaxLimit     = 500
)

// Query asks what one service calls and what calls it.
type Query struct {
	// Service is the folder of the service asked about.
	Service string
	// Direction is outbound (what it calls), inbound (who calls it) or both (default).
	Direction string
	// EndpointID narrows the inbound edges to one route of the service.
	EndpointID string
	// Limit caps the edges listed in each direction.
	Limit int
}

// ServiceInfo is a service of the workspace and how fresh what is known of it is.
type ServiceInfo struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
	// IndexedAt is the time its index was written, as RFC 3339; empty when unknown.
	IndexedAt string `json:"indexedAt,omitempty"`
	Endpoints int    `json:"endpoints"`
	Outbound  int    `json:"outbound"`
}

// Report is the answer: the edges, how they came out, and what was read to say so.
type Report struct {
	Service       ServiceInfo `json:"service"`
	Outbound      []Edge      `json:"outbound"`
	OutboundTotal int         `json:"outboundTotal"`
	Inbound       []Edge      `json:"inbound"`
	InboundTotal  int         `json:"inboundTotal"`
	// Statuses counts every edge asked about by how it came out, before the limit.
	Statuses map[string]int `json:"statuses"`
	// Services are all the workspace's services, so a reader sees which ones the
	// join could and could not read.
	Services []ServiceInfo `json:"services"`
	Notes    []string      `json:"notes,omitempty"`
}

// Run answers a query over the workspace's services.
func Run(services []Service, q Query) (Report, error) {
	switch q.Direction {
	case "", Both, Outbound, Inbound:
	default:
		return Report{}, fmt.Errorf("direction must be outbound, inbound or both (got %q)", q.Direction)
	}
	if q.Limit < 0 {
		return Report{}, fmt.Errorf("limit must be positive (got %d)", q.Limit)
	}
	limit := q.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)

	var subject *Service
	infos := make([]ServiceInfo, 0, len(services))
	for i := range services {
		s := &services[i]
		infos = append(infos, infoOf(s))
		if s.Path == q.Service {
			subject = s
		}
	}
	if subject == nil {
		return Report{}, errors.New("that folder is not a service of the workspace")
	}

	edges := Join(services)
	report := Report{Service: infoOf(subject), Outbound: []Edge{}, Inbound: []Edge{}, Statuses: map[string]int{}, Services: infos}
	wantOut := q.Direction == "" || q.Direction == Both || q.Direction == Outbound
	wantIn := q.Direction == "" || q.Direction == Both || q.Direction == Inbound

	for _, e := range edges {
		if wantOut && e.Caller.Path == q.Service {
			report.OutboundTotal++
			report.Statuses[e.Status]++
			if len(report.Outbound) < limit {
				report.Outbound = append(report.Outbound, e)
			}
		}
		if wantIn && reaches(e, q.Service, q.EndpointID) {
			report.InboundTotal++
			report.Statuses[e.Status]++
			if len(report.Inbound) < limit {
				report.Inbound = append(report.Inbound, e)
			}
		}
	}

	for _, s := range infos {
		if s.State != codegraph.SnapshotIndexed {
			report.Notes = append(report.Notes, fmt.Sprintf("%s is %s: its routes and calls are not in this answer", s.Name, s.State))
		}
	}
	report.Notes = append(report.Notes,
		"read from each service's index, not from its source now: a service nothing is watching can have drifted from the time shown in services[].indexedAt",
		"a call is linked by the host it names, else by a route specific enough to be one service's (provenance path-unique); a host only the runtime knows is dynamic-url")
	return report, nil
}

// reaches reports whether an edge lands on the service (and the route, when one is named).
func reaches(e Edge, servicePath, endpointID string) bool {
	if e.Caller.Path == servicePath {
		return false // a service calling itself is outbound, not a caller
	}
	if endpointID != "" {
		if e.Status != StatusLinked {
			return false
		}
		if e.Callee != nil && e.Callee.Path == servicePath && e.Callee.EntryPointID == endpointID {
			return true
		}
		for _, m := range e.Matches {
			if m.Path == servicePath && m.EntryPointID == endpointID {
				return true
			}
		}
		return false
	}
	if e.Callee != nil && e.Callee.Path == servicePath {
		return true
	}
	for _, c := range e.Candidates {
		if c == servicePath {
			return true
		}
	}
	return false
}

func infoOf(s *Service) ServiceInfo {
	state := s.Snapshot.State
	if state == "" {
		state = codegraph.SnapshotNotIndexed
	}
	info := ServiceInfo{Name: s.Name, Path: s.Path, State: state, Endpoints: len(s.Snapshot.Endpoints), Outbound: len(s.Snapshot.Outbound)}
	if s.Snapshot.IndexedAt > 0 {
		info.IndexedAt = time.UnixMilli(s.Snapshot.IndexedAt).UTC().Format(time.RFC3339)
	}
	return info
}
