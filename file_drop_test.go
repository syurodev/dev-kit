package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestDroppedFilesPayload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := &application.DropTargetDetails{Attributes: map[string]string{composerDropAttr: "c1"}}

	got, ok := droppedFilesPayload([]string{file, dir, "relative.txt", filepath.Join(dir, "gone")}, target)
	if !ok {
		t.Fatal("drop on a composer was ignored")
	}
	if got["composer"] != "c1" || got["skipped"] != 3 {
		t.Fatalf("payload = %v", got)
	}
	if paths := got["paths"].([]string); len(paths) != 1 || paths[0] != file {
		t.Fatalf("paths = %v", paths)
	}

	if _, ok := droppedFilesPayload([]string{file}, &application.DropTargetDetails{}); ok {
		t.Fatal("a drop that names no composer must be ignored")
	}
	if _, ok := droppedFilesPayload([]string{file}, nil); ok {
		t.Fatal("a drop with no target must be ignored")
	}
}
