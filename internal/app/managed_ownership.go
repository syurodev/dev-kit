package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"devkit/internal/agenthub"
	"devkit/internal/agentwork"
	"devkit/internal/identity"
	"devkit/internal/tasks"
	"devkit/internal/workspace"
)

// WireManagedOwnership installs durable writer fences and encrypted payload
// services after the root has opened the database. The app package receives
// interfaces and never imports storage.
func WireManagedOwnership(c *Container, store interface {
	agentwork.ManagedLeaseStore
	agentwork.ManagedPayloadStore
}) {
	if c == nil || c.AgentHub == nil || c.Tasks == nil || c.Workspace == nil || c.Vault == nil || store == nil {
		return
	}
	host := &managedOwnershipHost{container: c, store: store}
	leases := agentwork.NewManagedLeaseService(host, store)
	payloads := agentwork.NewManagedPayloadService(store, host, managedVaultPayloadCodec{vault: c.Vault})
	c.ManagedLeases = leases
	c.ManagedPayloads = payloads
	c.AgentHub.SetManagedWriterGuard(leases)
}

type managedOwnershipHost struct {
	container   *Container
	hubOverride agentSessions
	store       interface {
		agentwork.ManagedLeaseStore
		agentwork.ManagedPayloadStore
	}
}

func (h *managedOwnershipHost) hub() agentSessions {
	if h != nil && h.hubOverride != nil {
		return h.hubOverride
	}
	if h != nil && h.container != nil && h.container.AgentHub != nil {
		return h.container.AgentHub
	}
	return nil
}

func (h *managedOwnershipHost) ResolveManagedWriter(ctx context.Context, attempt agentwork.Attempt) (agentwork.ManagedWriterBinding, error) {
	if err := ctx.Err(); err != nil {
		return agentwork.ManagedWriterBinding{}, err
	}
	c := h.container
	hub := h.hub()
	if c == nil || hub == nil || c.Tasks == nil || c.Workspace == nil || c.Git == nil || h.store == nil {
		return agentwork.ManagedWriterBinding{}, agentwork.ErrManagedHostUnavailable
	}
	item, err := h.store.GetManagedWorkItem(ctx, attempt.WorkItemID)
	if err != nil {
		return agentwork.ManagedWriterBinding{}, err
	}
	group, err := h.store.GetManagedWorkGroup(ctx, attempt.GroupID)
	if err != nil {
		return agentwork.ManagedWriterBinding{}, err
	}
	if item.CurrentAttemptID != attempt.ID || item.GroupID != group.ID || attempt.SessionID == "" || attempt.Generation < 1 ||
		attempt.WorkspaceID != group.WorkspaceID || attempt.BoardID != group.BoardID || attempt.ParentTaskID != group.ParentTaskID ||
		item.TaskID == "" || item.WorkspaceID != group.WorkspaceID || item.BoardID != group.BoardID || item.ParentTaskID != group.ParentTaskID {
		return agentwork.ManagedWriterBinding{}, agentwork.ErrManagedLeaseScope
	}
	taskCaller := tasks.Caller{SessionID: attempt.SessionID}
	board, err := c.Tasks.Board(taskCaller, group.BoardID)
	if err != nil || board.ID != group.BoardID || board.WorkspaceID != group.WorkspaceID {
		return agentwork.ManagedWriterBinding{}, agentwork.ErrManagedLeaseScope
	}
	parent, err := c.Tasks.Get(taskCaller, group.ParentTaskID)
	if err != nil || parent.BoardID != group.BoardID || parent.SessionID != group.CoordinatorID {
		return agentwork.ManagedWriterBinding{}, agentwork.ErrManagedLeaseScope
	}
	internalCtx := identity.WithCaller(ctx, identity.UI())
	coordinator, err := hub.GetStatus(internalCtx, group.CoordinatorID)
	if err != nil || coordinator.ID != group.CoordinatorID || coordinator.WorkspaceID != group.WorkspaceID || coordinator.Status == agenthub.SessionStatusClosed {
		return agentwork.ManagedWriterBinding{}, agentwork.ErrManagedLeaseScope
	}
	session, err := hub.GetStatus(internalCtx, attempt.SessionID)
	if err != nil || session.ID != attempt.SessionID || session.WorkspaceID != group.WorkspaceID || session.Status == agenthub.SessionStatusClosed ||
		session.ParentSessionID == nil || *session.ParentSessionID != group.CoordinatorID {
		return agentwork.ManagedWriterBinding{}, agentwork.ErrManagedLeaseScope
	}
	services, err := c.Workspace.ListServices(group.WorkspaceID)
	if err != nil {
		return agentwork.ManagedWriterBinding{}, err
	}
	scope, err := managedFolderScope(services, coordinator.ProjectPath, coordinator.ExtraReadWritePaths)
	if err != nil {
		return agentwork.ManagedWriterBinding{}, err
	}
	gitService, err := c.Git.At(ctx, session.ProjectPath)
	if err != nil {
		return agentwork.ManagedWriterBinding{}, agentwork.ErrManagedLeaseScope
	}
	root, gitDir, err := gitService.WorktreeIdentity(ctx)
	if err != nil {
		return agentwork.ManagedWriterBinding{}, agentwork.ErrManagedLeaseScope
	}
	folderID := managedFolderForCanonicalPaths(session.ProjectPath, root, scope)
	if folderID == "" {
		return agentwork.ManagedWriterBinding{}, agentwork.ErrManagedLeaseScope
	}
	if err := managedWriterGrantsContained(session.ExtraReadWritePaths, root); err != nil {
		return agentwork.ManagedWriterBinding{}, err
	}
	// Resource identity is global across workspace and folder registrations so
	// overlapping registrations of one physical checkout still contend. The
	// per-worktree git directory keeps linked checkouts distinct.
	resourceKey := managedWriterResourceKey(root, gitDir)
	return agentwork.ManagedWriterBinding{
		WorkspaceID: group.WorkspaceID, BoardID: group.BoardID, ParentTaskID: group.ParentTaskID,
		TaskID: item.TaskID, GroupID: group.ID, WorkItemID: item.ID, AttemptID: attempt.ID,
		SessionID: attempt.SessionID, ResourceKey: resourceKey,
	}, nil
}

