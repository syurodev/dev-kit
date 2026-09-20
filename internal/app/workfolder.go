package app

import (
	"context"
	"sync"
	"time"

	"devkit/internal/workfolder"
)

// gitOpenTimeout bounds the `git rev-parse` a folder change waits on. It is a
// local work tree and answers in milliseconds; the bound is for a folder on a
// network mount that has stopped answering, which must not hold up whoever
// opened the project.
const gitOpenTimeout = 5 * time.Second

// WireWorkFolder makes Git follow the work folder: whenever the folder Git
// should be in changes — the current folder moved, or Git was pinned or sent
// back to following — the repository is reopened there, or let go when the
// folder is not one.
//
// It runs before the change is announced to the frontend, because the
// subscriber that announces is registered after this one and callbacks run in
// order. A Git view that refreshes on the announcement therefore finds the
// repository already switched, rather than racing it.
//
// It applies the state as it is now, too: the last project is restored during
// startup, and that can land before anything is subscribed.
func WireWorkFolder(c *Container) {
	if c == nil || c.WorkFolder == nil || c.Git == nil {
		return
	}
	// Serialised, and reading the state afresh rather than trusting the one it
	// was called with: two changes on two goroutines can run their callbacks in
	// either order, and only the latest state is worth applying.
	var mu sync.Mutex
	apply := func() {
		mu.Lock()
		defer mu.Unlock()
		folder := c.WorkFolder.Effective(workfolder.ModuleGit)
		if folder == "" {
			c.Git.Reset()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), gitOpenTimeout)
		defer cancel()
		if err := c.Git.Open(ctx, folder); err != nil {
			// Not a repository, or git is unavailable: either way there is
			// nothing of the previous folder's to keep showing.
			c.Git.Reset()
		}
	}
	c.WorkFolder.Subscribe(func(workfolder.State) { apply() })
	apply()
}
