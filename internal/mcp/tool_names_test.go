package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// toolNamePattern matches `Name: "foo.bar"` (and the aligned `Name:        "foo.bar",`
// gofmt produces inside an sdk.Tool literal) across every tools_*.go source file.
var toolNamePattern = regexp.MustCompile(`Name:\s*"([^"]+)"`)

// wireForm mirrors the substitution a vendor's function-name channel makes on
// our dotted tool names — "mcp__devkit__" + every "." turned into "_"
// (docs/dev-kit/tool-result-view.mdx § Cách tương tác). ToolResultView's
// frontend registry keys off this transform, computed forward from each
// renderer's own registered name — never guessed backwards from an arbitrary
// wire string, which is ambiguous for a name that nests ("tasks.board.list")
// or already contains "_" ("notes.set_organization").
func wireForm(name string) string {
	return "mcp__devkit__" + strings.ReplaceAll(name, ".", "_")
}

// TestToolNamesNoWireCollision guards the one thing that transform depends
// on staying true: two different Go tool names must never mangle to the same
// wire form. If they did, a vendor could not tell the two functions apart
// either — this would already be a bug in the tool set itself, independent
// of ToolResultView, so the check lives here rather than in the frontend.
func TestToolNamesNoWireCollision(t *testing.T) {
	files, err := filepath.Glob("tools_*.go")
	if err != nil {
		t.Fatalf("glob tools_*.go: %v", err)
	}
	seen := map[string]string{} // wire form -> first Go name that produced it
	count := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range toolNamePattern.FindAllStringSubmatch(string(data), -1) {
			name := m[1]
			wire := wireForm(name)
			if prev, ok := seen[wire]; ok && prev != name {
				t.Errorf("%s (in %s) and %s both mangle to %q — a vendor cannot tell them apart on the wire",
					prev, path, name, wire)
			}
			seen[wire] = name
			count++
		}
	}
	if count == 0 {
		t.Fatal("found no tool Name literals — toolNamePattern or the glob is broken")
	}
}

// TestNoToolSignsAgentsIn: signing an agent in or out, and choosing the API
// key it signs in with, is the person's, from the app (Codex spec §6.4). A
// tool for it would let one session put its own account in the home every
// later session uses, or sign the person out mid-work. The service refuses an agent caller too; this keeps the door from
// being built in the first place.
func TestNoToolSignsAgentsIn(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	signInName := regexp.MustCompile(`(?i)sign.?in|sign.?out|log.?in|log.?out`)
	signInCall := regexp.MustCompile(`\b(SignInStatus|StartSignIn|StartBrowserSignIn|StartDeviceSignIn|CancelSignIn|SignOut|SignInCommand|CodexAPIKey|CodexAPIKeyAccount|SetCodexAPIKeyAccount)\b`)
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.HasPrefix(path, "tools_") {
			for _, m := range toolNamePattern.FindAllStringSubmatch(string(data), -1) {
				if signInName.MatchString(m[1]) {
					t.Errorf("%s declares tool %q: sign-in is UI only", path, m[1])
				}
			}
		}
		if loc := signInCall.FindString(string(data)); loc != "" {
			t.Errorf("%s calls %s: sign-in is UI only", path, loc)
		}
	}
}

// TestNoToolBindsBoards: a binding decides which boards an agent sees, so an
// agent that could bind a board could widen its own scope. Bindings are
// tasks:admin and UI-only — they control access, never grant it.
func TestNoToolBindsBoards(t *testing.T) {
	files, err := filepath.Glob("tools_*.go")
	if err != nil {
		t.Fatalf("glob tools_*.go: %v", err)
	}
	bindName := regexp.MustCompile(`tasks\..*(bind|unbind)`)
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range toolNamePattern.FindAllStringSubmatch(string(data), -1) {
			if bindName.MatchString(m[1]) {
				t.Errorf("%s declares tool %q: board binding is UI only", path, m[1])
			}
		}
	}
}
