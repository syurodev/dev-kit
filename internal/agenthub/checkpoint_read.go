// internal/agenthub/checkpoint_read.go
package agenthub

import (
	"context"
	"fmt"
	"strings"
)

// Reading checkpoints back.
//
// Both reads go through authorizeTarget like every other session read: an agent
// asking after another agent's checkpoints is refused by the capability
// gateway, not by the UI.

// FileDiff is one path's two versions, for the drawer behind a changed file.
type FileDiff struct {
	Path string `json:"path"`
	// Before and After are the file's bytes as text. A file git called binary
	// is not sent at all.
	Before string `json:"before"`
	After  string `json:"after"`
	Binary bool   `json:"binary"`
	// Missing is a side the checkpoint does not hold — the file was added
	// (no before) or deleted (no after), or the snapshot on that side failed.
	BeforeMissing bool `json:"beforeMissing"`
	AfterMissing  bool `json:"afterMissing"`
	// Truncated says the file is too big to preview. Not a shortened diff: half
	// a file would draw a difference at the cut that is not in the file.
	Truncated bool `json:"truncated"`
}

// maxFileDiffBytes is what a diff drawer will load per side.
//
// Separate from the checkpoint ceiling, which the user can raise to a gigabyte:
// snapshotting a large file is cheap and useful, and pushing it through JSON
// into a line diff in the renderer is neither.
const maxFileDiffBytes = 1 << 20

// ListCheckpoints returns a session's checkpoints, oldest first, narrowed to
// what the transcript shows.
//
// Summaries rather than records: the vendor's own changed-file report, the skip
// list and the detail blob are read by nothing on the other side of the bridge,
// and this list travels on every session open.
func (s *Service) ListCheckpoints(ctx context.Context, sessionID string) ([]CheckpointSummary, error) {
	if err := s.authorizeTarget(ctx, sessionID); err != nil {
		return nil, err
	}
	records, err := s.store.ListCheckpointSummaries(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if records == nil {
		records = []CheckpointSummary{}
	}
	return records, nil
}

// CheckpointFileDiff reads one file's two versions out of a checkpoint's trees.
//
// The path is checked against the checkpoint's own list of changed files rather
// than passed through to git: a caller must not be able to read an arbitrary
// path out of a project by naming it here.
func (s *Service) CheckpointFileDiff(ctx context.Context, checkpointID, path string) (FileDiff, error) {
	record, err := s.store.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return FileDiff{}, err
	}
	if err := s.authorizeTarget(ctx, record.SessionID); err != nil {
		return FileDiff{}, err
	}
	var changed *ChangedFile
	for i := range record.Changed {
		if record.Changed[i].Path == path {
			changed = &record.Changed[i]
			break
		}
	}
	if changed == nil {
		return FileDiff{}, fmt.Errorf("%w: %s is not in checkpoint %s", ErrCheckpointNotFound, path, checkpointID)
	}

	engine, ok := s.currentCheckpoints().(interface {
		Repo(context.Context, string) (CheckpointRepo, error)
	})
	if !ok {
		return FileDiff{}, ErrUnavailable
	}
	session, err := s.store.GetSession(ctx, record.SessionID)
	if err != nil {
		return FileDiff{}, err
	}
	repo, err := engine.Repo(ctx, session.ProjectPath)
	if err != nil {
		return FileDiff{}, err
	}

	out := FileDiff{Path: path, Binary: changed.Binary}
	if out.Binary {
		// Bytes a diff view cannot render, and a file that could be hundreds of
		// megabytes. The UI says "binary file changed" instead.
		out.BeforeMissing, out.AfterMissing = true, true
		return out, nil
	}
	if record.BeforeTree != "" && changed.Kind != string(changeAdded) {
		body, tooLarge, err := repo.FileContent(ctx, record.BeforeTree, path, maxFileDiffBytes)
		switch {
		case err != nil:
			out.BeforeMissing = true
		case tooLarge:
			out.Truncated = true
		default:
			out.Before = string(body)
		}
	} else {
		out.BeforeMissing = true
	}
	if record.AfterTree != "" && changed.Kind != string(changeDeleted) {
		body, tooLarge, err := repo.FileContent(ctx, record.AfterTree, path, maxFileDiffBytes)
		switch {
		case err != nil:
			out.AfterMissing = true
		case tooLarge:
			out.Truncated = true
		default:
			out.After = string(body)
		}
	} else {
		out.AfterMissing = true
	}
	// One side over the ceiling makes the whole pair unusable: diffing a file
	// against nothing would draw every line of it as added.
	if out.Truncated {
		out.Before, out.After = "", ""
	}
	return out, nil
}

// changeAdded / changeDeleted mirror internal/checkpoint's ChangeKind values.
// Spelled out rather than imported so this file, which is about what the UI
// reads, does not pull the engine's vocabulary into every reader.
const (
	changeAdded   = "added"
	changeDeleted = "deleted"
)

// CheckpointRepo is the slice of the engine's repository a read needs.
type CheckpointRepo interface {
	FileContent(ctx context.Context, tree, path string, maxBytes int64) ([]byte, bool, error)
}

// Repo opens the shadow repository for a project. On EngineCheckpoints so the
// service can reach trees without importing the engine's types at every turn.
func (c *EngineCheckpoints) Repo(ctx context.Context, projectPath string) (CheckpointRepo, error) {
	if !c.Enabled() {
		return nil, ErrUnavailable
	}
	repo, err := c.engine.Open(ctx, projectPath)
	if err != nil {
		return nil, err
	}
	return repo, nil
}

// CheckpointForPrompt returns the turn checkpoint of one prompt, if there is
// one. The UI keys a turn header on the prompt's message id, which is the only
// name the live stream and a reloaded page agree on.
func CheckpointForPrompt(records []CheckpointRecord, promptMessageID string) (CheckpointRecord, bool) {
	if strings.TrimSpace(promptMessageID) == "" {
		return CheckpointRecord{}, false
	}
	for _, record := range records {
		if record.Kind == CheckpointTurn && record.PromptMessageID == promptMessageID {
			return record, true
		}
	}
	return CheckpointRecord{}, false
}
