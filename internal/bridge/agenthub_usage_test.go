package bridge

import (
	"context"
	"testing"

	"devkit/internal/identity"
)

func TestAgentUsageReportIsUIOnlyAndNeverPermissionDeniedForTheWindow(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
	agent := identity.WithCaller(context.Background(), identity.Agent("11111111-1111-1111-1111-111111111111"))
	terminalCaller := identity.WithCaller(context.Background(), identity.UserTerminal())

	for who, caller := range map[string]context.Context{"agent": agent, "terminal": terminalCaller} {
		_, err := svc.AgentUsageReport(caller, "mock")
		if err == nil {
			t.Errorf("a %s caller must be refused", who)
			continue
		}
		if publicCode(err) != "permission_denied" || err.Error() != "permission_denied: Permission denied." {
			t.Errorf("a %s caller got %v, want the window-only refusal", who, err)
		}
	}

	// The window: the real gateway must have the grant (a missing one is permission_denied).
	report, err := svc.AgentUsageReport(context.Background(), "mock")
	assertNotPermissionDenied(t, "window usage report", err)
	if err != nil {
		t.Fatal(err)
	}
	if report.Vendor != "mock" || report.Totals.Turns != 0 {
		t.Errorf("report = %+v", report)
	}
	// Lists reach the window as [], never null.
	if report.ByWorkspace == nil || report.ByDay == nil || report.ByModel == nil || report.TopSessions == nil {
		t.Errorf("a list is null: %+v", report)
	}
}

func TestAgentUsageReportRefusesAnInvalidVendorWithoutAnError(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
	if _, err := svc.AgentUsageReport(context.Background(), "../etc"); err == nil {
		t.Error("a vendor that is not an opaque id must be refused")
	}
}