func managedWriterResourceKey(worktreeRoot, gitDir string) string {
	digest := sha256.Sum256([]byte("devkit-managed-writer-resource-v1\x00" + filepath.Clean(worktreeRoot) + "\x00" + filepath.Clean(gitDir)))
	return hex.EncodeToString(digest[:])
}

func managedWriterGrantsContained(grants []string, worktreeRoot string) error {
	canonicalRoot, err := filepath.EvalSymlinks(worktreeRoot)
	if err != nil {
		return agentwork.ErrManagedLeaseScope
	}
	canonicalRoot, err = filepath.Abs(canonicalRoot)
	if err != nil {
		return agentwork.ErrManagedLeaseScope
	}
	for _, grant := range grants {
		if strings.TrimSpace(grant) == "" {
			return agentwork.ErrManagedLeaseScope
		}
		canonical, err := filepath.EvalSymlinks(grant)
		if err != nil {
			return agentwork.ErrManagedLeaseScope
		}
		canonical, err = filepath.Abs(canonical)
		if err != nil || !workspace.Within(canonical, canonicalRoot) {
			return agentwork.ErrManagedLeaseScope
		}
	}
	return nil
}

func (h *managedOwnershipHost) ResolveCurrentWriter(ctx context.Context, sessionID string) (agentwork.ManagedWriterResolution, error) {
	hub := h.hub()
	if sessionID == "" || h == nil || h.container == nil || hub == nil || h.store == nil {
		return agentwork.ManagedWriterResolution{}, agentwork.ErrManagedHostUnavailable
	}
	internalCtx := identity.WithCaller(ctx, identity.UI())
	session, err := hub.GetStatus(internalCtx, sessionID)
	if err != nil {
		return agentwork.ManagedWriterResolution{}, err
	}
	attempt, managed, err := h.store.ManagedSessionAssociation(ctx, sessionID)
	if err != nil {
		return agentwork.ManagedWriterResolution{}, err
	}
	if !managed {
		return agentwork.ManagedWriterResolution{}, nil
	}
	// Historical associations remain guarded. If their current host binding can
	// no longer be reconstructed, refusing the turn is safer than reclassifying
	// the old managed session as ordinary work.
	if attempt.SessionID != session.ID || attempt.ID == "" {
		return agentwork.ManagedWriterResolution{}, agentwork.ErrManagedLeaseScope
	}
	binding, err := h.ResolveManagedWriter(ctx, attempt)
	if err != nil {
		return agentwork.ManagedWriterResolution{}, err
	}
	return agentwork.ManagedWriterResolution{Managed: true, Binding: binding}, nil
}

