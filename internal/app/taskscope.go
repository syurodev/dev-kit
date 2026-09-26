package app

import (
	"context"
	"log"
	"path/filepath"

	"devkit/internal/tasks"
	"devkit/internal/workspace"
)

// workspaceRegistry is the slice of *workspace.Service taskScope needs — list
// workspaces, list a workspace's services. Narrow so a test can fake it
// without a SQL-backed registry, the same reason agentSessions narrows
// AgentHub down to GetStatus in wiring.go.
type workspaceRegistry interface {
	ListWorkspaces() ([]workspace.Workspace, error)
	ListServices(workspaceID string) ([]workspace.WorkspaceService, error)
}

// taskScope adapts the workspace registry and Agent Hub to tasks.WorkspaceScope
// (internal/tasks/binding.go), so internal/tasks imports neither — the same
// shape agentLiveness already gives it for SetLiveness.
type taskScope struct {
	workspace workspaceRegistry
	hub       agentSessions
}

// newTaskScope builds the adapter. hub may be nil (no Agent Hub wired yet, or
// server mode's stubs): SessionWorkspaces then reads as "no session" and every
// agent sees no board, which is the fail-closed answer, not a panic.
func newTaskScope(ws workspaceRegistry, hub agentSessions) *taskScope {
	return &taskScope{workspace: ws, hub: hub}
}

// ServicePath resolves a service name in a workspace to its path on this
// machine. ListServices already puts the workspace's primary first (see
// workspace.Service.ListServices), so an empty name — "the primary" — is
// simply its first entry.
func (t *taskScope) ServicePath(workspaceID, service string) (string, bool) {
	services, err := t.workspace.ListServices(workspaceID)
	if err != nil || len(services) == 0 {
		return "", false
	}
	if service == "" {
		return services[0].Path, true
	}
	for _, svc := range services {
		if svc.Name == service {
			return svc.Path, true
		}
	}
	return "", false
}

// HasPath reports whether path names one of the workspace's services. It is
// also what plantasks.go's reconcile uses (through workspaceHasPath) to
// decide which board a re-indexed project belongs to, so the two never
// disagree about what "this workspace has that path" means.
func (t *taskScope) HasPath(workspaceID, path string) bool {
	return workspaceHasPath(t.workspace, workspaceID, path)
}

func workspaceHasPath(ws workspaceRegistry, workspaceID, path string) bool {
	services, err := ws.ListServices(workspaceID)
	if err != nil {
		return false
	}
	path = filepath.Clean(path)
	for _, svc := range services {
		if svc.Path == path {
			return true
		}
	}
	return false
}

// SessionWorkspaces lists the workspaces whose services an agent session owns
// — its cwd is inside one, or a granted path covers one — read through
// workspace.SessionOwns (internal/workspace/scope.go) so this and
// workspace.list (the MCP tool an agent uses to ask the same question about
// itself) can never disagree about the rule.
//
// An unknown session, no hub, or any error along the way reads as no
// workspaces at all: fail closed, so an agent whose session cannot be
// resolved sees no board rather than every one.
func (t *taskScope) SessionWorkspaces(sessionID string) []string {
	if t.hub == nil || sessionID == "" {
		return nil
	}
	session, err := t.hub.GetStatus(context.Background(), sessionID)
	if err != nil {
		return nil
	}
	granted := append([]string{session.ProjectPath}, session.ExtraReadWritePaths...)
	all, err := t.workspace.ListWorkspaces()
	if err != nil {
		return nil
	}
	var owned []string
	for _, ws := range all {
		services, err := t.workspace.ListServices(ws.ID)
		if err != nil {
			continue
		}
		if workspace.SessionOwns(services, session.ProjectPath, granted) {
			owned = append(owned, ws.ID)
		}
	}
	return owned
}

// Exists reports whether a workspace is still registered here.
func (t *taskScope) Exists(workspaceID string) bool {
	all, err := t.workspace.ListWorkspaces()
	if err != nil {
		return false
	}
	for _, ws := range all {
		if ws.ID == workspaceID {
			return true
		}
	}
	return false
}

// MigrateBoardBindings binds every legacy board — one with a project_path
// that this machine has never considered — to the workspace that has a
// service at that exact path, so an upgrade does not make every existing
// board vanish from every agent the moment scope wiring turns on
// (docs/dev-kit/tasks.mdx § Migration). Idempotent, and more than merely
// re-run-safe: it skips anything tasks.Service.Considered already has an
// opinion about, bound or deliberately not, so a board a person unbinds stays
// unbound on every later startup instead of drifting back the moment its
// stale project_path matches a workspace again.
//
// It writes only the local binding table, never the board's own metadata —
// two devices each migrating the same synced board would be two racing
// writes to one record, and a board's sync conflict resolves only
// use-ours/use-theirs, never a merge.
//
// Call once at startup, after the workspace registry is populated and
// Tasks.SetScope has run: a migration before SetScope would read Considered
// as false for every board regardless of what task_board_bindings holds.
func MigrateBoardBindings(c *Container) {
	if c == nil || c.Tasks == nil || c.Workspace == nil {
		return
	}
	boards, err := c.Tasks.Boards(tasks.Caller{})
	if err != nil {
		log.Printf("tasks: migrate board bindings: list boards: %v", err)
		return
	}
	workspaces, err := c.Workspace.ListWorkspaces()
	if err != nil {
		log.Printf("tasks: migrate board bindings: list workspaces: %v", err)
		return
	}
	bound := 0
	for _, board := range boards {
		if board.ProjectPath == "" || c.Tasks.Considered(board.ID) {
			continue
		}
		workspaceID, ok := workspaceForProjectPath(c.Workspace, workspaces, board.ProjectPath)
		if !ok {
			continue
		}
		if _, err := c.Tasks.BindBoard(board.ID, workspaceID); err != nil {
			log.Printf("tasks: migrate board %s to workspace %s: %v", board.ID, workspaceID, err)
			continue
		}
		bound++
	}
	if bound > 0 {
		log.Printf("tasks: migrated %d legacy board(s) to a workspace by project_path", bound)
	}
}

// workspaceForProjectPath finds which registered workspace has a service at
// path. The registry does not forbid the same folder appearing in two
// workspaces, so when more than one matches, the one where it is the
// PRIMARY wins — that workspace's identity IS that path — else the first
// match in ListWorkspaces order, which is at least stable across repeated
// runs on this machine.
func workspaceForProjectPath(ws workspaceRegistry, all []workspace.Workspace, path string) (string, bool) {
	path = filepath.Clean(path)
	fallback := ""
	for _, w := range all {
		services, err := ws.ListServices(w.ID)
		if err != nil {
			continue
		}
		for i, svc := range services {
			if svc.Path != path {
				continue
			}
			if i == 0 {
				return w.ID, true
			}
			if fallback == "" {
				fallback = w.ID
			}
			break
		}
	}
	return fallback, fallback != ""
}
