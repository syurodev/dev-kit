package agenthub

import (
	"devkit/internal/usagemetrics"
	"time"
)

// RuntimeUsageFacts are host-owned facts about one opened process, not the
// mutable session binding. EndpointToken prevents stale sockets adopting them.
type RuntimeUsageFacts struct {
	Vendor                                                   AgentType
	WorkspaceID, AccountID, AccountGeneration, EndpointToken string
	ProfileRevision, OverrideRevision                        *int64
}
type TurnAttribution struct {
	ContextVersion int
	StartedAt      time.Time
	Facts          RuntimeUsageFacts
}

func factsForSpec(sess AgentSession, spec OpenSpec, id string) RuntimeUsageFacts {
	gen := spec.UsageAccountGeneration
	if id == BuiltinAccountID(sess.AgentType) {
		gen = "builtin"
	}
	return RuntimeUsageFacts{Vendor: sess.AgentType, WorkspaceID: spec.WorkspaceID, AccountID: id, AccountGeneration: gen, EndpointToken: spec.UsageToken}
}
func (s *Service) publishUsageLocked(sess AgentSession, spec OpenSpec, id string) {
	s.runtimeUsage[sess.ID] = factsForSpec(sess, spec, id)
	s.sessionAccount[sess.ID] = id
}
func (s *Service) turnAttribution(id string) TurnAttribution {
	s.mu.Lock()
	defer s.mu.Unlock()
	return TurnAttribution{ContextVersion: 1, StartedAt: time.Now().UTC(), Facts: s.runtimeUsage[id]}
}
func (s *Service) ToolUsageScope(id, token string) (usagemetrics.Scope, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.runtimeUsage[id]
	if !ok || token == "" || f.EndpointToken != token {
		return usagemetrics.Scope{}, false
	}
	return usagemetrics.Scope{Vendor: string(f.Vendor), WorkspaceID: f.WorkspaceID, ProfileRevision: f.ProfileRevision, OverrideRevision: f.OverrideRevision}, true
}
