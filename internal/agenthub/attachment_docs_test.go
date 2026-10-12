package agenthub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures in testdata/docs were written by python-docx / openpyxl and
// converted to .doc, .xls and .pdf by LibreOffice, so these read files made by
// real producers, not by this package's own writer.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "docs", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestExtractDocumentFormats(t *testing.T) {
	cases := []struct {
		file, kind string
		want       []string
	}{
		{"sample.docx", "docx", []string{"Thông tư 64", "Quy định về xác thực khách hàng.", "Mã\tNội dung", "A1\tGiá trị một"}},
		{"sample.pdf", "pdf", []string{"Quy định về xác thực khách hàng.", "Mã\tNội dung"}},
		{"sample.xlsx", "xlsx", []string{"## Sheet: Báo cáo", "Tên\tSố lượng", "Táo\t3", "Cam\t\tghi chú", "## Sheet: Phụ", "\tô B2"}},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			text, err := extractDocument(c.kind, fixture(t, c.file))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range c.want {
				if !strings.Contains(text, want) {
					t.Errorf("%s: missing %q in:\n%s", c.file, want, text)
				}
			}
		})
	}
}

func TestDocumentKind(t *testing.T) {
	for ext, want := range map[string]string{
		".PDF": "pdf", ".docx": "docx", ".doc": "", ".xlsx": "xlsx", ".XLS": "",
		".md": "", ".html": "", ".txt": "", "": "",
	} {
		if got := documentKind(ext); got != want {
			t.Errorf("documentKind(%q) = %q, want %q", ext, got, want)
		}
	}
}

func TestExtractDocumentRefusesGarbageWithoutPanicking(t *testing.T) {
	for _, kind := range []string{"pdf", "docx", "xlsx"} {
		if _, err := extractDocument(kind, []byte("this is not that kind of file")); err == nil {
			t.Errorf("%s: garbage was accepted", kind)
		}
		if _, err := extractDocument(kind, nil); err == nil {
			t.Errorf("%s: an empty file was accepted", kind)
		}
	}
}

func TestExtractDocumentCutsAndSaysSo(t *testing.T) {
	long := strings.Repeat("Xin chào thế giới. ", maxExtractedBytes/10)
	got := capText(long)
	if len(got) >= len(long) || !strings.Contains(got, "DevKit cut this attachment here") {
		t.Fatalf("not cut: %d of %d bytes", len(got), len(long))
	}
	if !strings.HasPrefix(got, "Xin chào") {
		t.Fatal("cut the wrong end")
	}
}

func TestAttachmentBlockReadsOfficeAndMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"sample.docx", "sample.pdf", "sample.xlsx"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, fixture(t, name), 0o600); err != nil {
			t.Fatal(err)
		}
		block, err := attachmentBlock(path, dir, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if block.Type != "resource" || block.Resource == nil || strings.TrimSpace(block.Resource.Text) == "" {
			t.Fatalf("%s: block = %+v", name, block)
		}
		if block.Resource.MimeType != "text/plain" || !strings.HasPrefix(block.Resource.URI, "file://") {
			t.Fatalf("%s: resource = %+v", name, block.Resource)
		}
	}
	for name, body := range map[string]string{
		"notes.md":  "# Tiêu đề\n\n- một\n- hai\n",
		"page.html": "<html><body><h1>Xin chào</h1></body></html>",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		block, err := attachmentBlock(path, dir, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if block.Resource == nil || block.Resource.Text != body {
			t.Fatalf("%s: sent %+v", name, block.Resource)
		}
	}
}

func TestAttachmentBlockNamesTheFileItRefuses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 nothing else"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := attachmentBlock(path, dir, false)
	if err == nil || !strings.Contains(err.Error(), "broken.pdf") {
		t.Fatalf("err = %v", err)
	}
	bin := filepath.Join(dir, "tool.exe")
	if err := os.WriteFile(bin, []byte{'M', 'Z', 0, 1, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := attachmentBlock(bin, dir, false); err == nil || !strings.Contains(err.Error(), "PDF") {
		t.Fatalf("a refusal should say what is accepted, got %v", err)
	}
}

func TestAttachmentBlockSaysHowToReplaceAnOldOfficeFile(t *testing.T) {
	dir := t.TempDir()
	for name, want := range map[string]string{"old.doc": ".docx", "old.XLS": ".xlsx"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, fixture(t, "sample.docx"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := attachmentBlock(path, dir, false)
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
