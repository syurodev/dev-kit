package agentruntime

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadNodeChecksIntegrityAndCancellation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("archive")) }))
	defer s.Close()
	m := New(t.TempDir())
	sum := sha256.Sum256([]byte("archive"))
	if err := m.downloadNode(context.Background(), s.URL, filepath.Join(t.TempDir(), "ok"), hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if err := m.downloadNode(context.Background(), s.URL, filepath.Join(t.TempDir(), "bad"), "wrong"); err == nil {
		t.Fatal("accepted checksum mismatch")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.downloadNode(ctx, s.URL, filepath.Join(t.TempDir(), "cancel"), "wrong"); err == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestExtractNodeRejectsTraversalAndSkipsLinks(t *testing.T) {
	for _, name := range []string{"node/bin/node", "node/../outside", "wrong/bin/node", "node/dir/../../outside"} {
		t.Run(name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "node.tgz")
			f, _ := os.Create(archive)
			gz := gzip.NewWriter(f)
			tw := tar.NewWriter(gz)
			tw.WriteHeader(&tar.Header{Name: "node/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside", Mode: 0755})
			tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: 2})
			tw.Write([]byte("ok"))
			tw.Close()
			gz.Close()
			f.Close()
			dest := t.TempDir()
			err := extractNode(context.Background(), archive, dest, "node", ".tar.gz")
			if name == "node/bin/node" {
				if err != nil {
					t.Fatal(err)
				}
				if !isExecutable(filepath.Join(dest, "bin", "node")) {
					t.Fatal("lost executable")
				}
			} else if err == nil {
				t.Fatal("accepted unsafe path")
			}
			if _, err := os.Lstat(filepath.Join(dest, "link")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("extracted symlink")
			}
		})
	}
}
