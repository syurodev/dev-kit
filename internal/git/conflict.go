package git

import (
	"context"
	"strings"
)

// Conflict is one unmerged path, in the three versions the resolve panel
// shows. It is deliberately the shape Sync's ResolvePanel already renders —
// Local / Merged (editable) / Remote — because a second three-pane merge UI is
// a second three-pane merge UI.
type Conflict struct {
	Path string `json:"path"`
	// Ours, Theirs and Base are the three stages git keeps in the index.
	// Missing on one side (added by one, deleted by the other) means empty.
	Ours   string `json:"ours"`
	Theirs string `json:"theirs"`
	Base   string `json:"base"`
	// Binary means there is nothing to merge by hand: only a side to pick.
	Binary bool `json:"binary"`
}

// Conflicts lists unmerged paths without reading their contents.
func (s *Service) Conflicts(ctx context.Context) ([]string, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	return status.Conflicted, nil
}

// Conflict reads one unmerged path's three stages.
func (s *Service) Conflict(ctx context.Context, path string) (Conflict, error) {
	r, err := s.active()
	if err != nil {
		return Conflict{}, err
	}
	out := Conflict{Path: path}
	// Stage 1 is the merge base, 2 ours, 3 theirs. A stage that does not exist
	// is a side that deleted or never had the file, which is information, not
	// an error.
	for stage, dest := range map[string]*string{":1:": &out.Base, ":2:": &out.Ours, ":3:": &out.Theirs} {
		blob, err := r.run(ctx, nil, "show", stage+path)
		if err != nil {
			continue
		}
		*dest = string(blob)
	}
	if isBinary(out.Ours) || isBinary(out.Theirs) {
		out.Binary = true
		out.Ours, out.Theirs, out.Base = "", "", ""
	}
	return out, nil
}

// isBinary uses git's own rule: a NUL byte in the first 8000 bytes.
func isBinary(content string) bool {
	if len(content) > 8000 {
		content = content[:8000]
	}
	return strings.IndexByte(content, 0) >= 0
}

// Resolve writes the merged content for one path and stages it, which is what
// marks it resolved.
func (s *Service) Resolve(ctx context.Context, path, merged string) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	if err := writeWorkTreeFile(r.work, path, merged); err != nil {
		return err
	}
	_, err = r.run(ctx, nil, "add", "--", path)
	return err
}

// AbortMerge throws the whole merge away. It is deliberately not exposed over
// MCP: discarding everyone's in-flight merge is a person's decision.
func (s *Service) AbortMerge(ctx context.Context) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	_, err = r.run(ctx, nil, "merge", "--abort")
	return err
}
