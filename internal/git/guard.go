package git

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// Guard binds an environment tag to a branch name pattern. A branch matching
// one runs commit and push through that environment's confirmation.
//
// Push is gated by its remote regardless; a guard is what makes *commit* ask,
// which matters for the repository whose commits are deploy manifests. No
// guard means nothing is gated — the default is not to be in anyone's way.
//
// The pattern matches names, not paths: "main", "release/*". It cannot say
// "when this touches deploy/", which is a real ceiling and written down.
type Guard struct {
	ID         string `json:"id"`
	ProjectKey string `json:"projectKey"`
	Pattern    string `json:"pattern"`
	Env        string `json:"env"`
}

// Matches reports whether a branch name falls under the guard.
func (g Guard) Matches(branch string) bool {
	if g.Pattern == "" || branch == "" {
		return false
	}
	if g.Pattern == branch {
		return true
	}
	ok, err := path.Match(g.Pattern, branch)
	return err == nil && ok
}

// GuardStore is where guards live. They are vault items, so a rule about a
// repository follows the user between machines — it is a property of the
// repository, not of the laptop.
type GuardStore interface {
	ListGuards(projectKey string) ([]Guard, error)
}

// guardFor returns the first guard covering branch, if any.
func (s *Service) guardFor(branch string) (Guard, bool) {
	if s.guards == nil || branch == "" {
		return Guard{}, false
	}
	guards, err := s.guards.ListGuards(s.Project())
	if err != nil {
		// A guard store that cannot be read must not silently un-gate a
		// protected branch. Refuse by reporting a guard with no env, which
		// confirm() turns into a denial.
		return Guard{Pattern: branch, Env: ""}, true
	}
	for _, guard := range guards {
		if guard.Matches(branch) {
			return guard, true
		}
	}
	return Guard{}, false
}

// gate asks the user when the branch is guarded and its environment demands
// it. It fails closed: no confirmer (headless, server mode) means no.
func (s *Service) gate(ctx context.Context, branch, what, detail string) error {
	guard, guarded := s.guardFor(branch)
	if !guarded {
		return nil
	}
	if guard.Env != "" && s.envs != nil && !s.envs.RequiresConfirmation(guard.Env) {
		return nil
	}
	if s.confirm == nil {
		return fmt.Errorf("%w: %s on %s needs the person's confirmation and nobody here can be asked", ErrNotAllowed, what, branch)
	}
	title := what + " on " + branch
	if guard.Env != "" {
		title += " (" + guard.Env + ")"
	}
	req := Confirmation{Title: title, Detail: detail, Branch: branch, Env: guard.Env, Action: what}
	if what == "Push" && strings.HasPrefix(detail, "to ") {
		req.Remote = strings.TrimPrefix(detail, "to ")
		target, err := s.remoteNamed(ctx, req.Remote, true)
		if err != nil {
			return err
		}
		req.Destination = safeDestination(target.URL)
	}
	allowed, err := s.confirmGit(ctx, req)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("%w: the person declined %s on %s", ErrNotAllowed, what, branch)
	}
	return nil
}

// protectedForAgent reports whether an agent may push to branch.
//
// The rule used to be "a branch it created in this session", which dies at the
// first restart. It is now "a branch written on a task the agent holds", which
// is durable and synced because the task is. Nothing on a task means commit
// only.
func protectedForAgent(branch, defaultBranch string, taskBranches []string) error {
	if branch == "" {
		return fmt.Errorf("%w: HEAD is not on a branch, so there is nothing to push", ErrProtectedBranch)
	}
	if branch == defaultBranch || branch == "main" || branch == "master" {
		return fmt.Errorf("%w: %s is the default branch, which an agent never pushes to — push a branch written on a task you hold", ErrProtectedBranch, branch)
	}
	for _, allowed := range taskBranches {
		if allowed == branch {
			return nil
		}
	}
	return fmt.Errorf("%w: %s is not a branch recorded on a task you hold — an agent pushes only those", ErrProtectedBranch, branch)
}

// BranchNameFor turns a task key and title into a branch name. Slugging keeps
// it to what every git implementation accepts on every filesystem.
func BranchNameFor(template, key, title string) string {
	if template == "" {
		template = "{key}-{slug}"
	}
	name := strings.ReplaceAll(template, "{key}", key)
	name = strings.ReplaceAll(name, "{slug}", slug(title))
	return strings.Trim(name, "-/")
}

func slug(title string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= 40 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}