// No Agent Hub status or in-memory runtime lookup proves that an external
// process has stopped writing. Until close completion is tied to a durable
// runtime generation and no-resume fence (or real isolation is available),
// ownership remains pinned.
func (h *managedOwnershipHost) CheckManagedWriterQuiescence(context.Context, agentwork.ManagedWriterLease) (agentwork.ManagedWriterQuiescence, error) {
	return agentwork.ManagedWriterQuiescence{}, agentwork.ErrManagedQuiescenceUnknown
}

func managedFolderForCanonicalPaths(sessionPath, worktreeRoot string, scope []string) string {
	canonicalSession, err := canonicalManagedPath(sessionPath)
	if err != nil {
		return ""
	}
	canonicalRoot, err := canonicalManagedPath(worktreeRoot)
	if err != nil {
		return ""
	}
	matchedID, matchedDepth := "", -1
	for _, entry := range scope {
		parts := strings.SplitN(entry, "|", 3)
		if len(parts) != 3 || parts[0] == "" {
			continue
		}
		allowedPath := parts[2]
		if parts[1] == "grant" {
			allowedPath, _, _ = strings.Cut(allowedPath, "|source|")
		} else if parts[1] != "project" {
			continue
		}
		canonicalScope, err := canonicalManagedPath(allowedPath)
		if err != nil || !workspace.Within(canonicalSession, canonicalScope) || !workspace.Within(canonicalRoot, canonicalScope) {
			continue
		}
		if len(canonicalScope) > matchedDepth {
			matchedID, matchedDepth = parts[0], len(canonicalScope)
		}
	}
	return matchedID
}

func canonicalManagedPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("managed path is empty")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func (h *managedOwnershipHost) packageScope(ctx context.Context, scope agentwork.PackageScope) (agentwork.GroupState, agentwork.WorkItem, agentwork.Attempt, error) {
	if err := ctx.Err(); err != nil {
		return agentwork.GroupState{}, agentwork.WorkItem{}, agentwork.Attempt{}, err
	}
	if h == nil || h.store == nil || h.container == nil || h.container.Vault == nil || h.container.Tasks == nil {
		return agentwork.GroupState{}, agentwork.WorkItem{}, agentwork.Attempt{}, agentwork.ErrManagedHostUnavailable
	}
	if h.container.Vault.IsLocked() {
		return agentwork.GroupState{}, agentwork.WorkItem{}, agentwork.Attempt{}, agentwork.ErrManagedPayloadLocked
	}
	group, err := h.store.GetWorkGroup(ctx, scope.GroupID)
	if err != nil {
		return agentwork.GroupState{}, agentwork.WorkItem{}, agentwork.Attempt{}, err
	}
	item, err := h.store.GetWorkItem(ctx, scope.WorkItemID)
	if err != nil {
		return agentwork.GroupState{}, agentwork.WorkItem{}, agentwork.Attempt{}, err
	}
	var attempt agentwork.Attempt
	if scope.AttemptID != "" {
		attempt, err = h.store.GetManagedAttempt(ctx, scope.AttemptID)
	} else {
		attempt, err = h.store.GetCurrentManagedAttempt(ctx, item.ID)
	}
	if err != nil {
		return agentwork.GroupState{}, agentwork.WorkItem{}, agentwork.Attempt{}, err
	}
	if group.WorkspaceID != scope.WorkspaceID || group.BoardID != scope.BoardID || group.ParentTaskID != scope.ParentTaskID ||
		item.GroupID != group.ID || item.TaskID != scope.TaskID || attempt.GroupID != group.ID || attempt.WorkItemID != item.ID ||
		(scope.Generation > 0 && attempt.Generation != scope.Generation) || (scope.ItemRevision > 0 && item.Revision != scope.ItemRevision) || (scope.AttemptRevision > 0 && attempt.Revision != scope.AttemptRevision) ||
		(scope.RequirementsRevision != "" && item.RequirementsRevision != scope.RequirementsRevision) || (scope.RequirementsRevision != "" && attempt.RequirementsRevision != scope.RequirementsRevision) {
		return agentwork.GroupState{}, agentwork.WorkItem{}, agentwork.Attempt{}, agentwork.ErrManagedPayloadStale
	}
	return group, item, attempt, nil
}

