package workspace

import (
	"context"
	"database/sql"
)

// SQLRepository persists workspaces in the workspaces / workspace_services
// tables (storage migration m28). Machine-local, not part of vault_items —
// same reasoning as env_tags: a path is only ever correct on the machine
// that resolved it, so there is nothing here sync could carry.
type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

const selectWorkspaceColumns = "SELECT id, name, root, instructions FROM workspaces"

func (r *SQLRepository) ListWorkspaces() ([]Workspace, error) {
	rows, err := r.db.QueryContext(context.Background(), selectWorkspaceColumns+" ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	workspaces := make([]Workspace, 0)
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Root, &ws.Instructions); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, rows.Err()
}

const selectServiceColumns = "SELECT id, workspace_id, name, path, name_source, index_state, index_error, index_missing_toolchain FROM workspace_services"

func (r *SQLRepository) ListServices(workspaceID string) ([]WorkspaceService, error) {
	rows, err := r.db.QueryContext(context.Background(),
		selectServiceColumns+" WHERE workspace_id = ? ORDER BY name", workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	services := make([]WorkspaceService, 0)
	for rows.Next() {
		var svc WorkspaceService
		if err := rows.Scan(&svc.ID, &svc.WorkspaceID, &svc.Name, &svc.Path, &svc.NameSource,
			&svc.IndexState, &svc.IndexError, &svc.IndexMissingToolchain); err != nil {
			return nil, err
		}
		services = append(services, svc)
	}
	return services, rows.Err()
}

// CreateWorkspace writes the workspace and all of its services in one
// transaction: a partial write would leave a workspace with only some of
// the repos it was supposed to register, and there is no "rescan" path yet
// to notice and fill in the rest.
func (r *SQLRepository) CreateWorkspace(ws Workspace, services []WorkspaceService) error {
	ctx := context.Background()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		"INSERT INTO workspaces (id, name, root, instructions) VALUES (?, ?, ?, ?)",
		ws.ID, ws.Name, ws.Root, ws.Instructions,
	); err != nil {
		return err
	}
	for _, svc := range services {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO workspace_services (id, workspace_id, name, path, name_source) VALUES (?, ?, ?, ?, ?)",
			svc.ID, svc.WorkspaceID, svc.Name, svc.Path, svc.NameSource,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteWorkspace removes the workspace row and every service under it, in
// one transaction — same reasoning as CreateWorkspace.
func (r *SQLRepository) DeleteWorkspace(id string) error {
	ctx := context.Background()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM workspace_services WHERE workspace_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM workspaces WHERE id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLRepository) SetIndexOutcome(path, state, reason string, missingToolchain bool) error {
	_, err := r.db.ExecContext(context.Background(),
		"UPDATE workspace_services SET index_state = ?, index_error = ?, index_missing_toolchain = ? WHERE path = ?",
		state, reason, missingToolchain, path)
	return err
}

// UpdateWorkspace renames the workspace, moves its root, and adds and
// removes services in one transaction: the Edit project dialog saves as one
// change, and half of it applied would be a list nobody asked for.
func (r *SQLRepository) UpdateWorkspace(ws Workspace, add []WorkspaceService, removeIDs []string) error {
	return r.updateWorkspace(ws, add, removeIDs, false)
}

func (r *SQLRepository) UpdateWorkspaceWithInstructions(ws Workspace, add []WorkspaceService, removeIDs []string) error {
	return r.updateWorkspace(ws, add, removeIDs, true)
}

func (r *SQLRepository) updateWorkspace(ws Workspace, add []WorkspaceService, removeIDs []string, withInstructions bool) error {
	ctx := context.Background()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var res sql.Result
	if withInstructions {
		res, err = tx.ExecContext(ctx, "UPDATE workspaces SET name = ?, root = ?, instructions = ? WHERE id = ?", ws.Name, ws.Root, ws.Instructions, ws.ID)
	} else {
		res, err = tx.ExecContext(ctx, "UPDATE workspaces SET name = ?, root = ? WHERE id = ?", ws.Name, ws.Root, ws.ID)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrWorkspaceNotFound
	}
	for _, id := range removeIDs {
		if _, err := tx.ExecContext(ctx, "DELETE FROM workspace_services WHERE id = ? AND workspace_id = ?", id, ws.ID); err != nil {
			return err
		}
	}
	for _, svc := range add {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO workspace_services (id, workspace_id, name, path, name_source) VALUES (?, ?, ?, ?, ?)",
			svc.ID, ws.ID, svc.Name, svc.Path, svc.NameSource,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *SQLRepository) SetInstructions(id, instructions string) error {
	result, err := r.db.ExecContext(context.Background(), "UPDATE workspaces SET instructions = ? WHERE id = ?", instructions, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrWorkspaceNotFound
	}
	return nil
}
