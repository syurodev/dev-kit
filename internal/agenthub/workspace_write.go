package agenthub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devkit/internal/identity"
)

// WorkspaceFolder is one folder (service) of a workspace as a write request sees it.
type WorkspaceFolder struct {
	Name string
	Path string
}

// GrantRefusedError is a request for folder access that cannot be granted. Reason
// is a fixed sentence: it names no path, and no folder the caller did not name.
type GrantRefusedError struct{ Reason string }

func (e *GrantRefusedError) Error() string { return e.Reason }

func refuseGrant(reason string) error { return &GrantRefusedError{Reason: reason} }

// WriteGrantPlan is what the person is asked to approve, built by
// PrepareWorkspaceWrite and checked again, whole, by CommitWorkspaceWrite.
type WriteGrantPlan struct {
	SessionID      string
	WorkspaceID    string
	WorkspaceName  string
	Folder         string
	Path           string
	AlreadyGranted bool
}

// When each reader sees a granted folder (WriteGrantEffect).
const (
	EffectNow           = "now"
	EffectAfterThisTurn = "after_this_turn" // DevKit will try to reopen the process when the turn ends
	EffectAfterReopen   = "after_reopen"    // DevKit cannot now; the next reopen applies it
)

// WriteGrantEffect says when git, new sub-sessions, and the session's own shell and
// file tools see the folder.
type WriteGrantEffect struct {
	Git           string `json:"git"`
	NewSessions   string `json:"newSessions"`
	ShellAndFiles string `json:"shellAndFiles"`
}

// WriteGrantResult is what the agent is told.
type WriteGrantResult struct {
	Folder         string           `json:"folder"`
	Granted        bool             `json:"granted"`
	AlreadyGranted bool             `json:"alreadyGranted"`
	Effective      WriteGrantEffect `json:"effective"`
}

// PrepareWorkspaceWrite runs every check of a request for write access to one folder
// of the caller's own workspace and returns what the person should be asked. It
// stores nothing. See docs/superpowers/specs/2026-10-29-agent-write-grant-design.md.
func (s *Service) PrepareWorkspaceWrite(ctx context.Context, folder string) (WriteGrantPlan, error) {
	caller, _ := identity.FromContext(ctx)
	if !caller.IsAgent() {
		return WriteGrantPlan{}, refuseGrant("Only an agent session can ask for access to a folder.")
	}
	return s.planWriteGrant(ctx, caller.SessionID, strings.TrimSpace(folder))
}

func (s *Service) planWriteGrant(ctx context.Context, sessionID, folder string) (WriteGrantPlan, error) {
	if folder == "" {
		return WriteGrantPlan{}, refuseGrant("Say which folder: pass its name as workspace.list shows it.")
	}
	sess, err := s.store.GetSession(ctx, sessionID)
	if errors.Is(err, ErrSessionNotFound) {
		return WriteGrantPlan{}, refuseGrant("This session no longer exists.")
	}
	if err != nil {
		return WriteGrantPlan{}, err
	}
	switch {
	case sess.Status == SessionStatusClosed:
		return WriteGrantPlan{}, refuseGrant("This session is closed.")
	case sess.ParentSessionID != nil:
		return WriteGrantPlan{}, refuseGrant("A sub-session cannot hold more folders than its parent. Ask your parent session to request it, or ask the person.")
	case sess.WorkspaceID == "":
		return WriteGrantPlan{}, refuseGrant("This session is not in a workspace.")
	case s.WorkspaceFolders == nil:
		return WriteGrantPlan{}, refuseGrant("Workspace folders are not available.")
	}
	workspaceName, folders, err := s.WorkspaceFolders(sess.WorkspaceID)
	if err != nil {
		return WriteGrantPlan{}, err
	}
	path := ""
	for _, f := range folders {
		if f.Name == folder {
			path = filepath.Clean(f.Path)
			break
		}
	}
	if path == "" || path == "." {
		return WriteGrantPlan{}, refuseGrant(fmt.Sprintf("%q is not a folder of this workspace. Call workspace.list to see its folders.", folder))
	}
	home, _ := os.UserHomeDir()
	if !filepath.IsAbs(path) || path == string(filepath.Separator) || (home != "" && path == filepath.Clean(home)) {
		return WriteGrantPlan{}, refuseGrant("That folder cannot be granted.")
	}
	writable := append([]string{sess.ProjectPath}, sess.ExtraReadWritePaths...)
	return WriteGrantPlan{
		SessionID: sessionID, WorkspaceID: sess.WorkspaceID, WorkspaceName: workspaceName,
		Folder: folder, Path: path, AlreadyGranted: folderCovered(path, writable),
	}, nil
}

// folderCovered reports whether path is one of roots or inside one. A folder whose
// name merely starts like a root's does not count.
func folderCovered(path string, roots []string) bool {
	path = filepath.Clean(path)
	for _, root := range roots {
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// CommitWorkspaceWrite stores a folder the person approved. The wait for the answer can
// be long, so every check runs again against the stored session and the current
// workspace; a folder that is no longer the one the person saw is refused.
func (s *Service) CommitWorkspaceWrite(ctx context.Context, plan WriteGrantPlan) (WriteGrantResult, error) {
	caller, _ := identity.FromContext(ctx)
	if !caller.IsAgent() || caller.SessionID != plan.SessionID {
		return WriteGrantResult{}, refuseGrant("This approval belongs to another session.")
	}
	now, err := s.planWriteGrant(ctx, plan.SessionID, plan.Folder)
	if err != nil {
		return WriteGrantResult{}, err
	}
	if now.Path != plan.Path {
		return WriteGrantResult{}, refuseGrant("The folder changed while the person was answering. Ask again.")
	}
	result := WriteGrantResult{Folder: plan.Folder, Granted: true, Effective: WriteGrantEffect{Git: EffectNow, NewSessions: EffectNow}}
	if now.AlreadyGranted {
		result.AlreadyGranted = true
		result.Effective.ShellAndFiles = EffectNow
		return result, nil
	}
	store, ok := s.store.(ExtraPathStore)
	if !ok {
		return WriteGrantResult{}, errors.New("agenthub: this store cannot add a folder grant")
	}
	if _, err := store.AddExtraReadWritePath(ctx, plan.SessionID, now.Path); err != nil {
		if errors.Is(err, ErrExtraPathsContended) {
			return WriteGrantResult{}, refuseGrant("The session's folders kept changing. Ask again.")
		}
		return WriteGrantResult{}, err
	}
	result.Effective.ShellAndFiles = s.scheduleReopenForGrant(ctx, plan.SessionID)
	return result, nil
}

// scheduleReopenForGrant marks the session so its process is reopened when the
// turn ends (reopen_grant.go), and says whether DevKit expects to. A vendor that
// cannot load a session again is never marked: releasing it would end the conversation.
func (s *Service) scheduleReopenForGrant(ctx context.Context, id string) string {
	sess, err := s.store.GetSession(ctx, id)
	if err != nil || !s.reopenable(ctx, sess) {
		return EffectAfterReopen
	}
	s.mu.Lock()
	if s.reopenForGrant == nil {
		s.reopenForGrant = make(map[string]bool)
	}
	s.reopenForGrant[id] = true
	s.mu.Unlock()
	return EffectAfterThisTurn
}
