package agenthub

import (
	"errors"
	"strings"
	"testing"
)

func TestParseAttachedRefRoundTrips(t *testing.T) {
	ref, err := parseAttachedRef("devkit://codegraph/endpoint/POST%20%2Frefunds?label=POST+%2Frefunds&project=%2Fhome%2Fme%2Fapp")
	if err != nil {
		t.Fatalf("parseAttachedRef: %v", err)
	}
	if ref.Module != "codegraph" || ref.Kind != "endpoint" || ref.ID != "POST /refunds" {
		t.Fatalf("ref = %+v, want module=codegraph kind=endpoint id=%q", ref, "POST /refunds")
	}
	if ref.Project != "/home/me/app" {
		t.Fatalf("ref.Project = %q, want /home/me/app", ref.Project)
	}
}

func TestParseAttachedRefRefusesWhatIsNotADevkitURI(t *testing.T) {
	for _, raw := range []string{
		"http://codegraph/endpoint/x",
		"devkit://codegraph",
		"devkit://codegraph/endpoint",
		"not a uri at all",
	} {
		if _, err := parseAttachedRef(raw); !errors.Is(err, ErrInvalidPrompt) {
			t.Fatalf("parseAttachedRef(%q) err = %v, want ErrInvalidPrompt", raw, err)
		}
	}
}

func TestRefResolutionHintNamesTheShippedTools(t *testing.T) {
	cases := map[AttachedRef]string{
		{Module: "codegraph", Kind: "endpoint", ID: "POST /x"}: "codegraph.getApiFlow",
		{Module: "codegraph", Kind: "file", ID: "a.go"}:        "codegraph.readFile",
		{Module: "tasks", Kind: "task", ID: "T-1"}:             "tasks.get",
		{Module: "apiclient", Kind: "request", ID: "r1"}:       "api.getRequest",
		{Module: "accounts", Kind: "account", ID: "a1"}:        "accounts.get",
		{Module: "git", Kind: "commit", ID: "abc123"}:          "git.diff",
	}
	for ref, tool := range cases {
		if hint := refResolutionHint(ref); !strings.Contains(hint, tool) {
			t.Errorf("refResolutionHint(%+v) = %q, want it to name %s", ref, hint, tool)
		}
	}
}

// A kind nobody has built a resolver for must say so plainly rather than
// naming a tool that does not exist — see refResolutionHint's own comment.
func TestRefResolutionHintIsHonestAboutWhatHasNoResolver(t *testing.T) {
	hint := refResolutionHint(AttachedRef{Module: "codegraph", Kind: "planNode", ID: "n1"})
	if strings.Contains(hint, "call ") {
		t.Fatalf("refResolutionHint invented a tool call for an unresolved kind: %q", hint)
	}
}

func TestBuildPromptWithImagesAppendsARefsBlockAsItsOwnTextBlock(t *testing.T) {
	blocks, err := buildPromptWithImages(
		"look at this",
		nil,
		nil,
		[]string{"devkit://codegraph/endpoint/POST%20%2Frefunds?label=POST+%2Frefunds"},
		t.TempDir(),
		false,
	)
	if err != nil {
		t.Fatalf("buildPromptWithImages: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %+v, want prompt text plus one refs block", blocks)
	}
	if blocks[0].Text != "look at this" {
		t.Fatalf("blocks[0] = %+v, want the prompt's own text untouched", blocks[0])
	}
	if !strings.Contains(blocks[1].Text, "codegraph.getApiFlow") || !strings.Contains(blocks[1].Text, "devkit://codegraph/endpoint") {
		t.Fatalf("blocks[1].Text = %q, want the ref URI and its resolution hint", blocks[1].Text)
	}
}

func TestBuildPromptWithImagesRefusesAMalformedRef(t *testing.T) {
	if _, err := buildPromptWithImages("x", nil, nil, []string{"not-a-devkit-uri"}, t.TempDir(), false); !errors.Is(err, ErrInvalidPrompt) {
		t.Fatalf("err = %v, want ErrInvalidPrompt", err)
	}
}
