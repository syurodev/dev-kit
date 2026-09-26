package workspace

import (
	"testing"
)

func TestSessionOwns(t *testing.T) {
	tests := []struct {
		name     string
		services []WorkspaceService
		cwd      string
		granted  []string
		want     bool
	}{
		{
			name: "cwd inside a service",
			services: []WorkspaceService{
				{Path: "/work/api", Name: "api"},
			},
			cwd:     "/work/api/src",
			granted: []string{},
			want:    true,
		},
		{
			name: "granted path covers a service",
			services: []WorkspaceService{
				{Path: "/work/api", Name: "api"},
			},
			cwd:     "/other/path",
			granted: []string{"/work/api"},
			want:    true,
		},
		{
			name: "granted path inside a service (partial grant, not owner)",
			services: []WorkspaceService{
				{Path: "/work/api", Name: "api"},
			},
			cwd:     "/other/path",
			granted: []string{"/work/api/src"},
			want:    false,
		},
		{
			name: "unrelated paths",
			services: []WorkspaceService{
				{Path: "/work/api", Name: "api"},
			},
			cwd:     "/other/project",
			granted: []string{"/home/user/code"},
			want:    false,
		},
		{
			name: "sibling paths with shared prefix",
			services: []WorkspaceService{
				{Path: "/work/api", Name: "api"},
			},
			cwd:     "/work/api2/src",
			granted: []string{},
			want:    false,
		},
		{
			name: "multiple services, one matches by granted",
			services: []WorkspaceService{
				{Path: "/work/api", Name: "api"},
				{Path: "/work/web", Name: "web"},
				{Path: "/work/db", Name: "db"},
			},
			cwd:     "/other/path",
			granted: []string{"/work/web"},
			want:    true,
		},
		{
			name: "multiple services, one matches by cwd",
			services: []WorkspaceService{
				{Path: "/work/api", Name: "api"},
				{Path: "/work/web", Name: "web"},
				{Path: "/work/db", Name: "db"},
			},
			cwd:     "/work/db/migrations",
			granted: []string{},
			want:    true,
		},
		{
			name:     "empty services",
			services: []WorkspaceService{},
			cwd:      "/work/api",
			granted:  []string{},
			want:     false,
		},
		{
			name: "empty cwd and granted",
			services: []WorkspaceService{
				{Path: "/work/api", Name: "api"},
			},
			cwd:     "",
			granted: []string{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SessionOwns(tt.services, tt.cwd, tt.granted)
			if got != tt.want {
				t.Errorf("SessionOwns(...) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCovered(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		roots []string
		want  bool
	}{
		{
			name:  "path equals root",
			path:  "/work/api",
			roots: []string{"/work/api"},
			want:  true,
		},
		{
			name:  "path inside root",
			path:  "/work/api/src/main",
			roots: []string{"/work/api"},
			want:  true,
		},
		{
			name:  "path not covered",
			path:  "/work/web",
			roots: []string{"/work/api"},
			want:  false,
		},
		{
			name:  "prefix match is not cover",
			path:  "/work/api2",
			roots: []string{"/work/api"},
			want:  false,
		},
		{
			name:  "multiple roots, first matches",
			path:  "/work/api/src",
			roots: []string{"/work/api", "/work/web"},
			want:  true,
		},
		{
			name:  "multiple roots, second matches",
			path:  "/work/web/src",
			roots: []string{"/work/api", "/work/web"},
			want:  true,
		},
		{
			name:  "empty path",
			path:  "",
			roots: []string{"/work/api"},
			want:  false,
		},
		{
			name:  "empty roots",
			path:  "/work/api",
			roots: []string{},
			want:  false,
		},
		{
			name:  "empty string in roots is ignored",
			path:  "/work/api",
			roots: []string{"", "/work/api"},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Covered(tt.path, tt.roots)
			if got != tt.want {
				t.Errorf("Covered(%q, %v) = %v, want %v", tt.path, tt.roots, got, tt.want)
			}
		})
	}
}

func TestWithin(t *testing.T) {
	tests := []struct {
		name string
		path string
		root string
		want bool
	}{
		{
			name: "path equals root",
			path: "/work/api",
			root: "/work/api",
			want: true,
		},
		{
			name: "path inside root",
			path: "/work/api/src",
			root: "/work/api",
			want: true,
		},
		{
			name: "deep nesting",
			path: "/work/api/src/main/java",
			root: "/work/api",
			want: true,
		},
		{
			name: "path not inside root",
			path: "/work/web",
			root: "/work/api",
			want: false,
		},
		{
			name: "prefix match is not within",
			path: "/work/api2",
			root: "/work/api",
			want: false,
		},
		{
			name: "empty path",
			path: "",
			root: "/work/api",
			want: false,
		},
		{
			name: "empty root",
			path: "/work/api",
			root: "",
			want: false,
		},
		{
			name: "both empty",
			path: "",
			root: "",
			want: false,
		},
		{
			name: "trailing slashes normalized",
			path: "/work/api/src/",
			root: "/work/api/",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Within(tt.path, tt.root)
			if got != tt.want {
				t.Errorf("Within(%q, %q) = %v, want %v", tt.path, tt.root, got, tt.want)
			}
		})
	}
}
