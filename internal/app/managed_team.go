package app

import (
	"context"
	"errors"
	"path/filepath"
	"sort"

	"devkit/internal/agenthub"
	"devkit/internal/agentwork"
	"devkit/internal/identity"
	"devkit/internal/tasks"
	"devkit/internal/vaultitem"
	"devkit/internal/workfolder"
	"devkit/internal/workspace"
)

var errManagedHumanRequired = errors.New("managed-work mutations require a current human caller")

// WireManagedTeams installs the host adapter and metadata service after the
// composition root has opened its local database. The app package accepts the
// store as an interface and does not depend on internal/storage.
func WireManagedTeams(c *Container, store agentwork.ManagedStore) {
	if c == nil || c.Tasks == nil || store == nil {
		return
	}
	host := &managedTeamHost{container: c}
	service := agentwork.NewManagedService(host, store)
	c.ManagedTeams = service
	policy := &managedTerminalPolicy{service: service, tasks: c.Tasks}
	c.managedTerminalPolicy = policy
	c.Tasks.SetTerminalPolicy(policy)
}

type managedTeamHost struct {
	container   *Container
	hubOverride agentSessions
}

func (h *managedTeamHost) AuthorizeManagedHuman(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	caller, ok := identity.FromContext(ctx)
	if !ok {
		return errManagedHumanRequired
	}
	switch caller.Kind {
	case identity.KindUI:
		return nil
	default:
		return errManagedHumanRequired
	}
}

func (h *managedTeamHost) CurrentManagedWorkspace(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := h.AuthorizeManagedHuman(ctx); err != nil {
		return "", err
	}
	c := h.container
	if c == nil || c.Workspace == nil || c.WorkFolder == nil {
		return "", agentwork.ErrManagedHostUnavailable
	}
	root := c.WorkFolder.Effective(workfolder.ModuleTasks)
	if root == "" {
		return "", agentwork.ErrManagedHostUnavailable
	}
	root = filepath.Clean(root)
	workspaces, err := c.Workspace.ListWorkspaces()
	if err != nil {
		return "", err
	}
	var match string
	for _, ws := range workspaces {
		services, err := c.Workspace.ListServices(ws.ID)
		if err != nil {
			return "", err
		}
		for _, folder := range services {
			if !workspace.Within(root, folder.Path) {
				continue
			}
			if match != "" && match != ws.ID {
				return "", agentwork.ErrManagedHostUnavailable
			}
			match = ws.ID
		}
	}
	if match == "" {
		return "", agentwork.ErrManagedHostUnavailable
	}
	return match, nil
}

func (h *managedTeamHost) ResolveManagedParent(ctx context.Context, parentTaskID string) (agentwork.ManagedParentSnapshot, error) {
	return h.resolveManagedParent(ctx, parentTaskID, nil)
}

