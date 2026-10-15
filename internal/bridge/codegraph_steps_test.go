package bridge

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"devkit/internal/apperrors"
	"devkit/internal/codegraph"
	"devkit/internal/preferences"
)

// newStepsService is a bridge whose CodeGraph has the given index open; its
// preferences are the container's in-memory ones.
func newStepsService(t *testing.T, result codegraph.AnalysisResult) *AppService {
	t.Helper()
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	if err := s.container.CodeGraph.OpenProjectAt(context.Background(), filepath.Join(t.TempDir(), "index.db"), "/fake/project"); err != nil {
		t.Fatalf("OpenProjectAt: %v", err)
	}
	t.Cleanup(func() { s.container.CodeGraph.Close() })
	return s
}

func TestCodeGraphGetStepsAppliesSavedOverrides(t *testing.T) {
	service := newStepsService(t, codegraph.AnalysisResult{
		Nodes: []codegraph.Node{{ID: "a", Label: "a", Kind: "method", Layer: "service", FilePath: "a.java", Line: 1, EndLine: 2}},
		CallSites: []codegraph.CallSite{{From: "a", Line: 2, Unresolved: "library", Text: "log.warn(x)",
			CalleeKey: "org.slf4j.Logger#warn", ResultUse: "discarded"}},
	})
	got, err := service.CodeGraphGetSteps([]string{"a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if steps := got.Steps["a"]; len(steps) != 1 || steps[0].Visible {
		t.Fatalf("steps = %+v", got.Steps)
	}
	if err := service.PreferencesSetCallVisibility("org.slf4j.Logger#warn", "show"); err != nil {
		t.Fatal(err)
	}
	overrides, err := service.PreferencesGetCallVisibility()
	if err != nil || overrides["org.slf4j.Logger#warn"] != "show" {
		t.Fatalf("overrides = %v, err = %v", overrides, err)
	}
	got, _ = service.CodeGraphGetSteps([]string{"a"}, "")
	if !got.Steps["a"][0].Visible {
		t.Fatalf("override not applied: %+v", got.Steps["a"])
	}
}

func TestPreferencesSetCallVisibilityRefusesABadKeyAsValidation(t *testing.T) {
	service := newTestService(t)
	err := service.PreferencesSetCallVisibility("no-hash", "show")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != codeValidationFailed {
		t.Fatalf("err = %v, want %s", err, codeValidationFailed)
	}
}

func TestCodeGraphGetStepsTooManyNodesIsValidation(t *testing.T) {
	service := newStepsService(t, codegraph.AnalysisResult{
		Nodes: []codegraph.Node{{ID: "a", Label: "a", Kind: "method", Layer: "service", FilePath: "a.java", Line: 1, EndLine: 2}},
	})
	ids := make([]string, codegraph.MaxStepNodeIDs+1)
	for i := range ids {
		ids[i] = "n" + strconv.Itoa(i)
	}
	_, err := service.CodeGraphGetSteps(ids, "")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != codeValidationFailed {
		t.Fatalf("err = %v, want %s", err, codeValidationFailed)
	}
}

// unreadableOverrides is a preferences store whose call-visibility row cannot
// be read.
type unreadableOverrides struct {
	*preferences.MemoryRepository
}

func (unreadableOverrides) GetCallVisibility() (map[string]string, bool, error) {
	return nil, false, errors.New("disk I/O error")
}

func TestCodeGraphGetStepsWithUnreadableOverridesUsesTheDefaults(t *testing.T) {
	service := newStepsService(t, codegraph.AnalysisResult{
		Nodes: []codegraph.Node{{ID: "a", Label: "a", Kind: "method", Layer: "service", FilePath: "a.java", Line: 1, EndLine: 2}},
		CallSites: []codegraph.CallSite{{From: "a", Line: 2, Unresolved: "library", Text: "log.warn(x)",
			CalleeKey: "org.slf4j.Logger#warn", ResultUse: "discarded"}},
	})
	service.container.Preferences = preferences.NewService(unreadableOverrides{preferences.NewMemoryRepository()})
	got, err := service.CodeGraphGetSteps([]string{"a"}, "")
	if err != nil {
		t.Fatalf("an unreadable preference failed the outline: %v", err)
	}
	if steps := got.Steps["a"]; len(steps) != 1 || steps[0].Visible {
		t.Fatalf("steps = %+v, want the default (hidden) answer", got.Steps)
	}
}

func TestPreferencesSetCallVisibilityPastTheCapSaysSo(t *testing.T) {
	service := newTestService(t)
	for i := 0; i < 500; i++ {
		if err := service.PreferencesSetCallVisibility("T#m"+strconv.Itoa(i), "show"); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
	}
	err := service.PreferencesSetCallVisibility("T#one-more", "show")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != codeValidationFailed ||
		appErr.Message != "You can keep at most 500 always show/hide choices." {
		t.Fatalf("err = %#v", err)
	}
}
