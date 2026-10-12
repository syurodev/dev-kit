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

// maxImageBase64Bytes bounds one image the way Claude measures it: the base64
// payload, not the decoded bytes. The API takes 10 MB per image directly but 5
// MB on Bedrock and Vertex, and DevKit cannot tell which one the agent is
// signed in to, so the lower limit holds. The old 8 MiB decoded limit was
// over 10 MiB of base64 — past both. Mirrors MAX_AGENT_IMAGE_BYTES in
// frontend/src/modules/agenthub/imageAttachments.ts.
const maxImageBase64Bytes = 5 << 20

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
	return buildPromptWithImages(text, attachments, nil, nil, allowedRoot, callerIsAgent)
}

// buildPromptWithImages also takes refs — devkit:// EntityRef URIs
// (attachedref.go) — which never turn into their own content block: they are
// not files DevKit read, so they ride as one line each inside a text block
// that names the MCP tool that resolves them, appended after the prompt's own
// text rather than mixed into it.
func buildPromptWithImages(text string, attachments []string, images []ImageAttachment, refs []string, allowedRoot string, callerIsAgent bool) ([]acp.ContentBlock, error) {
	blocks := make([]acp.ContentBlock, 0, len(attachments)+len(images)+2)
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
	if len(refs) > 0 {
		parsed := make([]AttachedRef, 0, len(refs))
		for _, raw := range refs {
			ref, err := parseAttachedRef(raw)
			if err != nil {
				return nil, err
			}
			parsed = append(parsed, ref)
		}
		blocks = append(blocks, acp.ContentBlock{Type: "text", Text: refsBlock(parsed)})
	}

	if len(blocks) == 0 {
		return nil, fmt.Errorf("%w: prompt is empty", ErrInvalidPrompt)
	}
	return blocks, nil
}

func pastedImageBlock(image ImageAttachment) (acp.ContentBlock, error) {
	allowed := map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}
	if !allowed[image.MimeType] {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is not a supported image", ErrInvalidPrompt, image.Name)
	}
	// Size first, on the encoded length: decoding an oversized paste only to
	// refuse it spends the memory the limit is there to protect.
	if len(image.Data) > maxImageBase64Bytes {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is over the %d byte image limit", ErrInvalidPrompt, image.Name, maxImageBase64Bytes)
	}
	if _, err := base64.StdEncoding.DecodeString(image.Data); err != nil {
		return acp.ContentBlock{}, fmt.Errorf("%w: invalid image data for %s", ErrInvalidPrompt, image.Name)
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
		return acp.ContentBlock{}, fmt.Errorf("%w: %s", ErrInvalidPrompt, path)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s: %v", ErrInvalidPrompt, path, err)
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
		return acp.ContentBlock{}, fmt.Errorf("%w: %s: %v", ErrInvalidPrompt, path, err)
	}
	if info.IsDir() {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is a directory", ErrInvalidPrompt, path)
	}
	mimeType := mime.TypeByExtension(filepath.Ext(resolved))
	isImage := strings.HasPrefix(mimeType, "image/")
	if info.Size() > maxAttachmentBytes {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is %d bytes, over the %d limit",
			ErrInvalidPrompt, filepath.Base(path), info.Size(), maxAttachmentBytes)
	}
	if isImage && base64.StdEncoding.EncodedLen(int(info.Size())) > maxImageBase64Bytes {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is %d bytes, over the %d byte base64 image limit",
			ErrInvalidPrompt, filepath.Base(path), info.Size(), maxImageBase64Bytes)
	}

	body, err := os.ReadFile(resolved)
	if err != nil {
		return acp.ContentBlock{}, fmt.Errorf("%w: %s: %v", ErrInvalidPrompt, path, err)
	}

	uri := "file://" + resolved

	// PDF and Office files: DevKit reads the text out of them (attachment_docs.go).
	if kind := documentKind(filepath.Ext(resolved)); kind != "" {
		text, err := extractDocument(kind, body)
		if err != nil {
			return acp.ContentBlock{}, fmt.Errorf("%w: %s: %v", ErrInvalidPrompt, filepath.Base(path), err)
		}
		return acp.ContentBlock{
			Type: "resource",
			Resource: &acp.ResourceContents{
				URI:      uri,
				Text:     text,
				MimeType: "text/plain",
			},
		}, nil
	}

	if isImage {
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
		return acp.ContentBlock{}, fmt.Errorf("%w: %s is not text or an image (DevKit reads %s)",
			ErrInvalidPrompt, filepath.Base(path), supportedDocumentTypes())
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
