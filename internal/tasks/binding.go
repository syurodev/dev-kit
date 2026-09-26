package tasks

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Boards and workspaces live on opposite sides of the sync boundary.
//
// A board is a vault item and travels to every device; a workspace is a row in
// `workspaces` and never leaves the machine that made it. So the link between
// them cannot be a `workspace_id` on the board — on the next device that id
// points at nothing, which is the mistake `project_path` already makes with an
// absolute path. The link lives in a local table instead, and a board that
// syncs in from elsewhere simply arrives unbound until someone gathers it into
// a workspace here. The same reasoning is why accounts link through
// `scope_owner` rather than a stored `account_id`.
//
// The binding is also what decides which agent sees which board, so it is
// tasks:admin and UI-only: letting an agent bind a board would let it widen
// its own reach.

// WorkspaceScope is what tasks needs from the workspace registry and Agent
// Hub, injected by internal/app so this package imports neither — the same
// shape SessionLiveness already has, and no new edge for internal/arch.
type WorkspaceScope interface {
	// ServicePath resolves a service name in a workspace to its path on this
	// machine. An empty service is the primary.
	ServicePath(workspaceID, service string) (string, bool)
	// HasPath reports whether path names one of the workspace's service
	// paths. This is what lets a legacy board's `project_path` keep working
	// without a synced write: the path is trusted only when it still names a
	// real service in the workspace the board is bound to, never as a bare
	// string handed back unchecked.
	HasPath(workspaceID, path string) bool
	// SessionWorkspaces lists the workspaces an agent session belongs to.
	SessionWorkspaces(sessionID string) []string
	// Exists reports whether a workspace is still registered here.
	Exists(workspaceID string) bool
}

// bindingRepo is the local table, and nothing more: which board is bound to
// which workspace on this machine.
type bindingRepo struct {
	db    *sql.DB
	table string
	now   func() time.Time
}

// Bind binds a board, replacing whatever it was bound to — a board belongs to
// at most one workspace per machine.
func (r *bindingRepo) Bind(boardID, workspaceID string) error {
	_, err := r.db.Exec(fmt.Sprintf(
		`INSERT INTO %s (board_id, workspace_id, bound_at) VALUES (?, ?, ?)
		 ON CONFLICT(board_id) DO UPDATE SET workspace_id = excluded.workspace_id, bound_at = excluded.bound_at`,
		r.table), boardID, workspaceID, r.now().UTC().Format(time.RFC3339))
	return err
}

