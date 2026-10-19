package agenthub

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"devkit/internal/pdfdoc"
)

// AttachmentInfo is what the composer's chip says about a file before it is
// sent: how big it is, what DevKit will do with it, and — when it cannot go —
// why. Kind is "text", "image", "pdf", "word", "excel" or "other".
type AttachmentInfo struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Kind    string `json:"kind"`
	Problem string `json:"problem,omitempty"`
}

// textSniffBytes is how much of a file is read to decide it is text. The
// refusal in attachmentBlock reads the whole file, but a binary shows a NUL
// long before the end, and the chip must not read a 8 MiB file to draw itself.
const textSniffBytes = 8 << 10

// DescribeAttachment reports on one file the way attachmentBlock and
// importDocuments will judge it, without reading it past a sniff of its first
// bytes. A file DevKit would refuse has a Problem; TestDescribeAgreesWithTheRefusals
// keeps the two from drifting apart. Nothing here refuses: the chip warns, and
// the turn's own refusal still has the last word.
func DescribeAttachment(path string) AttachmentInfo {
	info := AttachmentInfo{Path: path, Name: filepath.Base(path), Kind: "other"}
	stat, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			info.Problem = "This file no longer exists."
		} else {
			info.Problem = "This file cannot be read."
		}
		return info
	}
	if stat.IsDir() {
		info.Problem = "A folder cannot be attached."
		return info
	}
	info.Size = stat.Size()
	ext := filepath.Ext(path)

	// A PDF is a document of the session, not prompt text (importDocuments), and
	// has its own, larger limit.
	if strings.EqualFold(ext, ".pdf") {
		info.Kind = "pdf"
		if info.Size > pdfdoc.MaxFileBytes {
			info.Problem = fmt.Sprintf("Over the %d MiB limit for a PDF.", pdfdoc.MaxFileBytes>>20)
		}
		return info
	}
	if app := legacyOffice(ext); app != "" {
		info.Kind = strings.ToLower(app)
		info.Problem = fmt.Sprintf("An old %s format DevKit cannot read; save it as %s and attach that.",
			app, map[string]string{"Word": ".docx", "Excel": ".xlsx"}[app])
		return info
	}
	switch documentKind(ext) {
	case "docx":
		info.Kind = "word"
	case "xlsx":
		info.Kind = "excel"
	}
	mimeType := mime.TypeByExtension(ext)
	isImage := strings.HasPrefix(mimeType, "image/")
	if isImage {
		info.Kind = "image"
	}
	if info.Size > maxAttachmentBytes {
		info.Problem = fmt.Sprintf("Over the %d MiB limit for an attachment.", maxAttachmentBytes>>20)
		return info
	}
	if isImage && base64.StdEncoding.EncodedLen(int(info.Size)) > maxImageBase64Bytes {
		info.Problem = fmt.Sprintf("Over the %d MiB limit DevKit sends for an image.", maxImageBase64Bytes>>20)
		return info
	}
	if info.Kind == "word" || info.Kind == "excel" || isImage {
		return info
	}

	// Anything else rides as text, or is refused as noise.
	head, err := sniff(path)
	if err != nil {
		info.Problem = "This file cannot be read."
		return info
	}
	if !utf8Text(head) {
		info.Problem = "Not text or an image: DevKit reads PDF, Word (.docx), Excel (.xlsx), text and images."
		return info
	}
	info.Kind = "text"
	return info
}

func sniff(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, textSniffBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return buf[:n], nil
}

// DescribeAttachments is DescribeAttachment for each path, in order.
func DescribeAttachments(paths []string) []AttachmentInfo {
	out := make([]AttachmentInfo, 0, len(paths))
	for _, path := range paths {
		out = append(out, DescribeAttachment(path))
	}
	return out
}
