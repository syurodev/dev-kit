package app

import "devkit/internal/agenthub"

// AgentWorkspaceFolders answers Agent Hub's question "what is this workspace called
// and which folders does it hold" for a request for write access
// (agenthub.Service.WorkspaceFolders). An unknown workspace is an empty answer, not
// an error: the request is then refused for naming a folder that is not there.
func AgentWorkspaceFolders(ws workspaceLister) func(workspaceID string) (string, []agenthub.WorkspaceFolder, error) {
	return func(workspaceID string) (string, []agenthub.WorkspaceFolder, error) {
		all, err := ws.ListWorkspaces()
		if err != nil {
			return "", nil, err
		}
		for _, w := range all {
			if w.ID != workspaceID {
				continue
			}
			services, err := ws.ListServices(w.ID)
			if err != nil {
				return "", nil, err
			}
			folders := make([]agenthub.WorkspaceFolder, 0, len(services))
			for _, svc := range services {
				folders = append(folders, agenthub.WorkspaceFolder{Name: svc.Name, Path: svc.Path})
			}
			return w.Name, folders, nil
		}
		return "", nil, nil
	}
}
