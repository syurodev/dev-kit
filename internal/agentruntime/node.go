package agentruntime

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Checksums are pinned from https://nodejs.org/dist/v24.14.1/SHASUMS256.txt.
// Updating Node is a reviewed runtime release, never a lookup of 'latest'.
const managedNodeVersion = "24.14.1"

var nodeChecksums = map[string]string{
	"darwin-arm64": "25495ff85bd89e2d8a24d88566d7e2f827c6b0d3d872b2cebf75371f93fcb1fe",
	"darwin-x64":   "2526230ad7d922be82d4fdb1e7ee1e84303e133e3b4b0ec4c2897ab31de0253d",
	"linux-arm64":  "734ff04fa7f8ed2e8a78d40cacf5ac3fc4515dac2858757cbab313eb483ba8a2",
	"linux-x64":    "ace9fa104992ed0829642629c46ca7bd7fd6e76278cb96c958c4b387d29658ea",
	"win-arm64":    "a7b7c68490e4a8cde1921fe5a0cfb3001d53f9c839e416903e4f28e727b62f60",
	"win-x64":      "6e50ce5498c0cebc20fd39ab3ff5df836ed2f8a31aa093cecad8497cff126d70",
}

func nodePlatform() string {
	osName, arch := runtime.GOOS, runtime.GOARCH
	if osName == "windows" {
		osName = "win"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	return osName + "-" + arch
}
func (m *Manager) nodeDir() string {
	return filepath.Join(m.root, "node", managedNodeVersion+"-"+nodePlatform())
}
func nodeAt(dir string) nodeInstall {
	node := filepath.Join(dir, "bin", "node")
	npm := filepath.Join(dir, "lib", "node_modules", "npm", "bin", "npm-cli.js")
	if runtime.GOOS == "windows" {
		node = filepath.Join(dir, "node.exe")
		npm = filepath.Join(dir, "node_modules", "npm", "bin", "npm-cli.js")
	}
	return nodeInstall{Node: node, NPM: npm, Version: managedNodeVersion}
}
func nodePresent(n nodeInstall) bool {
	st, err := os.Stat(n.NPM)
	return isExecutable(n.Node) && err == nil && st.Mode().IsRegular()
}

// installNode is used only by Install. Status and session creation never download.
func (m *Manager) installNode(ctx context.Context) (nodeInstall, []Requirement) {
	n, err := m.ensureNode(ctx)
	if err != nil {
		return nodeInstall{}, []Requirement{{Name: "managed runtime", Problem: err.Error()}}
	}
	return n, nil
}
func (m *Manager) ensureNode(ctx context.Context) (nodeInstall, error) {
	m.nodeMu.Lock()
	defer m.nodeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nodeInstall{}, err
	}
	n := nodeAt(m.nodeDir())
	if nodePresent(n) {
		return n, nil
	}
	checksum, ok := nodeChecksums[nodePlatform()]
	if !ok {
		return n, fmt.Errorf("managed Node is unavailable for %s", nodePlatform())
	}
	base := "node-v" + managedNodeVersion + "-" + nodePlatform()
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	parent := filepath.Dir(m.nodeDir())
	if err := os.MkdirAll(parent, 0700); err != nil {
		return n, err
	}
	staging, err := os.MkdirTemp(parent, ".staging-")
	if err != nil {
		return n, err
	}
	defer os.RemoveAll(staging)
	archive := filepath.Join(staging, "download")
	if err := m.downloadNode(ctx, "https://nodejs.org/dist/v"+managedNodeVersion+"/"+base+ext, archive, checksum); err != nil {
		return n, err
	}
	payload := filepath.Join(staging, "payload")
	if err := os.Mkdir(payload, 0700); err != nil {
		return n, err
	}
	if err := extractNode(ctx, archive, payload, base, ext); err != nil {
		return n, err
	}
	candidate := nodeAt(payload)
	if !nodePresent(candidate) {
		return n, fmt.Errorf("managed Node archive is missing node or npm")
	}
	_, version, err := probeNode(ctx, candidate.Node)
	if err != nil || version != managedNodeVersion {
		return n, fmt.Errorf("managed Node %s cannot run on this system", managedNodeVersion)
	}
	// An incomplete directory is never replaced: an existing runtime may refer
	// to it. Surface repair explicitly instead of deleting a live dependency.
	if err := os.Rename(payload, m.nodeDir()); err != nil {
		return n, fmt.Errorf("activate managed Node: %w", err)
	}
	return n, nil
}
func (m *Manager) downloadNode(ctx context.Context, url, dest, checksum string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := m.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download managed Node: %s", sanitizeReason(err.Error()))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download managed Node: HTTP %d", res.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	const limit = 150 << 20
	size, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, limit+1))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if size > limit {
		return fmt.Errorf("managed Node archive exceeds size limit")
	}
	if hex.EncodeToString(h.Sum(nil)) != checksum {
		return fmt.Errorf("managed Node checksum mismatch")
	}
	return nil
}

// Extract only ordinary files and directories from the known archive root.
// Node's convenience bin/npm and bin/npx symlinks are unnecessary: npm-cli.js
// is launched with the managed node explicitly, including on Windows.
func extractNode(ctx context.Context, archive, dest, root, ext string) error {
	var total int64
	write := func(name string, mode os.FileMode, size int64, r io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, ok := strings.CutPrefix(name, root+"/")
		if !ok {
			return fmt.Errorf("unexpected Node archive root")
		}
		if rel == "" {
			return nil
		}
		if strings.Contains(rel, "\\") || !filepath.IsLocal(filepath.FromSlash(rel)) {
			return fmt.Errorf("unsafe Node archive path")
		}
		p := filepath.Join(dest, filepath.FromSlash(rel))
		if mode.IsDir() {
			return os.MkdirAll(p, 0755)
		}
		if !mode.IsRegular() {
			return nil
		}
		total += size
		if size < 0 || total > 700<<20 {
			return fmt.Errorf("expanded Node archive exceeds size limit")
		}
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm()&0755)
		if err != nil {
			return err
		}
		_, err = io.CopyN(f, r, size)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	if ext == ".zip" {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer z.Close()
		for _, f := range z.File {
			r, err := f.Open()
			if err != nil {
				return err
			}
			err = write(f.Name, f.Mode(), int64(f.UncompressedSize64), r)
			r.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := write(h.Name, h.FileInfo().Mode(), h.Size, tr); err != nil {
			return err
		}
	}
}
