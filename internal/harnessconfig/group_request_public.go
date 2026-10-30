package harnessconfig

import "context"

// The public face of the child tool-group request ledger. The ledger itself
// lives in Agent Hub; this is the bounded shape tools and the window see. Scope
// is never a field: the calling session, its parent and the child come from the
// authenticated identity and the persisted session rows.

// GroupRequestView is one request. ReasonUntrusted is the child's own text: show
// it only labelled as such, never as an instruction.
type GroupRequestView struct {
	ID              string   `json:"id"`
	ChildID         string   `json:"childId"`
	Groups          []string `json:"groups"`
	Route           string   `json:"route"`
	State           string   `json:"state"`
	Revision        int64    `json:"revision"`
	ReasonUntrusted string   `json:"reasonUntrusted,omitempty"`
	Resolver        string   `json:"resolver,omitempty"`
	Outcome         string   `json:"outcome,omitempty"`
	CreatedAt       string   `json:"createdAt"`
}

// GroupRequestResult is a request and, when a change was made, what became of the
// child's endpoint. Live is true only for an acknowledged endpoint.
type GroupRequestResult struct {
	Request GroupRequestView `json:"request"`
	Outcome *Outcome         `json:"outcome,omitempty"`
}

// GroupRequestControls is implemented by the same facade as Controls.
type GroupRequestControls interface {
	// CreateGroupRequest is the calling session's own request. route is "parent"
	// (returns at once, the parent is rung) or "user" (waits for the person).
	CreateGroupRequest(ctx context.Context, groups []string, route, reason string) (GroupRequestResult, error)
	// GroupRequestStatus reads a request of the caller's own, of its direct
	// child, or any request for the window.
	GroupRequestStatus(ctx context.Context, id string) (GroupRequestView, error)
	// ListGroupRequests lists the open requests the caller may watch or answer.
	ListGroupRequests(ctx context.Context) ([]GroupRequestView, error)
	// ResolveGroupRequest is the direct parent's answer: "approve", "deny" or
	// "forward", bound to the revision the parent saw.
	ResolveGroupRequest(ctx context.Context, id string, revision int64, decision string) (GroupRequestResult, error)
	// AnswerGroupRequest is the window's answer to a request waiting on the
	// person: "approve", "decline" or "block". Window only.
	AnswerGroupRequest(ctx context.Context, id string, revision int64, answer string) (GroupRequestResult, error)
	// GroupLocks and SetGroupLock read and write the person's locks. A session
	// reads its own; only the window writes.
	GroupLocks(ctx context.Context, sessionID string) ([]string, error)
	SetGroupLock(ctx context.Context, sessionID, group string, locked bool) error

	// PlanChildGroups is the check agenthub.createAgentSession runs before
	// anything is created: given is false for "inherit", true for an explicit
	// list. It returns the groups the child should start with.
	PlanChildGroups(ctx context.Context, explicit []string, given bool) ([]string, error)
	// GrantChildGroups writes the planned groups into the new child's override
	// and publishes them. A nil outcome means there was nothing to grant.
	GrantChildGroups(ctx context.Context, childID string, groups []string) (*Outcome, error)
}
