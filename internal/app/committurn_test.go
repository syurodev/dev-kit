package app

import (
	"strings"
	"testing"

	gitservice "devkit/internal/git"
)

// The body is the list of paths — a fact — and the subject stays empty,
// because a subject filled in for you is a subject nobody rewrites.
func TestSeedTurnMessageLeavesTheSubjectBlank(t *testing.T) {
	message := seedTurnMessage([]string{"internal/refund/service.go", "internal/refund/service_test.go"}, "")
	lines := strings.Split(message, "\n")
	if lines[0] != "" {
		t.Fatalf("a subject was written for the user: %q", lines[0])
	}
	if !strings.Contains(message, "- internal/refund/service.go") {
		t.Fatalf("the staged paths are not in the body:\n%s", message)
	}
}

func TestSeedTurnMessageCarriesTheTaskTrailer(t *testing.T) {
	message := seedTurnMessage([]string{"a.go"}, "DEV-4f2a91")
	if gitservice.TaskKeyOf(message) != "DEV-4f2a91" {
		t.Fatalf("trailer not readable back:\n%s", message)
	}
	// No task is not an error: plenty of turns are not a task's.
	if key := gitservice.TaskKeyOf(seedTurnMessage([]string{"a.go"}, "")); key != "" {
		t.Fatalf("a trailer appeared with no task: %q", key)
	}
}

// A turn that touched two hundred files must not paste two hundred lines into
// the commit box.
func TestSeedTurnMessageIsBounded(t *testing.T) {
	paths := make([]string, 200)
	for i := range paths {
		paths[i] = "pkg/file.go"
	}
	message := seedTurnMessage(paths, "")
	if strings.Count(message, "\n- ") > 21 {
		t.Fatalf("unbounded body:\n%s", message)
	}
	if !strings.Contains(message, "…and 180 more") {
		t.Fatalf("the remainder was dropped silently:\n%s", message)
	}
}
