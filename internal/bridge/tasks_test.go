package bridge

import (
	"testing"

	"devkit/internal/app"
)

// The board's project picker is the Tasks module's own, under tasks:admin.
// Reaching for the Agent Hub's picker would have handed this module the
// capability that one is gated on, along with everything else it covers.
func TestTasksBoardChooseProjectPassesStartInAndReturnsChoice(t *testing.T) {
	svc := NewAppService(app.NewContainer())
	var gotTitle, gotStartIn string
	svc.openDirDialog = func(title, startIn string) (string, error) {
		gotTitle, gotStartIn = title, startIn
		return "/acme/billing-service", nil
	}

	got, err := svc.TasksBoardChooseProject("/acme")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/acme/billing-service" {
		t.Fatalf("path = %q", got)
	}
	if gotStartIn != "/acme" {
		t.Fatalf("startIn = %q, want /acme", gotStartIn)
	}
	if gotTitle == "" {
		t.Fatal("dialog opened with no title")
	}
}

// Backing out of a picker is not a failure the user caused, and rendering it
// as one in a settings dialog reads as though saving broke.
func TestTasksBoardChooseProjectCancelIsNotAnError(t *testing.T) {
	svc := NewAppService(app.NewContainer())
	svc.openDirDialog = func(string, string) (string, error) { return "", nil }

	got, err := svc.TasksBoardChooseProject("")
	if err != nil {
		t.Fatalf("cancel reported as error: %v", err)
	}
	if got != "" {
		t.Fatalf("path = %q, want empty", got)
	}
}
