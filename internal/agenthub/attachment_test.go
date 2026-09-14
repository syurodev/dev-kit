package agenthub

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestAttachmentTravelsWithThePrompt(t *testing.T) {
	// Contents, not a path: a path is only useful to the agent if the file is
	// inside its jail, and attaching something from elsewhere on the machine is
	// exactly the case a path cannot serve.
	dir := t.TempDir()
	note := write(t, dir, "note.txt", "the fixture still expects the old EUR value")

	blocks, err := buildPrompt("look at this", []string{note}, dir, false)
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want the text and the attachment", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "look at this" {
		t.Fatalf("first block = %+v, want the user's text", blocks[0])
	}
	if blocks[1].Type != "resource" || blocks[1].Resource == nil {
		t.Fatalf("second block = %+v, want an embedded resource", blocks[1])
	}
	if !strings.Contains(blocks[1].Resource.Text, "old EUR value") {
		t.Fatalf("resource text = %q, want the file's contents", blocks[1].Resource.Text)
	}
	if !strings.HasPrefix(blocks[1].Resource.URI, "file://") {
		t.Fatalf("resource uri = %q, want a file:// uri", blocks[1].Resource.URI)
	}
}

func TestImageAttachmentUsesTheImageBlock(t *testing.T) {
	// The agent advertises promptCapabilities.image; a PNG sent as text would
	// arrive as mojibake.
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 16))
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}

	blocks, err := buildPrompt("", []string{path}, dir, false)
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Type != "image" {
		t.Fatalf("blocks = %+v, want a single image block", blocks)
	}
	if blocks[0].MimeType != "image/png" {
		t.Fatalf("mimeType = %q, want image/png", blocks[0].MimeType)
	}
	decoded, err := base64.StdEncoding.DecodeString(blocks[0].Data)
	if err != nil || string(decoded) != string(png) {
		t.Fatalf("data did not round-trip through base64")
	}
}

func TestPastedImageUsesTheImageBlockAndLimitsInput(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("png bytes"))
	blocks, err := buildPromptWithImages("see this", nil, []ImageAttachment{{
		Name: "paste.png", MimeType: "image/png", Data: data,
	}}, t.TempDir(), false)
	if err != nil {
		t.Fatalf("buildPromptWithImages: %v", err)
	}
	if len(blocks) != 2 || blocks[1].Type != "image" || blocks[1].Data != data {
		t.Fatalf("blocks = %+v, want text and pasted image", blocks)
	}
	if _, err := buildPromptWithImages("x", nil, []ImageAttachment{{Name: "paste.bmp", MimeType: "image/bmp", Data: data}}, t.TempDir(), false); err == nil {
		t.Fatal("unsupported pasted image type was accepted")
	}
}

func TestAgentCannotAttachOutsideItsProject(t *testing.T) {
	// This is the one that matters. Invoke is also how one agent prompts
	// another, and there the paths come from a model. DevKit reads attachments
	// with its own privileges, outside the sandbox that exists to stop exactly
	// this — so an agent-supplied path is confined to the session's project.
	project := t.TempDir()
	elsewhere := t.TempDir()
	secret := write(t, elsewhere, "id_rsa", "PRIVATE KEY")
	inside := write(t, project, "main.go", "package main")

	if _, err := buildPrompt("read this", []string{secret}, project, true); err == nil {
		t.Fatal("an agent attached a file outside the session's project")
	}
	if _, err := buildPrompt("read this", []string{inside}, project, true); err != nil {
		t.Fatalf("an agent could not attach a file inside its own project: %v", err)
	}

	// A person choosing a file in the composer is not the same actor, and is
	// not confined to the project.
	if _, err := buildPrompt("read this", []string{secret}, project, false); err != nil {
		t.Fatalf("a human attachment outside the project was refused: %v", err)
	}
}

func TestAgentCannotEscapeWithASymlink(t *testing.T) {
	// The check resolves symlinks before comparing, so a link planted inside
	// the project cannot point out of it.
	project := t.TempDir()
	elsewhere := t.TempDir()
	secret := write(t, elsewhere, "id_rsa", "PRIVATE KEY")
	link := filepath.Join(project, "innocent.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := buildPrompt("read this", []string{link}, project, true); err == nil {
		t.Fatal("a symlink out of the project was followed for an agent caller")
	}
}

func TestAttachmentRefusesWhatCannotBeRead(t *testing.T) {
	dir := t.TempDir()

	if _, err := buildPrompt("x", []string{filepath.Join(dir, "missing")}, dir, false); err == nil {
		t.Fatal("a missing file was accepted")
	}
	if _, err := buildPrompt("x", []string{dir}, dir, false); err == nil {
		t.Fatal("a directory was accepted")
	}

	// Binary that is not an image would reach the model as noise it would
	// answer about anyway.
	bin := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(bin, []byte{0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildPrompt("x", []string{bin}, dir, false); err == nil {
		t.Fatal("a binary file was accepted as text")
	}

	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, make([]byte, maxAttachmentBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildPrompt("x", []string{big}, dir, false); err == nil {
		t.Fatal("an oversized file was accepted")
	}
}

func TestEmptyPromptIsRefused(t *testing.T) {
	// Nothing to say and nothing attached is not a turn.
	if _, err := buildPrompt("   ", nil, t.TempDir(), false); err == nil {
		t.Fatal("an empty prompt was accepted")
	}
}
