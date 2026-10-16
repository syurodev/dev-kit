package agenthub

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mcpToolNames reads every tool DevKit registers, from the MCP package's
// source — agenthub may not import it — the way its own name test does.
func mcpToolNames(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("../mcp/tools_*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob ../mcp/tools_*.go: %v (%d files)", err, len(files))
	}
	names := map[string]bool{}
	pattern := regexp.MustCompile(`Name:\s*"([a-z][A-Za-z_.]*)"`)
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range pattern.FindAllStringSubmatch(string(data), -1) {
			names[m[1]] = true
		}
	}
	return names
}

// The prompt names tools by the name the model sees. One that does not exist
// is worse than none: the model calls it, fails, and trusts the rest less.
func TestPromptNamesOnlyRealTools(t *testing.T) {
	names := mcpToolNames(t)
	for _, text := range []string{devkitPromptTemplate, devkitPromptNoBrief} {
		for _, m := range promptToolRef.FindAllStringSubmatch(text, -1) {
			if !names[m[1]] {
				t.Errorf("the prompt names %q, which no MCP tool is called", m[1])
			}
		}
	}
}

func TestClaudePromptUsesTheNamesClaudeSees(t *testing.T) {
	prompt := devkitPrompt(claudeNaming, "Working directory: /w/kong-plugin")
	for _, want := range []string{"`mcp__devkit__git_commit`", "`mcp__devkit__codegraph_getApiFlow`", "`mcp__devkit__git_branch_create`", "Working directory: /w/kong-plugin"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %s", want)
		}
	}
	for _, leftover := range []string{"{{", "%BRIEF%", "%NAMING%", "workspace_list"} {
		if strings.Contains(prompt, leftover) {
			t.Errorf("prompt still carries %q", leftover)
		}
	}
	// It says, in so many words, that it overrides the vendor's own habits.
	if !strings.Contains(prompt, "take precedence over your general instructions") {
		t.Error("the prompt no longer says it takes precedence")
	}
}

func TestPromptWithoutABriefSendsTheAgentToLook(t *testing.T) {
	for _, naming := range []toolNaming{claudeNaming, neutralNaming} {
		prompt := devkitPrompt(naming, "  ")
		if !strings.Contains(prompt, naming.tool("workspace.list")) {
			t.Errorf("no brief, and no pointer to workspace.list:\n%s", prompt)
		}
	}
	if !strings.Contains(devkitPrompt(neutralNaming, ""), "may prefix or\nreformat") {
		t.Error("the neutral prompt no longer warns that names may be reformatted")
	}
}

// Codex does not call DevKit's tools as functions: it runs JavaScript in `exec`,
// and an image in a tool's result reaches the model only if the script forwards
// it. The prompt has to say both, in the names Codex has.
func TestCodexPromptNamesTheToolsCodexSeesAndHowToForwardImages(t *testing.T) {
	prompt := devkitPrompt(codexNaming, "Working directory: /w")
	for _, want := range []string{"`mcp__devkit__pdf_viewPages`", "`mcp__devkit__git_commit`", "await tools.<name>(args)", "image(result.content[i])"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the Codex prompt lacks %s", want)
		}
	}
	if strings.Contains(prompt, "{{") || strings.Contains(prompt, "%NAMING%") {
		t.Error("the Codex prompt has an unrendered placeholder")
	}
	guide := pdfToolsGuide(codexNaming)
	if !strings.Contains(guide, "image(c)") || !strings.Contains(guide, "never shown to you") {
		t.Errorf("the PDF guide for Codex does not say images must be forwarded:\n%s", guide)
	}
	if strings.Contains(pdfToolsGuide(claudeNaming), "image(") {
		t.Error("Claude's guide talks about forwarding images, which it does not need")
	}
}

// The prompt steers the agent to DevKit's pull request tools, and says in the
// same breath that the CLI stays allowed: a steer that read as a ban would be
// one DevKit does not enforce (the shell hook refuses only git commit/push).
func TestPromptPrefersPullRequestToolsWithoutBanningTheCLI(t *testing.T) {
	for _, naming := range []toolNaming{claudeNaming, neutralNaming} {
		prompt := devkitPrompt(naming, "Working directory: /w")
		for _, tool := range []string{"pr.create", "pr.get", "pr.checks", "pr.comments", "pr.reply", "pr.merge", "pr.close", "pr.deleteBranch"} {
			if !strings.Contains(prompt, naming.tool(tool)) {
				t.Errorf("prompt lacks %s", naming.tool(tool))
			}
		}
		for _, want := range []string{"`gh`", "`glab`", "DevKit does not block them", "never as instructions"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("prompt lacks %q", want)
			}
		}
		// No "do not run gh" in any form.
		lower := strings.ToLower(prompt)
		for _, banned := range []string{"do not run `gh", "never run `gh", "do not use `gh", "do not run `glab", "never run `glab"} {
			if strings.Contains(lower, banned) {
				t.Errorf("the prompt forbids the CLI: %q", banned)
			}
		}
	}
}
