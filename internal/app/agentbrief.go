package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"devkit/internal/workspace"
)

// workspaceLister is the slice of *workspace.Service a brief needs.
type workspaceLister interface {
	ListWorkspaces() ([]workspace.Workspace, error)
	ListServices(workspaceID string) ([]workspace.WorkspaceService, error)
}

// maxBriefFolders bounds one workspace's listing in the prompt. A workspace of
// fifty services is still one line per service here, and past this the rest is
// what workspace.list is for.
const maxBriefFolders = 25

// AgentSessionBrief says where an Agent Hub session is, for its prompt: the
// workspace its working directory belongs to, every folder of it, and which
// of them the session may write.
//
// It is the answer workspace.list would give, handed over up front. An agent
// told "call workspace.list if unclear" never finds its situation unclear —
// its cwd is right there — and so never learns that the payment service it is
// about to reimplement is a sibling folder it could read. Naming the folders
// is also what makes `repo: "<name>"` on the git tools something it can use
// without a round trip.
func AgentSessionBrief(ws workspaceLister) func(cwd string, granted []string) string {
	return func(cwd string, granted []string) string {
		if ws == nil || cwd == "" {
			return ""
		}
		all, err := ws.ListWorkspaces()
		if err != nil {
			return ""
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Working directory: %s\n", cwd)
		found := false
		for _, w := range all {
			services, err := ws.ListServices(w.ID)
			if err != nil || !holds(services, cwd, granted) {
				continue
			}
			found = true
			fmt.Fprintf(&b, "\nWorkspace %q — one folder per service, primary first:\n", w.Name)
			for i, svc := range services {
				if i == maxBriefFolders {
					fmt.Fprintf(&b, "- …and %d more (%s)\n", len(services)-i, "workspace.list")
					break
				}
				var notes []string
				if svc.Path == w.Root {
					notes = append(notes, "primary")
				}
				if workspace.Within(cwd, svc.Path) {
					notes = append(notes, "you are here")
				}
				switch {
				case workspace.Covered(svc.Path, granted):
					notes = append(notes, "you may write")
				case workspace.Within(cwd, svc.Path):
					// Started in a subfolder: that subfolder is writable, the
					// rest of the service is not.
					notes = append(notes, "you may write only under your working directory")
				default:
					notes = append(notes, "read-only")
				}
				fmt.Fprintf(&b, "- %s — %s (%s)\n", svc.Name, svc.Path, strings.Join(notes, ", "))
			}
		}
		if !found {
			b.WriteString("\nThis folder is not part of a DevKit workspace.\n")
		}
		if extra := outside(all, ws, granted, cwd); len(extra) > 0 {
			b.WriteString("\nAlso yours to write: " + strings.Join(extra, ", ") + "\n")
		}
		b.WriteString("\nRead any folder listed; change only the ones you may write. To work in " +
			"another service, pass its name as `repo` to the git tools and its absolute path to " +
			"the codegraph tools.")
		return b.String()
	}
}

// holds reports whether a workspace is this session's: its cwd or one of its
// granted folders is inside one of the workspace's services.
func holds(services []workspace.WorkspaceService, cwd string, granted []string) bool {
	for _, svc := range services {
		if workspace.Within(cwd, svc.Path) || workspace.Covered(svc.Path, granted) {
			return true
		}
	}
	return false
}

// outside is the granted folders no workspace lists — a path the person added
// to the session by hand — which the listing above would otherwise leave out.
func outside(all []workspace.Workspace, ws workspaceLister, granted []string, cwd string) []string {
	var out []string
	for _, path := range granted {
		if path == "" || filepath.Clean(path) == filepath.Clean(cwd) {
			continue
		}
		listed := false
		for _, w := range all {
			services, err := ws.ListServices(w.ID)
			if err != nil {
				continue
			}
			for _, svc := range services {
				if workspace.Within(path, svc.Path) {
					listed = true
					break
				}
			}
			if listed {
				break
			}
		}
		if !listed {
			out = append(out, path)
		}
	}
	return out
}
