package agenthub

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name string, body []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDescribeAttachmentSaysWhatTheFileIs(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body []byte
		kind string
	}{
		{"notes.md", []byte("# hi\n"), "text"},
		{"main.go", []byte("package main\n"), "text"},
		{"shot.png", []byte{0x89, 'P', 'N', 'G'}, "image"},
		{"spec.pdf", []byte("%PDF-1.4"), "pdf"},
		{"a.docx", []byte("PK"), "word"},
		{"b.xlsx", []byte("PK"), "excel"},
	}
	for _, tc := range cases {
		info := DescribeAttachment(writeFile(t, dir, tc.name, tc.body))
		if info.Kind != tc.kind || info.Problem != "" || info.Name != tc.name || info.Size != int64(len(tc.body)) {
			t.Errorf("%s: %+v, want kind %q, no problem", tc.name, info, tc.kind)
		}
	}
}

func TestDescribeAttachmentNamesWhyAFileCannotGo(t *testing.T) {
	dir := t.TempDir()
	big := writeFile(t, dir, "big.txt", bytes.Repeat([]byte("a"), maxAttachmentBytes+1))
	cases := map[string]string{
		big: "limit",
		writeFile(t, dir, "old.doc", []byte("x")):   "old Word",
		writeFile(t, dir, "old.xls", []byte("x")):   "old Excel",
		writeFile(t, dir, "blob.bin", []byte{0, 1}): "Not text",
		dir:                            "folder",
		filepath.Join(dir, "gone.txt"): "no longer exists",
	}
	for path, want := range cases {
		if got := DescribeAttachment(path).Problem; !strings.Contains(got, want) {
			t.Errorf("%s: problem %q, want it to mention %q", filepath.Base(path), got, want)
		}
	}
}

// The chip and the refusal read the same rules; a file one calls fine and the
// other refuses would be a chip that lies.
func TestDescribeAgreesWithTheRefusals(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		writeFile(t, dir, "ok.txt", []byte("fine")),
		writeFile(t, dir, "ok.png", []byte{0x89, 'P'}),
		writeFile(t, dir, "big.txt", bytes.Repeat([]byte("a"), maxAttachmentBytes+1)),
		writeFile(t, dir, "old.doc", []byte("x")),
		writeFile(t, dir, "old.xls", []byte("x")),
		writeFile(t, dir, "blob.bin", []byte{0, 1, 2}),
		dir,
		filepath.Join(dir, "missing.txt"),
	}
	for _, path := range paths {
		_, refused := attachmentBlock(path, "", false)
		if described := DescribeAttachment(path).Problem != ""; described != (refused != nil) {
			t.Errorf("%s: describe says problem=%v, attachmentBlock error=%v", filepath.Base(path), described, refused)
		}
	}
}

func TestDescribeAttachmentsKeepsOrder(t *testing.T) {
	dir := t.TempDir()
	a, b := writeFile(t, dir, "a.txt", []byte("a")), writeFile(t, dir, "b.txt", []byte("b"))
	got := DescribeAttachments([]string{b, a})
	if len(got) != 2 || got[0].Name != "b.txt" || got[1].Name != "a.txt" {
		t.Fatalf("got %+v", got)
	}
}
