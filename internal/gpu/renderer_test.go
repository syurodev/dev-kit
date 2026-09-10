package gpu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasRenderNode(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		want    bool
	}{
		// A desktop with a GPU: the render node is what DMABUF needs.
		{name: "render node present", entries: []string{"card0", "renderD128"}, want: true},
		{name: "several GPUs", entries: []string{"card0", "renderD128", "card1", "renderD129"}, want: true},
		// A card node without a render node is not enough: card* is the
		// modesetting device, and it cannot export a buffer for import.
		{name: "card node only", entries: []string{"card0"}, want: false},
		{name: "empty directory", entries: nil, want: false},
		// Nothing that merely starts with "render" counts by accident.
		{name: "unrelated name", entries: []string{"renderer.conf"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, entry := range tc.entries {
				if err := os.WriteFile(filepath.Join(dir, entry), nil, 0o600); err != nil {
					t.Fatalf("seeding %q: %v", entry, err)
				}
			}
			if got := HasRenderNode(dir); got != tc.want {
				t.Errorf("HasRenderNode(%v) = %v, want %v", tc.entries, got, tc.want)
			}
		})
	}
}

// A path that does not exist must read as "no GPU", not panic and not true:
// this is the container case the whole check exists for.
func TestHasRenderNodeMissingDir(t *testing.T) {
	if HasRenderNode(filepath.Join(t.TempDir(), "absent")) {
		t.Error("HasRenderNode on a missing directory = true, want false")
	}
}
