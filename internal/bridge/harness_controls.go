package bridge

import (
	"context"

	"devkit/internal/harnessconfig"
)

// The window's tool-group controls (H5a). Each method is a thin call into the
// shared facade (internal/app.HarnessPort → the assembly's controls): scope is
// resolved there from the identity and the persisted rows, and the facade itself
// refuses any caller but the window, so a terminal or an agent that reached this
// service could not use them. A committed change whose publication failed comes
// back as a normal Outcome (status paused or unknown) rather than as an error, so
// the window never loses what was saved.

// HarnessAvailable reports whether the controls are wired. The window shows its
// entry points only from this; it is not a feature flag, it is whether the graph
// exists.
func (s *AppService) HarnessAvailable() bool { return s.container.Harness.Bound() }

// HarnessWorkspaceProfile reads a workspace's tool groups.
func (s *AppService) HarnessWorkspaceProfile(workspaceID string) (harnessconfig.ProfileView, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:read", "ui:profile", map[string]any{
		"action": "workspace_profile", "workspace_id": recordID(workspaceID),
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.WorkspaceProfile(ctx, workspaceID)
	})
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	return result.(harnessconfig.ProfileView), nil
}

// HarnessSessionProfile reads one session's tool groups: saved and running.
func (s *AppService) HarnessSessionProfile(workspaceID, sessionID string) (harnessconfig.ProfileView, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:read", "ui:profile", map[string]any{
		"action": "session_profile", "workspace_id": recordID(workspaceID), "session_id": recordID(sessionID),
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.SessionProfile(ctx, workspaceID, sessionID)
	})
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	return result.(harnessconfig.ProfileView), nil
}

// HarnessPrepareWorkspaceGroups prepares an edit of a workspace's group entries.
// It writes nothing; HarnessApply asks and commits.
func (s *AppService) HarnessPrepareWorkspaceGroups(workspaceID string, edits []harnessconfig.GroupEdit) (harnessconfig.Review, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:write", "ui:change", map[string]any{
		"action": "prepare_workspace_groups", "workspace_id": recordID(workspaceID), "edits": len(edits),
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.PrepareWorkspaceGroups(ctx, workspaceID, edits)
	})
	if err != nil {
		return harnessconfig.Review{}, err
	}
	return result.(harnessconfig.Review), nil
}

// HarnessWorkspaceBudgets reads a bounded page of a workspace's result limits:
// the explicit rules, the effective values and where each comes from. group
// filters; cursor is the previous page's, valid for the same group only.
func (s *AppService) HarnessWorkspaceBudgets(workspaceID, group, cursor string) (harnessconfig.BudgetPage, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:read", "ui:profile", map[string]any{
		"action": "workspace_budgets", "workspace_id": recordID(workspaceID),
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.WorkspaceBudgets(ctx, workspaceID, harnessconfig.BudgetFilter{Group: group, Cursor: cursor})
	})
	if err != nil {
		return harnessconfig.BudgetPage{}, err
	}
	return result.(harnessconfig.BudgetPage), nil
}

// HarnessPrepareBudgetEdits prepares partial result-budget edits of a workspace's
// profile (size and cut mode independently settable or resettable). It writes
// nothing; HarnessApply asks and commits.
func (s *AppService) HarnessPrepareBudgetEdits(workspaceID string, edits []harnessconfig.BudgetEdit) (harnessconfig.Review, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:write", "ui:change", map[string]any{
		"action": "prepare_budget_edits", "workspace_id": recordID(workspaceID), "edits": len(edits),
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.PrepareBudgetEdits(ctx, workspaceID, edits)
	})
	if err != nil {
		return harnessconfig.Review{}, err
	}
	return result.(harnessconfig.Review), nil
}

// HarnessPrepareRevoke prepares removing groups from a session's override.
func (s *AppService) HarnessPrepareRevoke(workspaceID, sessionID string, groups []string) (harnessconfig.Review, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:write", "ui:change", map[string]any{
		"action": "prepare_revoke", "workspace_id": recordID(workspaceID), "session_id": recordID(sessionID), "groups": len(groups),
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.PrepareRevoke(ctx, workspaceID, sessionID, groups)
	})
	if err != nil {
		return harnessconfig.Review{}, err
	}
	return result.(harnessconfig.Review), nil
}

