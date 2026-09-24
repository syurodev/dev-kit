package sandbox

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRejectsRelativeDenyWritePath(t *testing.T) {
	err := Policy{WorkDir: "/tmp/proj", DenyWritePaths: []string{"rules"}}.Validate()
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Validate = %v, want ErrInvalidPolicy", err)
	}
}

func TestSeatbeltProfileDenyWritePaths(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{
		WorkDir:        "/tmp/proj",
		ReadWritePaths: []string{"/Users/alice/Library/Application Support/DevKit/agent-homes/codex"},
		DenyWritePaths: []string{"/Users/alice/Library/Application Support/DevKit/agent-homes/codex/rules"},
		Env:            map[string]string{"HOME": "/Users/alice"},
	}.withDefaults())
	block := "(deny file-write*\n  (subpath \"/Users/alice/Library/Application Support/DevKit/agent-homes/codex/rules\")"
	at := strings.Index(prof, block)
	if at < 0 {
		t.Fatalf("expected a deny-write block for rules: %s", prof)
	}
	if strings.LastIndex(prof, "(allow ") > at {
		t.Fatalf("deny-write must come after every allow (the later rule wins): %s", prof)
	}
	want := `(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal "/Users/alice/Library/Application Support/DevKit/agent-homes/codex")))`
	if !strings.Contains(prof, want) {
		t.Fatalf("the deny-write path's parent must not be renamable: %s", prof)
	}
}
