package agenthub

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// PermissionFileDiff is what a pending permission would do to one file: the
// file as it is on disk now, and as it would be once the edit is allowed.
//
// The request itself says less than that. ACP carries the edit as a diff
// content item, {path, oldText, newText}, and an agent fills it with the
// tool's own arguments: Claude's Edit sends only the snippet it replaces and
// its replacement, and Write sends the new file with oldText null — so an
// overwrite of an existing file reads as a new one. Reading the file lets the
// card show the edit where it lands, with the lines around it, and tell a new
// file from an overwritten one.
type PermissionFileDiff struct {
	FileDiff
	// Stale says the snippet the agent means to replace is not in the file —
	// it changed since the agent read it. Before and After are then the
	// snippet and its replacement, which is still what the agent asked for.
	Stale bool `json:"stale"`
}

type permissionDiffItem struct {
	Type    string  `json:"type"`
	Path    string  `json:"path"`
	OldText *string `json:"oldText"`
	NewText string  `json:"newText"`
}

type permissionDiffParams struct {
	ToolCall struct {
		Content  []permissionDiffItem `json:"content"`
		RawInput struct {
			ReplaceAll bool `json:"replace_all"`
		} `json:"rawInput"`
	} `json:"toolCall"`
}

// PermissionFileDiffs previews every file edit a request_permission carries.
// Nothing for a request that edits no file (a shell command, a fetch). Relative
// paths resolve against cwd, the session's folder. readFile is os.ReadFile
// outside tests.
func PermissionFileDiffs(params json.RawMessage, cwd string, readFile func(string) ([]byte, error)) []PermissionFileDiff {
	var parsed permissionDiffParams
	if err := json.Unmarshal(params, &parsed); err != nil {
		return nil
	}
	var out []PermissionFileDiff
	for _, item := range parsed.ToolCall.Content {
		if item.Type != "diff" || item.Path == "" {
			continue
		}
		path := item.Path
		if !filepath.IsAbs(path) && cwd != "" {
			path = filepath.Join(cwd, path)
		}
		out = append(out, previewEdit(item, path, parsed.ToolCall.RawInput.ReplaceAll, readFile))
	}
	return out
}

func previewEdit(item permissionDiffItem, path string, replaceAll bool, readFile func(string) ([]byte, error)) PermissionFileDiff {
	diff := PermissionFileDiff{FileDiff: FileDiff{Path: item.Path}}

	current, err := readFile(path)
	missing := errors.Is(err, fs.ErrNotExist)
	unreadable := err != nil && !missing

	switch {
	case len(current) > maxFileDiffBytes || len(item.NewText) > maxFileDiffBytes:
		diff.Truncated = true
		return diff
	case !missing && !unreadable && isBinary(current):
		diff.Binary = true
		return diff
	}

	// Write: the whole new file. Before is whatever is there now, if anything.
	if item.OldText == nil {
		diff.After = item.NewText
		if missing || unreadable {
			diff.BeforeMissing = true
		} else {
			diff.Before = string(current)
		}
		return diff
	}

	// Edit: the snippet and its replacement, applied to the file as it stands.
	old := *item.OldText
	text := string(current)
	if missing || unreadable || old == "" || !strings.Contains(text, old) {
		diff.Before = old
		diff.After = item.NewText
		diff.Stale = !(missing && old == "")
		diff.BeforeMissing = missing && old == ""
		return diff
	}
	diff.Before = text
	if replaceAll {
		diff.After = strings.ReplaceAll(text, old, item.NewText)
	} else {
		diff.After = strings.Replace(text, old, item.NewText, 1)
	}
	return diff
}

// isBinary is the same test git makes: a NUL in the first 8000 bytes. Invalid
// UTF-8 counts too — it would reach the renderer as replacement characters.
func isBinary(data []byte) bool {
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(data)
}