// HarnessStaleProfile diagnoses a workspace's stored profile for references to
// groups or tools this build does not register. It needs no session and works
// when the profile does not resolve.
func (s *AppService) HarnessStaleProfile(workspaceID string) (harnessconfig.StaleDiagnosis, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:read", "ui:profile", map[string]any{
		"action": "stale_profile", "workspace_id": recordID(workspaceID),
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.StaleProfile(ctx, workspaceID)
	})
	if err != nil {
		return harnessconfig.StaleDiagnosis{}, err
	}
	return result.(harnessconfig.StaleDiagnosis), nil
}

// HarnessPrepareStaleRepair prepares removing exactly the stale references of a
// workspace's stored profile. Only the workspace is named; the removals are derived
// in Go. It writes nothing; HarnessApply always asks and commits.
func (s *AppService) HarnessPrepareStaleRepair(workspaceID string) (harnessconfig.Review, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:write", "ui:change", map[string]any{
		"action": "prepare_stale_repair", "workspace_id": recordID(workspaceID),
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.PrepareStaleRepair(ctx, workspaceID)
	})
	if err != nil {
		return harnessconfig.Review{}, err
	}
	return result.(harnessconfig.Review), nil
}

// HarnessApply asks the person through the shared confirmation flow and commits
// a prepared change. A change that was saved but not published returns an
// Outcome, not an error.
func (s *AppService) HarnessApply(id string) (harnessconfig.Outcome, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:write", "ui:change", map[string]any{
		"action": "apply",
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.Apply(ctx, id)
	})
	if err != nil {
		return harnessconfig.Outcome{}, err
	}
	return result.(harnessconfig.Outcome), nil
}

// HarnessCancel withdraws a prepared change.
func (s *AppService) HarnessCancel(id string) error {
	_, err := s.call(context.Background(), "builtin.harness", "harness:write", "ui:change", map[string]any{
		"action": "cancel",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Harness.Cancel(ctx, id)
	})
	return err
}

// HarnessReconcile brings a session's endpoint to its latest saved override.
func (s *AppService) HarnessReconcile(workspaceID, sessionID string) (harnessconfig.Outcome, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:write", "ui:reconcile", map[string]any{
		"action": "reconcile", "workspace_id": recordID(workspaceID), "session_id": recordID(sessionID),
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.Reconcile(ctx, workspaceID, sessionID)
	})
	if err != nil {
		return harnessconfig.Outcome{}, err
	}
	return result.(harnessconfig.Outcome), nil
}

// The window's side of the child tool-group request ledger. The facade refuses
// anything but the window for the answer and the locks, so these are thin.

// HarnessGroupRequests lists the requests for more tool groups that wait for the
// person, with the revision each was read at. A request's reason is the session's
// own words: show it as untrusted text.
func (s *AppService) HarnessGroupRequests() ([]harnessconfig.GroupRequestView, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:read", "ui:profile", map[string]any{
		"action": "group_requests",
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.ListGroupRequests(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]harnessconfig.GroupRequestView), nil
}

// HarnessAnswerGroupRequest answers a request that waits for the person:
// "approve" (shows the exact change and asks again), "decline" (this request
// only) or "block" (decline and lock those groups for that session). The answer
// is bound to the revision the window showed.
func (s *AppService) HarnessAnswerGroupRequest(id string, revision int64, answer string) (harnessconfig.GroupRequestResult, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:write", "ui:change", map[string]any{
		"action": "answer_group_request", "answer": answer,
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.AnswerGroupRequest(ctx, id, revision, answer)
	})
	if err != nil {
		return harnessconfig.GroupRequestResult{}, err
	}
	return result.(harnessconfig.GroupRequestResult), nil
}

// HarnessGroupLocks lists the tool groups the person blocked for a session.
func (s *AppService) HarnessGroupLocks(sessionID string) ([]string, error) {
	result, err := s.call(context.Background(), "builtin.harness", "harness:read", "ui:profile", map[string]any{
		"action": "group_locks", "session_id": recordID(sessionID),
	}, func(ctx context.Context) (any, error) {
		return s.container.Harness.GroupLocks(ctx, sessionID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}

// HarnessSetGroupLock blocks a tool group for a session (no agent can add it) or
// lifts the block. Only the person writes a lock.
func (s *AppService) HarnessSetGroupLock(sessionID, group string, locked bool) error {
	_, err := s.call(context.Background(), "builtin.harness", "harness:write", "ui:change", map[string]any{
		"action": "set_group_lock", "session_id": recordID(sessionID), "locked": locked,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Harness.SetGroupLock(ctx, sessionID, group, locked)
	})
	return err
}
