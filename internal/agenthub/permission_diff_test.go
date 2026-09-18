package agenthub

import (
	"encoding/json"
	"io/fs"
	"testing"
)

func files(m map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if s, ok := m[path]; ok {
			return []byte(s), nil
		}
		return nil, fs.ErrNotExist
	}
}

func params(t *testing.T, content []map[string]any, rawInput map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"toolCall": map[string]any{"content": content, "rawInput": rawInput}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPermissionEditLandsInTheWholeFile(t *testing.T) {
	disk := files(map[string]string{"/p/a.go": "one\ntwo\nthree\n"})
	got := PermissionFileDiffs(params(t, []map[string]any{{"type": "diff", "path": "/p/a.go", "oldText": "two", "newText": "TWO"}}, nil), "/p", disk)
	if len(got) != 1 {
		t.Fatalf("got %d diffs", len(got))
	}
	if got[0].Before != "one\ntwo\nthree\n" || got[0].After != "one\nTWO\nthree\n" || got[0].Stale {
		t.Fatalf("diff = %+v", got[0])
	}
}

func TestPermissionEditReplaceAll(t *testing.T) {
	disk := files(map[string]string{"/p/a": "x x x"})
	got := PermissionFileDiffs(params(t, []map[string]any{{"type": "diff", "path": "a", "oldText": "x", "newText": "y"}}, map[string]any{"replace_all": true}), "/p", disk)
	if got[0].After != "y y y" {
		t.Fatalf("after = %q", got[0].After)
	}
}

func TestPermissionEditOfChangedFileIsStale(t *testing.T) {
	disk := files(map[string]string{"/p/a": "moved on"})
	got := PermissionFileDiffs(params(t, []map[string]any{{"type": "diff", "path": "/p/a", "oldText": "gone", "newText": "new"}}, nil), "/p", disk)
	if !got[0].Stale || got[0].Before != "gone" || got[0].After != "new" {
		t.Fatalf("diff = %+v", got[0])
	}
}

func TestPermissionWriteTellsNewFromOverwrite(t *testing.T) {
	disk := files(map[string]string{"/p/old": "before"})
	got := PermissionFileDiffs(params(t, []map[string]any{
		{"type": "diff", "path": "/p/old", "oldText": nil, "newText": "after"},
		{"type": "diff", "path": "/p/new", "oldText": nil, "newText": "fresh"},
	}, nil), "/p", disk)
	if got[0].BeforeMissing || got[0].Before != "before" {
		t.Fatalf("overwrite = %+v", got[0])
	}
	if !got[1].BeforeMissing || got[1].After != "fresh" {
		t.Fatalf("new file = %+v", got[1])
	}
}

func TestPermissionWithoutAFileEditPreviewsNothing(t *testing.T) {
	got := PermissionFileDiffs(params(t, []map[string]any{{"type": "content", "content": map[string]any{"type": "text", "text": "ls"}}}, nil), "/p", files(nil))
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestPermissionBinaryFileIsNotPreviewed(t *testing.T) {
	disk := files(map[string]string{"/p/b": "a\x00b"})
	got := PermissionFileDiffs(params(t, []map[string]any{{"type": "diff", "path": "/p/b", "oldText": nil, "newText": "x"}}, nil), "/p", disk)
	if !got[0].Binary || got[0].Before != "" {
		t.Fatalf("diff = %+v", got[0])
	}
}
