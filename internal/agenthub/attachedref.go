package agenthub

import (
	"fmt"
	"net/url"
	"strings"
)

// AttachedRef is one EntityRef (docs/dev-kit/cross-module.mdx § Primitive 1),
// addressed by its devkit:// URI rather than by content. Unlike a file
// attachment, DevKit never resolves what a ref points at: the agent reads it
// fresh through the owning module's own MCP tool at the point it actually
// needs it, so a stale snapshot taken at attach time is never sent in place
// of live data.
type AttachedRef struct {
	URI     string
	Module  string
	Kind    string
	ID      string
	Label   string
	Project string
}

// parseAttachedRef decodes "devkit://<module>/<kind>/<id>[?label=&project=]",
// the string form docs/dev-kit/cross-module.mdx defines for EntityRef ("để
// dán, để agent đọc"). Refused the same way a bad attachment path is: this
// runs during the same pre-turn validation as buildPromptWithImages, so a
// malformed ref never reaches the transcript.
func parseAttachedRef(raw string) (AttachedRef, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "devkit" || u.Host == "" {
		return AttachedRef{}, fmt.Errorf("%w: not a devkit reference: %s", ErrInvalidPrompt, raw)
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return AttachedRef{}, fmt.Errorf("%w: malformed devkit reference: %s", ErrInvalidPrompt, raw)
	}
	id, err := url.PathUnescape(parts[1])
	if err != nil {
		return AttachedRef{}, fmt.Errorf("%w: malformed devkit reference: %s", ErrInvalidPrompt, raw)
	}
	q := u.Query()
	return AttachedRef{
		URI:     raw,
		Module:  u.Host,
		Kind:    parts[0],
		ID:      id,
		Label:   q.Get("label"),
		Project: q.Get("project"),
	}, nil
}

// refResolutionHint names the MCP tool call that reads a ref's kind fresh,
// for the one line the agent gets alongside each attached reference. Only
// kinds with a shipped resolver get a tool call; every other kind says so
// plainly rather than inventing one — that dishonesty would be worse than the
// gap, because a made-up tool call fails at run time with no context left to
// explain why.
func refResolutionHint(ref AttachedRef) string {
	switch ref.Module + "." + ref.Kind {
	case "codegraph.endpoint":
		return fmt.Sprintf("call codegraph.getApiFlow with endpointId=%q for its current call chain", ref.ID)
	case "codegraph.file":
		return fmt.Sprintf("call codegraph.readFile with relativePath=%q for its current contents", ref.ID)
	case "tasks.task":
		return fmt.Sprintf("call tasks.get with taskId=%q for its current status", ref.ID)
	case "apiclient.request":
		return fmt.Sprintf("call api.getRequest with requestId=%q for its current definition", ref.ID)
	case "accounts.account":
		return fmt.Sprintf("call accounts.get with accountId=%q for its current status (never a credential)", ref.ID)
	case "git.commit":
		return fmt.Sprintf("call git.diff with rev=%q for what it changed", ref.ID)
	default:
		return "no DevKit tool resolves this reference yet — ask the person what it contains rather than guessing"
	}
}

// refsBlock renders attached references as one text block: a label line per
// ref plus how to resolve it. A separate block from the prompt's own text so
// a transcript reader can tell what the person wrote from what DevKit added.
func refsBlock(refs []AttachedRef) string {
	var b strings.Builder
	b.WriteString("Attached references — read each one fresh through the tool named, not from this line:\n")
	for _, ref := range refs {
		label := ref.Label
		if label == "" {
			label = ref.ID
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", label, ref.URI, refResolutionHint(ref))
	}
	return b.String()
}
