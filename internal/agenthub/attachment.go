package agenthub

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"devkit/internal/acp"
	"devkit/internal/identity"
)

// maxAttachmentBytes bounds one attached file.
//
// The file is read into memory, base64'd, and sent inside a JSON-RPC frame the
// agent must also hold — so a large attachment costs several times its size on
// both sides, and the model would refuse it anyway. Failing at 8 MiB with a
// sentence beats a turn that stalls and then errors somewhere less legible.
const maxAttachmentBytes = 8 << 20

// buildPrompt turns the user's text and the files they attached into the
// blocks session/prompt takes.
//
// The contents travel with the prompt rather than a path for the agent to
// open: the agent advertises promptCapabilities.embeddedContext, and a path is
// only useful to it if the file happens to be inside its jail — attaching
// something from elsewhere on the machine is exactly the case a path cannot
// serve. Images go as image blocks because that is what the vendor asked for;
// everything else goes as an embedded resource with its text.
func buildPrompt(text string, attachments []string, allowedRoot string, callerIsAgent bool) ([]acp.ContentBlock, error) {
	return buildPromptWithImages(text, attachments, nil, allowedRoot, callerIsAgent)
}

func buildPromptWithImages(text string, attachments []string, images []ImageAttachment, allowedRoot string, callerIsAgent bool) ([]acp.ContentBlock, error) {
	blocks := make([]acp.ContentBlock, 0, len(attachments)+len(images)+1)
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, acp.ContentBlock{Type: "text", Text: text})
	}

	for _, path := range attachments {
		block, err := attachmentBlock(path, allowedRoot, callerIsAgent)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	for _, image := range images {
		block, err := pastedImageBlock(image)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}

	if len(blocks) == 0 {
		return nil, fmt.Errorf("%w: prompt is empty", ErrInvalidCwd)
	}
	return blocks, nil
}

func pastedImageBlock(image ImageAttachment) (acp.ContentBlock, error) {
	allowed := map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}
	if !allowed[image.MimeType] {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is not a supported image", ErrInvalidCwd, image.Name)
	}
	body, err := base64.StdEncoding.DecodeString(image.Data)
	if err != nil {
		return acp.ContentBlock{}, fmt.Errorf("%w: invalid image data for %s", ErrInvalidCwd, image.Name)
	}
	if len(body) > maxAttachmentBytes {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is over the %d byte limit", ErrInvalidCwd, image.Name, maxAttachmentBytes)
	}
	return acp.ContentBlock{Type: "image", Data: image.Data, MimeType: image.MimeType, Name: image.Name}, nil
}

// attachmentBlock reads one file and wraps it.
//
// The path check is the point of the allowedRoot argument. A person attaching a
// file through the composer chose it themselves, but Invoke is also how one
// agent prompts another (invokeAgent), and there the paths come from a model.
// DevKit reads these files with its own privileges, outside the sandbox that
// exists to stop exactly this — so an agent-supplied attachment is confined to
// the session's own project, and the jail's boundary is preserved by the one
// component that could otherwise walk around it.
func attachmentBlock(path, allowedRoot string, callerIsAgent bool) (acp.ContentBlock, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s", ErrInvalidCwd, path)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s: %v", ErrInvalidCwd, path, err)
	}

	if callerIsAgent {
		root, err := filepath.EvalSymlinks(allowedRoot)
		if err != nil || !withinRoot(resolved, root) {
			return acp.ContentBlock{}, fmt.Errorf("%w: attachment outside the session's project: %s",
				ErrCwdOutsideParent, path)
		}
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s: %v", ErrInvalidCwd, path, err)
	}
	if info.IsDir() {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is a directory", ErrInvalidCwd, path)
	}
	if info.Size() > maxAttachmentBytes {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is %d bytes, over the %d limit",
			ErrInvalidCwd, filepath.Base(path), info.Size(), maxAttachmentBytes)
	}

	body, err := os.ReadFile(resolved)
	if err != nil {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s: %v", ErrInvalidCwd, path, err)
	}

	mimeType := mime.TypeByExtension(filepath.Ext(resolved))
	uri := "file://" + resolved

	if strings.HasPrefix(mimeType, "image/") {
		return acp.ContentBlock{
			Type:     "image",
			Data:     base64.StdEncoding.EncodeToString(body),
			MimeType: mimeType,
			URI:      uri,
		}, nil
	}

	// Anything else rides as text. A binary that is not an image would arrive
	// as mojibake, so it is refused rather than sent as something the model
	// will read as noise and answer about anyway.
	if !utf8Text(body) {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is not text or an image",
			ErrInvalidCwd, filepath.Base(path))
	}
	if mimeType == "" {
		mimeType = "text/plain"
	}
	return acp.ContentBlock{
		Type: "resource",
		Resource: &acp.ResourceContents{
			URI:      uri,
			Text:     string(body),
			MimeType: mimeType,
		},
	}, nil
}

// withinRoot reports whether path is root or sits under it. Compares cleaned
// absolute paths with a separator so "/a/bc" does not pass as inside "/a/b".
func withinRoot(path, root string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// utf8Text reports whether the bytes look like text a model can read. A NUL
// byte is the cheap, reliable tell for binary; the rest is left to the model.
func utf8Text(body []byte) bool {
	for _, b := range body {
		if b == 0 {
			return false
		}
	}
	return true
}

// callerIsAgent is a thin wrapper so buildPrompt's call site reads as prose.
func callerIsAgent(ctxCaller identity.Caller) bool { return ctxCaller.IsAgent() }