func (h *managedOwnershipHost) authorizePackage(ctx context.Context, scope agentwork.PackageScope, writer bool) (agentwork.GroupState, agentwork.WorkItem, agentwork.Attempt, error) {
	group, item, attempt, err := h.packageScope(ctx, scope)
	if err != nil {
		return group, item, attempt, err
	}
	caller, ok := identity.FromContext(ctx)
	if !ok {
		return group, item, attempt, agentwork.ErrManagedHostUnavailable
	}
	switch caller.Kind {
	case identity.KindUI:
		current, err := (&managedTeamHost{container: h.container}).CurrentManagedWorkspace(ctx)
		if err != nil || current != scope.WorkspaceID {
			return group, item, attempt, agentwork.ErrManagedLeaseScope
		}
	case identity.KindAgent:
		if caller.SessionID == "" || caller.SessionID != attempt.SessionID || attempt.SessionID == "" {
			return group, item, attempt, agentwork.ErrManagedLeaseScope
		}
		internalCtx := identity.WithCaller(ctx, identity.UI())
		hub := h.hub()
		if hub == nil {
			return group, item, attempt, agentwork.ErrManagedHostUnavailable
		}
		session, err := hub.GetStatus(internalCtx, caller.SessionID)
		if err != nil || session.Status == agenthub.SessionStatusClosed || session.WorkspaceID != scope.WorkspaceID {
			return group, item, attempt, agentwork.ErrManagedLeaseScope
		}
	default:
		return group, item, attempt, agentwork.ErrManagedLeaseScope
	}
	if writer && (item.CurrentAttemptID != attempt.ID || group.EndedAt != nil || item.EndedAt != nil || item.TombstoneReason != "") {
		return group, item, attempt, agentwork.ErrManagedPayloadStale
	}
	return group, item, attempt, nil
}