// Unbind removes a board's binding. Unbinding an unbound board is not an error.
func (r *bindingRepo) Unbind(boardID string) error {
	_, err := r.db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE board_id = ?`, r.table), boardID)
	return err
}

// workspaceOf is one board's binding. A read that fails reports unbound, which
// is the closed answer: an unbound board is one no agent sees.
func (r *bindingRepo) workspaceOf(boardID string) (string, bool) {
	var workspaceID string
	err := r.db.QueryRow(fmt.Sprintf(`SELECT workspace_id FROM %s WHERE board_id = ?`, r.table),
		boardID).Scan(&workspaceID)
	if err != nil {
		return "", false
	}
	return workspaceID, true
}

// bindings is every binding on this machine, board id to workspace id, so a
// board listing is one query rather than one per board.
func (r *bindingRepo) bindings() (map[string]string, error) {
	rows, err := r.db.Query(fmt.Sprintf(`SELECT board_id, workspace_id FROM %s`, r.table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var boardID, workspaceID string
		if err := rows.Scan(&boardID, &workspaceID); err != nil {
			return nil, err
		}
		out[boardID] = workspaceID
	}
	return out, rows.Err()
}

// SetScope wires the workspace registry and the binding table.
//
// A nil scope is today's behaviour exactly: every caller sees every board.
// That keeps the tests and a build without workspaces working, and it is not
// a hole — it is the state before bindings existed. A nil db leaves no table
// to read, so every board reads as unbound.
func (s *Service) SetScope(scope WorkspaceScope, db *sql.DB, table string) {
	s.scope = scope
	s.binds = nil
	if db != nil && table != "" {
		s.binds = &bindingRepo{db: db, table: table, now: s.now}
	}
}

// workspaceOf is a board's binding as the rest of the package sees it. A
// binding whose workspace is gone reads as unbound: the foreign key's cascade
// normally removes it, and this is the net for a database opened without
// foreign keys switched on.
func (s *Service) workspaceOf(boardID string) string {
	if s.binds == nil {
		return ""
	}
	workspaceID, ok := s.binds.workspaceOf(boardID)
	if !ok || (s.scope != nil && !s.scope.Exists(workspaceID)) {
		return ""
	}
	return workspaceID
}

// boundWorkspaces is workspaceOf for every board at once.
func (s *Service) boundWorkspaces() (map[string]string, error) {
	if s.binds == nil {
		return map[string]string{}, nil
	}
	all, err := s.binds.bindings()
	if err != nil {
		return nil, err
	}
	if s.scope != nil {
		for boardID, workspaceID := range all {
			if !s.scope.Exists(workspaceID) {
				delete(all, boardID)
			}
		}
	}
	return all, nil
}

// BindBoard binds a board to a workspace on this machine. It writes the local
// table only — never the board's record, which syncs and on which a workspace
// id would dangle.
//
// tasks:admin and UI-only, never MCP: a binding decides which agents see a
// board, and an agent that could bind one could widen its own scope.
func (s *Service) BindBoard(boardID, workspaceID string) (Board, error) {
	board, err := s.board(boardID)
	if err != nil {
		return Board{}, err
	}
	if workspaceID == "" || s.binds == nil {
		return Board{}, ErrInvalid
	}
	if s.scope != nil && !s.scope.Exists(workspaceID) {
		return Board{}, ErrInvalid
	}
	if err := s.binds.Bind(boardID, workspaceID); err != nil {
		return Board{}, err
	}
	board.WorkspaceID = workspaceID
	return board, nil
}

// UnbindBoard returns a board to the unbound group on this machine. Its tasks
// are untouched; only which agents can see them changes — none, until it is
// bound again. tasks:admin and UI-only, like BindBoard.
func (s *Service) UnbindBoard(boardID string) (Board, error) {
	board, err := s.board(boardID)
	if err != nil {
		return Board{}, err
	}
	if s.binds != nil {
		if err := s.binds.Unbind(boardID); err != nil {
			return Board{}, err
		}
	}
	board.WorkspaceID = ""
	return board, nil
}

// ---- who sees which board --------------------------------------------

// scoped reports whether the caller is one the binding restricts. The user is
// not — and neither is a terminal CLI on the shared MCP socket, which carries
// no session id and so arrives here as the user, consistent with
// workspace.list. Only an Agent Hub session is narrowed, and only once a scope
// is wired.
func (s *Service) scoped(caller Caller) bool {
	return s.scope != nil && caller.IsAgent()
}

// inScope reports whether a board bound to workspaceID is visible to caller.
// An unbound board is visible to no agent: failing open would put every board
// synced in from another machine in front of every agent on this one.
func (s *Service) inScope(caller Caller, workspaceID string) bool {
	if !s.scoped(caller) {
		return true
	}
	if workspaceID == "" {
		return false
	}
	for _, id := range s.scope.SessionWorkspaces(caller.SessionID) {
		if id == workspaceID {
			return true
		}
	}
	return false
}

// boardFor is the one scope check. Every public path reaches a board through
// it — directly, or through the task's board — rather than each MCP tool
// filtering for itself and one of them forgetting.
//
// A board outside the caller's scope is ErrNotFound, deliberately not
// ErrPermissionDenied: "you may not" says the board exists, and which boards
// another workspace has is exactly what the scope is keeping from the agent.
func (s *Service) boardFor(caller Caller, boardID string) (Board, error) {
	board, err := s.board(boardID)
	if err != nil {
		if s.scoped(caller) {
			return Board{}, notFound(err)
		}
		return Board{}, err
	}
	if !s.inScope(caller, board.WorkspaceID) {
		return Board{}, ErrNotFound
	}
	return board, nil
}

// guard is boardFor for paths that do not otherwise need the board. For a
// caller the scope does not restrict it reads nothing, so those paths behave
// exactly as they did before bindings — including a listing of a board that
// does not exist still being empty rather than an error.
func (s *Service) guard(caller Caller, boardID string) error {
	if !s.scoped(caller) {
		return nil
	}
	_, err := s.boardFor(caller, boardID)
	return err
}

// taskFor reads a task through its board's scope. Anything that keeps a
// scoped caller from the board — out of scope, unbound, or a board that is
// gone — reads as the task not existing.
func (s *Service) taskFor(caller Caller, id string) (Task, error) {
	task, err := s.get(id)
	if err != nil {
		return Task{}, err
	}
	if err := s.guard(caller, task.BoardID); err != nil {
		return Task{}, err
	}
	return task, nil
}

// notFound folds every refusal of a scoped read into ErrNotFound, so an agent
// cannot tell a board it may not see from one that is gone or does not decode.
func notFound(err error) error {
	if err == nil || errors.Is(err, ErrNotFound) {
		return err
	}
	return ErrNotFound
}
