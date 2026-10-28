package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrNoSuchSide is returned when a side is asked for that is neither "ours"
// nor "theirs".
var ErrNoSuchSide = errors.New("git: side must be ours or theirs")

// Conflict is one unmerged path, in the three versions the resolve panel
// shows. It is deliberately the shape Sync's ResolvePanel already renders —
// Local / Merged (editable) / Remote — because a second three-pane merge UI is
// a second three-pane merge UI.
type Conflict struct {
	Path string `json:"path"`
	// Ours, Theirs and Base are the three stages git keeps in the index.
	Ours   string `json:"ours"`
	Theirs string `json:"theirs"`
	Base   string `json:"base"`
	// HasOurs/HasTheirs/HasBase say whether that stage EXISTS, which an empty
	// string cannot: a side that deleted the file and a side that emptied it
	// are different conflicts and are resolved differently. A missing base is
	// the ordinary add/add case and means only that there is nothing to
	// compare against.
	HasOurs   bool `json:"hasOurs"`
	HasTheirs bool `json:"hasTheirs"`
	HasBase   bool `json:"hasBase"`
	// Merged is git's OWN merge of the two sides, as it sits in the work tree:
	// everything that merged cleanly, plus <<<<<<< markers around what did
	// not. It is the seed the panel edits, because seeding from Ours would
	// silently throw away every clean change the other side made.
	Merged string `json:"merged"`
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

// Conflict reads one unmerged path's three stages plus git's own merge of them.
func (s *Service) Conflict(ctx context.Context, path string) (Conflict, error) {
	r, err := s.active()
	if err != nil {
		return Conflict{}, err
	}
	out := Conflict{Path: path}
	// Stage 1 is the merge base, 2 ours, 3 theirs. A stage that does not exist
	// is a side that deleted or never had the file, which is information, not
	// an error — so the failure to read one is recorded rather than returned.
	stages := []struct {
		arg     string
		dest    *string
		present *bool
	}{
		{":1:", &out.Base, &out.HasBase},
		{":2:", &out.Ours, &out.HasOurs},
		{":3:", &out.Theirs, &out.HasTheirs},
	}
	for _, stage := range stages {
		blob, err := r.run(ctx, nil, "show", stage.arg+path)
		if err != nil {
			continue
		}
		*stage.dest = string(blob)
		*stage.present = true
	}

	merged, _, err := r.readWork(path)
	if err != nil {
		return Conflict{}, err
	}
	out.Merged = merged

	if isBinary(out.Ours) || isBinary(out.Theirs) || isBinary(out.Merged) {
		out.Binary = true
		// Bytes that are not text have no business crossing into a textarea
		// and coming back changed. A binary conflict is resolved by picking a
		// side, which ResolveSide does without reading the file at all.
		out.Ours, out.Theirs, out.Base, out.Merged = "", "", "", ""
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
	if err := r.writeWork(path, merged); err != nil {
		return err
	}
	_, err = r.run(ctx, nil, "add", "--", path)
	return err
}

// ResolveSide takes one whole side of a conflict, "ours" or "theirs".
//
// It is git that copies the bytes, not this process: `git checkout --ours`
// writes the stage exactly as it is recorded, where sending the text out to a
// panel and back would re-encode it, normalise its line endings and be unable
// to express a binary file at all.
//
// A side that has no stage is a side that DELETED the file, and taking it
// means removing the file rather than writing an empty one.
func (s *Service) ResolveSide(ctx context.Context, path, side string) error {
	if side != "ours" && side != "theirs" {
		return fmt.Errorf("%w: %q", ErrNoSuchSide, side)
	}
	r, err := s.active()
	if err != nil {
		return err
	}
	// Ask the index which stages exist rather than trusting the caller: the
	// panel's copy of the conflict may be older than the work tree.
	conflict, err := s.Conflict(ctx, path)
	if err != nil {
		return err
	}
	if (side == "ours" && !conflict.HasOurs) || (side == "theirs" && !conflict.HasTheirs) {
		_, err = r.run(ctx, nil, "rm", "-f", "--", path)
		return err
	}
	if _, err := r.run(ctx, nil, "checkout", "--"+side, "--", path); err != nil {
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
