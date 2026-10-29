package agenthub

import "testing"

func TestFolderCovered(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		roots []string
		want  bool
	}{
		{"the root itself", "/a/foo", []string{"/a/foo"}, true},
		{"inside the root", "/a/foo/x", []string{"/a/foo"}, true},
		{"a sibling that starts the same", "/a/foobar", []string{"/a/foo"}, false},
		{"a parent of the root", "/a", []string{"/a/foo"}, false},
		{"trailing slash on the path", "/a/foo/", []string{"/a/foo"}, true},
		{"trailing slash on the root", "/a/foo/x", []string{"/a/foo/"}, true},
		{"a root with a trailing slash does not cover a sibling", "/a/foobar", []string{"/a/foo/"}, false},
		{"an empty root is ignored", "/a/foo", []string{"", "/b"}, false},
		{"an empty root never covers everything", "/", []string{""}, false},
		{"any of several roots", "/b/y", []string{"/a/foo", "/b"}, true},
		{"no roots", "/a/foo", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := folderCovered(c.path, c.roots); got != c.want {
				t.Fatalf("folderCovered(%q, %q) = %v, want %v", c.path, c.roots, got, c.want)
			}
		})
	}
}