func (h *managedOwnershipHost) requirements(ctx context.Context, scope agentwork.PackageScope) (agentwork.RequirementsSnapshot, error) {
	group, _, _, err := h.packageScope(ctx, scope)
	if err != nil {
		return agentwork.RequirementsSnapshot{}, err
	}
	c := h.container
	hub := h.hub()
	if c == nil || hub == nil || c.Tasks == nil || c.Workspace == nil {
		return agentwork.RequirementsSnapshot{}, agentwork.ErrManagedHostUnavailable
	}
	task, err := c.Tasks.Get(managedTasksCaller(ctx), scope.TaskID)
	if err != nil || task.ID != scope.TaskID || task.ParentID != group.ParentTaskID || task.BoardID != group.BoardID {
		return agentwork.RequirementsSnapshot{}, agentwork.ErrManagedLeaseScope
	}
	board, err := c.Tasks.Board(managedTasksCaller(ctx), group.BoardID)
	if err != nil || board.ID != group.BoardID || board.WorkspaceID != group.WorkspaceID {
		return agentwork.RequirementsSnapshot{}, agentwork.ErrManagedLeaseScope
	}
	internalCtx := identity.WithCaller(ctx, identity.UI())
	coordinator, err := hub.GetStatus(internalCtx, group.CoordinatorID)
	if err != nil || coordinator.ID != group.CoordinatorID || coordinator.WorkspaceID != group.WorkspaceID || coordinator.Status == agenthub.SessionStatusClosed {
		return agentwork.RequirementsSnapshot{}, agentwork.ErrManagedLeaseScope
	}
	services, err := c.Workspace.ListServices(group.WorkspaceID)
	if err != nil {
		return agentwork.RequirementsSnapshot{}, err
	}
	folderScope, err := managedFolderScope(services, coordinator.ProjectPath, coordinator.ExtraReadWritePaths)
	if err != nil {
		return agentwork.RequirementsSnapshot{}, err
	}
	return agentwork.RequirementsSnapshot{WorkspaceID: group.WorkspaceID, BoardID: group.BoardID, TaskID: task.ID, Branch: task.Branch, Description: task.Description, Scope: folderScope, Dependencies: append([]string(nil), task.BlockedBy...)}, nil
}

func managedTasksCaller(ctx context.Context) tasks.Caller {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind == identity.KindAgent && caller.SessionID != "" {
		return tasks.Caller{SessionID: caller.SessionID}
	}
	return tasks.Caller{}
}

func (h *managedOwnershipHost) CurrentRequirements(ctx context.Context, scope agentwork.PackageScope) (agentwork.RequirementsSnapshot, error) {
	if _, _, _, err := h.authorizePackage(ctx, scope, false); err != nil {
		return agentwork.RequirementsSnapshot{}, err
	}
	return h.requirements(ctx, scope)
}

func (h *managedOwnershipHost) AuthorizePackageBuild(ctx context.Context, scope agentwork.PackageScope) error {
	_, item, attempt, err := h.authorizePackage(ctx, scope, true)
	if err != nil {
		return err
	}
	if attempt.State != "reserved" || item.CurrentAttemptID != attempt.ID {
		return agentwork.ErrManagedPayloadStale
	}
	return nil
}

func (h *managedOwnershipHost) AuthorizePackageRead(ctx context.Context, scope agentwork.PackageScope) error {
	_, _, _, err := h.authorizePackage(ctx, scope, false)
	return err
}

func (h *managedOwnershipHost) AuthorizeArtifactWrite(ctx context.Context, scope agentwork.PackageScope) error {
	_, item, attempt, err := h.authorizePackage(ctx, scope, true)
	if err != nil {
		return err
	}
	caller, ok := identity.FromContext(ctx)
	if !ok || caller.Kind != identity.KindAgent || caller.SessionID != attempt.SessionID || item.CurrentAttemptID != attempt.ID || attempt.SessionID == "" {
		return agentwork.ErrManagedPayloadStale
	}
	if h.container.ManagedLeases == nil {
		return agentwork.ErrManagedHostUnavailable
	}
	if err := h.container.ManagedLeases.AuthorizeAttemptWriter(ctx, attempt.ID); err != nil {
		return err
	}
	return nil
}

func (h *managedOwnershipHost) CurrentArtifactLeaseGeneration(ctx context.Context, scope agentwork.PackageScope) (int64, error) {
	_, item, attempt, err := h.authorizePackage(ctx, scope, true)
	if err != nil {
		return 0, err
	}
	caller, ok := identity.FromContext(ctx)
	if !ok || caller.Kind != identity.KindAgent || caller.SessionID == "" || caller.SessionID != attempt.SessionID || item.CurrentAttemptID != attempt.ID {
		return 0, agentwork.ErrManagedLeaseScope
	}
	if h.container.ManagedLeases == nil {
		return 0, agentwork.ErrManagedHostUnavailable
	}
	lease, err := h.container.ManagedLeases.CurrentAttemptLease(ctx, attempt.ID)
	if err != nil {
		return 0, err
	}
	return lease.Generation, nil
}