func (h *managedTeamHost) ResolveManagedParentForGroup(ctx context.Context, group agentwork.GroupState) (agentwork.ManagedParentSnapshot, error) {
	workspaceID, err := h.CurrentManagedWorkspace(ctx)
	if err != nil {
		return agentwork.ManagedParentSnapshot{}, err
	}
	if workspaceID != group.WorkspaceID {
		return agentwork.ManagedParentSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	return h.resolveManagedParent(ctx, group.ParentTaskID, &group)
}

func (h *managedTeamHost) resolveManagedParent(ctx context.Context, parentTaskID string, group *agentwork.GroupState) (agentwork.ManagedParentSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return agentwork.ManagedParentSnapshot{}, err
	}
	c := h.container
	if c == nil || c.Tasks == nil || c.Vault == nil {
		return agentwork.ManagedParentSnapshot{}, agentwork.ErrManagedHostUnavailable
	}
	workspaceID, err := h.CurrentManagedWorkspace(ctx)
	if err != nil {
		return agentwork.ManagedParentSnapshot{}, err
	}
	if c.Vault.IsLocked() {
		return agentwork.ManagedParentSnapshot{}, agentwork.ErrManagedHostUnavailable
	}
	parent, err := c.Tasks.Get(tasks.Caller{}, parentTaskID)
	if err != nil {
		if group != nil && errors.Is(err, tasks.ErrNotFound) && h.managedTaskDefinitelyMissing(parentTaskID) {
			return agentwork.ManagedParentSnapshot{
				WorkspaceID: group.WorkspaceID, BoardID: group.BoardID, ParentTaskID: group.ParentTaskID,
				CoordinatorID: group.CoordinatorID, Revision: 1, Deleted: true,
			}, nil
		}
		return agentwork.ManagedParentSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	board, err := c.Tasks.Board(tasks.Caller{}, parent.BoardID)
	if err != nil || board.WorkspaceID == "" || board.WorkspaceID != workspaceID {
		return agentwork.ManagedParentSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	if parent.SessionID == "" {
		return agentwork.ManagedParentSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	var hub agentSessions
	if c.AgentHub != nil {
		hub = c.AgentHub
	}
	if h.hubOverride != nil {
		hub = h.hubOverride
	}
	if hub == nil {
		return agentwork.ManagedParentSnapshot{}, agentwork.ErrManagedHostUnavailable
	}
	coordinator, err := hub.GetStatus(ctx, parent.SessionID)
	if err != nil || coordinator.ID != parent.SessionID || coordinator.WorkspaceID != workspaceID || coordinator.Status == agenthub.SessionStatusClosed {
		return agentwork.ManagedParentSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	category, known := board.Workflow.CategoryOf(parent.Status)
	if !known {
		return agentwork.ManagedParentSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	statuses := make([]agentwork.BoardStatus, 0, len(board.Workflow.Statuses))
	for _, status := range board.Workflow.Statuses {
		statuses = append(statuses, agentwork.BoardStatus{ID: status.ID, Category: string(status.Category), WontDo: status.ID == "wont_do"})
	}
	return agentwork.ManagedParentSnapshot{
		WorkspaceID: workspaceID, BoardID: board.ID, ParentTaskID: parent.ID,
		CoordinatorID: coordinator.ID, Revision: 1, StatusID: parent.Status,
		Archived: parent.ArchivedAt != "", Terminal: category == tasks.CategoryTerminal,
		Statuses: statuses,
	}, nil
}

func (h *managedTeamHost) LoadManagedTask(ctx context.Context, group agentwork.GroupState, taskID string) (agentwork.ManagedTaskSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return agentwork.ManagedTaskSnapshot{}, err
	}
	c := h.container
	if c == nil || c.Tasks == nil || c.Vault == nil {
		return agentwork.ManagedTaskSnapshot{}, agentwork.ErrManagedHostUnavailable
	}
	workspaceID, err := h.CurrentManagedWorkspace(ctx)
	if err != nil || workspaceID != group.WorkspaceID {
		return agentwork.ManagedTaskSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	if c.Vault.IsLocked() {
		return agentwork.ManagedTaskSnapshot{}, agentwork.ErrManagedHostUnavailable
	}
	caller := tasks.Caller{}
	task, err := c.Tasks.Get(caller, taskID)
	if err != nil {
		if errors.Is(err, tasks.ErrNotFound) && h.managedTaskDefinitelyMissing(taskID) {
			return agentwork.ManagedTaskSnapshot{
				WorkspaceID: group.WorkspaceID, BoardID: group.BoardID,
				ParentTaskID: group.ParentTaskID, TaskID: taskID, Revision: 1, Deleted: true,
			}, nil
		}
		return agentwork.ManagedTaskSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	board, err := c.Tasks.Board(caller, task.BoardID)
	if err != nil || board.WorkspaceID == "" || board.WorkspaceID != group.WorkspaceID || board.ID != group.BoardID || task.ParentID != group.ParentTaskID {
		return agentwork.ManagedTaskSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	var hub agentSessions
	if c.AgentHub != nil {
		hub = c.AgentHub
	}
	if h.hubOverride != nil {
		hub = h.hubOverride
	}
	if hub == nil {
		return agentwork.ManagedTaskSnapshot{}, agentwork.ErrManagedHostUnavailable
	}
	coordinator, err := hub.GetStatus(ctx, group.CoordinatorID)
	if err != nil || coordinator.ID != group.CoordinatorID || coordinator.WorkspaceID != group.WorkspaceID || coordinator.Status == agenthub.SessionStatusClosed {
		return agentwork.ManagedTaskSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	services, err := c.Workspace.ListServices(workspaceID)
	if err != nil {
		return agentwork.ManagedTaskSnapshot{}, err
	}
	scope, err := managedFolderScope(services, coordinator.ProjectPath, coordinator.ExtraReadWritePaths)
	if err != nil {
		return agentwork.ManagedTaskSnapshot{}, err
	}
	category, known := board.Workflow.CategoryOf(task.Status)
	if !known {
		return agentwork.ManagedTaskSnapshot{}, agentwork.ErrManagedTaskInvalid
	}
	requirements := agentwork.RequirementsSnapshot{
		WorkspaceID: workspaceID, BoardID: board.ID, TaskID: task.ID,
		Branch: task.Branch, Description: task.Description,
		Scope: scope, Dependencies: append([]string(nil), task.BlockedBy...),
	}
	return agentwork.ManagedTaskSnapshot{
		WorkspaceID: workspaceID, BoardID: board.ID, ParentTaskID: task.ParentID,
		TaskID: task.ID, Revision: 1, StatusID: task.Status,
		StatusCategory: string(category), Archived: task.ArchivedAt != "",
		Terminal: category == tasks.CategoryTerminal, WontDo: task.Status == "wont_do",
		Requirements: requirements,
	}, nil
}

// Tasks.Get currently reports ErrNotFound for repository failures as well as
// missing IDs. Only a definite repository miss may create a durable tombstone;
// transient or corrupt reads must leave the ledger unchanged for a later read.
func (h *managedTeamHost) managedTaskDefinitelyMissing(id string) bool {
	if h.container == nil || h.container.Items == nil {
		return false
	}
	_, err := h.container.Items.Get(id)
	return errors.Is(err, vaultitem.ErrNotFound)
}

// managedFolderScope narrows a coordinator's stored paths to registered
// workspace folders. Each digest member carries the folder identity and the
// exact granted path; sibling folders never become implicit access.
func managedFolderScope(services []workspace.WorkspaceService, projectPath string, extraPaths []string) ([]string, error) {
	if projectPath == "" {
		return nil, agentwork.ErrManagedTaskInvalid
	}
	var scope []string
	projectServiceID := ""
	projectServiceDepth := -1
	for _, service := range services {
		if service.ID == "" || service.Path == "" {
			return nil, agentwork.ErrManagedHostUnavailable
		}
		if workspace.Within(projectPath, service.Path) {
			depth := len(filepath.Clean(service.Path))
			if depth > projectServiceDepth {
				projectServiceID, projectServiceDepth = service.ID, depth
			} else if depth == projectServiceDepth && projectServiceID != service.ID {
				return nil, agentwork.ErrManagedTaskInvalid
			}
		}
	}
	if projectServiceID == "" {
		return nil, agentwork.ErrManagedTaskInvalid
	}
	scope = append(scope, projectServiceID+"|project|"+filepath.Clean(projectPath))
	for _, grant := range extraPaths {
		if grant == "" {
			return nil, agentwork.ErrManagedTaskInvalid
		}
		grant = filepath.Clean(grant)
		matches := 0
		for _, service := range services {
			if service.ID == "" || service.Path == "" {
				return nil, agentwork.ErrManagedHostUnavailable
			}
			if workspace.Within(grant, service.Path) || workspace.Within(service.Path, grant) {
				effectiveGrant := grant
				if workspace.Within(service.Path, grant) {
					effectiveGrant = filepath.Clean(service.Path)
				}
				scope = append(scope, service.ID+"|grant|"+effectiveGrant+"|source|"+grant)
				matches++
			}
		}
		if matches == 0 {
			return nil, agentwork.ErrManagedTaskInvalid
		}
	}
	sort.Strings(scope)
	return scope, nil
}

func (h *managedTeamHost) ValidateManagedEligibility(ctx context.Context, parent agentwork.ManagedParentSnapshot, statusMap agentwork.StatusMap) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if h.container == nil || h.container.Tasks == nil || h.container.Vault == nil || h.container.Vault.IsLocked() {
		return agentwork.ErrManagedIneligible
	}
	if h.container.Interaction == nil || h.container.Interaction.Headless || h.container.managedTerminalPolicy == nil || !h.container.Tasks.HasTerminalPolicy(h.container.managedTerminalPolicy) {
		return agentwork.ErrManagedIneligible
	}
	if err := statusMap.Validate(parent.Statuses); err != nil {
		return err
	}
	return nil
}

type managedTerminalPolicy struct {
	service *agentwork.ManagedService
	tasks   *tasks.Service
}

func (p *managedTerminalPolicy) CheckTerminal(ctx context.Context, caller tasks.Caller, task tasks.Task, _ string) error {
	if !caller.IsAgent() {
		return nil
	}
	if p.service == nil || p.tasks == nil {
		return tasks.ErrManagedTerminalNotReady
	}
	board, err := p.tasks.Board(caller, task.BoardID)
	if err != nil {
		return err
	}
	managed, err := p.service.ManagedTaskMembership(ctx, board.ID, task.ID)
	if err != nil {
		return tasks.ErrManagedTerminalNotReady
	}
	if managed {
		return tasks.ErrManagedTerminalNotReady
	}
	return nil
}

var _ agentwork.ManagedHost = (*managedTeamHost)(nil)
var _ tasks.TerminalPolicy = (*managedTerminalPolicy)(nil)
