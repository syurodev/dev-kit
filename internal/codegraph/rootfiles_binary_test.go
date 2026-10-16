package codegraph

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadBinaryAtStaysInsideTheRootAndHonoursTheLimit(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.pdf")
	if err := os.WriteFile(outside, []byte("%PDF-1.4 secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.pdf"), []byte("%PDF-1.4\x00\xff\xfe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.pdf")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}

	got, err := ReadBinaryAt(root, "a.pdf", 64)
	if err != nil || string(got.Data) != "%PDF-1.4\x00\xff\xfe" || got.Signature.Size != int64(len(got.Data)) {
		t.Fatalf("read = %+v, %v", got, err)
	}
	sig, err := SignatureAt(root, "a.pdf")
	if err != nil || sig != got.Signature {
		t.Fatalf("signature = %+v, %v; read said %+v", sig, err, got.Signature)
	}
	if _, err := ReadBinaryAt(root, "a.pdf", 4); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("over the limit: %v", err)
	}
	for _, rel := range []string{"../secret.pdf", "link.pdf", outside} {
		if _, err := ReadBinaryAt(root, rel, 64); !errors.Is(err, ErrPathOutsideProject) {
			t.Fatalf("%q escaped the folder: %v", rel, err)
		}
	}
	if _, err := ReadBinaryAt(root, ".", 64); err == nil {
		t.Fatal("a directory was read")
	}
}