func (h *managedOwnershipHost) AuthorizeArtifactRead(ctx context.Context, metadata agentwork.ArtifactMetadata) error {
	scope := agentwork.PackageScope{WorkspaceID: metadata.WorkspaceID, BoardID: metadata.BoardID, ParentTaskID: metadata.ParentTaskID, GroupID: metadata.GroupID, WorkItemID: metadata.WorkItemID, TaskID: metadata.TaskID, AttemptID: metadata.AttemptID, Generation: metadata.Generation, RequirementsRevision: metadata.RequirementsRevision}
	_, _, attempt, err := h.authorizePackage(ctx, scope, false)
	if err != nil {
		return err
	}
	if metadata.ProducerSessionID == "" || metadata.ProducerSessionID != attempt.SessionID {
		return agentwork.ErrManagedLeaseScope
	}
	if metadata.LeaseGeneration < 1 {
		return agentwork.ErrManagedPayloadScope
	}
	return nil
}

func (h *managedOwnershipHost) AuthorizeArtifactList(ctx context.Context, scope agentwork.PackageScope) error {
	_, _, _, err := h.authorizePackage(ctx, scope, false)
	return err
}

func (h *managedOwnershipHost) BuildPackageSource(ctx context.Context, scope agentwork.PackageScope) (agentwork.PackageSource, error) {
	if _, _, _, err := h.authorizePackage(ctx, scope, true); err != nil {
		return agentwork.PackageSource{}, err
	}
	requirements, err := h.requirements(ctx, scope)
	if err != nil {
		return agentwork.PackageSource{}, err
	}
	_, item, attempt, err := h.authorizePackage(ctx, scope, true)
	if err != nil {
		return agentwork.PackageSource{}, err
	}
	contract, output, err := managedRoleContract(item.Role)
	if err != nil {
		return agentwork.PackageSource{}, err
	}
	if item.Role != attempt.Role {
		return agentwork.PackageSource{}, agentwork.ErrManagedPayloadStale
	}
	// Selected references are empty until a separate host-owned selector exists.
	return agentwork.PackageSource{Requirements: requirements, Role: item.Role, RoleContract: contract, Output: output}, nil
}

func managedRoleContract(role string) (string, agentwork.PackageOutputContract, error) {
	output := agentwork.PackageOutputContract{
		AllowedOutcomes: []string{"ready_for_review", "blocked", "failed"},
		EvidenceKinds:   []string{"file", "patch", "test_output", "other"},
		EscalateOn:      []string{"requirements_changed", "scope_unavailable", "verification_failed"},
	}
	switch role {
	case "coordinator":
		return "Coordinate only the assigned work item. Keep changes within the supplied registered folder scope, summarize dependencies and blockers, and do not claim acceptance. Evidence is an agent claim and has not been host-verified.", output, nil
	case "implementer":
		return "Implement only the assigned task. Work within the supplied registered folder scope, follow the task requirements, and report concrete evidence. Do not claim acceptance. Evidence is an agent claim and has not been host-verified.", output, nil
	case "reviewer":
		return "Review the assigned task result against its requirements. Report findings and evidence; do not modify task status or claim acceptance. Evidence is an agent claim and has not been host-verified.", output, nil
	case "integrator":
		return "Integrate only the assigned changes within the supplied registered folder scope. Report the resulting diff and verification evidence; do not claim acceptance. Evidence is an agent claim and has not been host-verified.", output, nil
	default:
		return "", agentwork.PackageOutputContract{}, fmt.Errorf("%w: unsupported managed role", agentwork.ErrManagedPayloadScope)
	}
}

var _ agentwork.ManagedLeaseHost = (*managedOwnershipHost)(nil)
var _ agentwork.PackageHost = (*managedOwnershipHost)(nil)
var _ agentwork.PayloadCodec = managedVaultPayloadCodec{}
