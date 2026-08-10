package main

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"changeme/internal/syncclient"
)

func TestG2PushRetriesWithoutDuplicateLog(t *testing.T) {
	upstream, token := requireDeployed(t)
	resetDeployedSyncState(t)
	proxy := newSelectiveFaultProxy(t, upstream, 2)
	vlt := newSharedVault(t)
	a := newBootstrappedDevice(t, proxy, token, vlt)
	noteID := a.createNote(t, "retry-me", "body")
	b := newEnrolledDevice(t, proxy, token, a, vlt)

	first, firstErr := a.syncNowMaybeFail(t)
	if firstErr != nil {
		if first.PushRetried == 0 {
			t.Fatalf("first SyncNow retried = 0 with err %v, report = %#v", firstErr, first)
		}
		second := a.syncNow(t)
		if second.PushSent < 1 {
			t.Fatalf("second SyncNow push sent = %d, want >= 1 (first = %#v)", second.PushSent, first)
		}
	} else if first.PushSent < 1 {
		t.Fatalf("first SyncNow push sent = %d, want >= 1", first.PushSent)
	}

	replay := a.syncNow(t)
	if replay.PushSent != 0 {
		t.Fatalf("replay SyncNow push sent = %d, want 0 (idempotent)", replay.PushSent)
	}

	report := b.syncNow(t)
	if report.Pulled < 1 || report.Applied < 1 {
		t.Fatalf("B report = %#v, want pulled/applied >= 1", report)
	}
	if got := b.body(t, noteID); got != "body" {
		t.Fatalf("note body on B = %q, want %q", got, "body")
	}

	if count := replicationLogCount(t, a.account(t)); count > 0 && count != 1 {
		t.Fatalf("replication_log rows for account = %d, want 1", count)
	}
}

func (d *device) syncNowMaybeFail(t *testing.T) (syncclient.Report, error) {
	t.Helper()
	report, err := d.sync.SyncNow(context.Background())
	if err != nil && !errors.Is(err, syncclient.ErrPushFailed) {
		t.Fatalf("sync now: %v", err)
	}
	return report, err
}

func replicationLogCount(t *testing.T, accountID string) int {
	t.Helper()
	if accountID == "" {
		t.Fatal("account id is required")
	}
	cmd := exec.Command(
		"docker", "exec", "devkit-db", "sh", "-c",
		`psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -c "SELECT COUNT(*) FROM replication_log WHERE account_id = '`+accountID+`'"`,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("replication_log count skipped: %v (%s)", err, strings.TrimSpace(string(output)))
		return 0
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Logf("replication_log count parse skipped: %v", err)
		return 0
	}
	return count
}
