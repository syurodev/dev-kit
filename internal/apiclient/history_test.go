package apiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"devkit/internal/storage"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

func newServiceWithHistory(t *testing.T) (*Service, *vault.Service) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(t.TempDir() + "/devkit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	envs := stubEnvs{
		exists:  map[string]bool{"dev": true, "prod": true},
		confirm: map[string]bool{"prod": true},
	}
	svc := NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v), envs)
	svc.SetHistory(db, storage.TableAPIRequestHistory, v)
	return svc, v
}

// The URL is sealed at rest because an interpolated URL can carry a secret
// variable, and a local file is still at rest. The row around it is not: a
// status and a duration cannot be a credential.
func TestHistorySealsTheURLAndStoresNoBody(t *testing.T) {
	svc, v := newServiceWithHistory(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte("a body that must not be kept"))
	}))
	defer server.Close()

	env, err := svc.CreateEnvironment("Local", "dev",
		map[string]string{"baseUrl": server.URL}, map[string]string{"token": "s3cret-token"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.SaveRequest(Request{
		Name: "Ping", Method: "get", URL: "{{baseUrl}}/ping",
		Query: map[string]string{"key": "{{token}}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	full, err := svc.Request(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Send(context.Background(), SendInput{Request: full, EnvironmentID: env.ID}); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.History(saved.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("history = %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Status != 201 || entry.Method != "GET" || entry.Bytes == 0 {
		t.Fatalf("entry = %+v", entry)
	}
	// The recorded URL is the one that actually went out, query and all.
	if !strings.Contains(entry.URL, "key=s3cret-token") {
		t.Fatalf("the recorded URL is not the one that was sent: %q", entry.URL)
	}

	// Nothing in the table itself may hold the secret in plaintext.
	var raw string
	if err := svc.history.db.QueryRow(
		`SELECT url_envelope FROM ` + storage.TableAPIRequestHistory).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "s3cret-token") {
		t.Fatalf("the URL was stored in plaintext: %s", raw)
	}
	if strings.Contains(raw, "must not be kept") {
		t.Fatal("the response body was stored")
	}

	// A locked vault means the row still lists, without its URL: that is a
	// locked vault, not a broken row.
	v.Lock()
	entries, err = svc.History(saved.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != 201 {
		t.Fatalf("a locked vault lost the row: %+v", entries)
	}
	if entries[0].URL != "" {
		t.Fatal("a locked vault handed back the URL")
	}
}

// A history that lists only what succeeded cannot answer "did I actually send
// that". A refusal at the gate is exactly the case worth being able to check.
func TestHistoryRecordsARefusalAsSendingNothing(t *testing.T) {
	svc, _ := newServiceWithHistory(t)
	svc.SetConfirmer(&stubConfirm{allow: false})

	env, err := svc.CreateEnvironment("Prod", "prod", map[string]string{"baseUrl": "https://api.example"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.SaveRequest(Request{Name: "Deploy", Method: "post", URL: "{{baseUrl}}/deploy"})
	if err != nil {
		t.Fatal(err)
	}
	full, err := svc.Request(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Send(context.Background(), SendInput{Request: full, EnvironmentID: env.ID}); err == nil {
		t.Fatal("a refused send succeeded")
	}

	entries, err := svc.History(saved.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("history = %d entries, want the refusal", len(entries))
	}
	if entries[0].Status != 0 || entries[0].Failure == "" {
		t.Fatalf("a refusal must be recorded as nothing sent: %+v", entries[0])
	}
}

// Unbounded growth in a local table is the kind of thing that bites a year
// later. The row that pushes past the limit is the only moment it can be
// exceeded, so that is where the trim runs.
func TestHistoryKeepsOnlyTheLastSendsPerRequest(t *testing.T) {
	svc, _ := newServiceWithHistory(t)
	for i := 0; i < HistoryLimit+5; i++ {
		svc.record(HistoryEntry{
			RequestID: "req-1", Method: "GET", URL: "https://api.example/ping",
			Status: 200, SentAt: baseTime().Add(timeStep(i)),
		})
	}
	svc.record(HistoryEntry{RequestID: "req-2", Method: "GET", URL: "https://other", Status: 200, SentAt: baseTime()})

	entries, err := svc.History("req-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != HistoryLimit {
		t.Fatalf("kept %d entries, want %d", len(entries), HistoryLimit)
	}
	// Newest first, and the oldest are the ones that went.
	if !entries[0].SentAt.After(entries[len(entries)-1].SentAt) {
		t.Fatal("history is not newest first")
	}
	// Trimming one request must not touch another's.
	if others, err := svc.History("req-2", 0); err != nil || len(others) != 1 {
		t.Fatalf("another request's history was trimmed: %d %v", len(others), err)
	}

	if err := svc.ClearHistory("req-1"); err != nil {
		t.Fatal(err)
	}
	if entries, err = svc.History("req-1", 0); err != nil || len(entries) != 0 {
		t.Fatalf("clearing left %d entries: %v", len(entries), err)
	}
	if others, err := svc.History("req-2", 0); err != nil || len(others) != 1 {
		t.Fatalf("clearing one request cleared another: %d %v", len(others), err)
	}
}

// Without the table a send still goes out. A module that refused to send
// because it could not write a log would be trading the thing for the record
// of the thing.
func TestSendWorksWithNoHistoryWired(t *testing.T) {
	svc := newService(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))
	defer server.Close()

	env, err := svc.CreateEnvironment("Local", "dev", map[string]string{"baseUrl": server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.SaveRequest(Request{Name: "Ping", Method: "get", URL: "{{baseUrl}}/ping"})
	if err != nil {
		t.Fatal(err)
	}
	full, err := svc.Request(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Send(context.Background(), SendInput{Request: full, EnvironmentID: env.ID})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 204 {
		t.Fatalf("status = %d", resp.Status)
	}
	if entries, err := svc.History(saved.ID, 0); err != nil || entries != nil {
		t.Fatalf("history with no store = %v %v", entries, err)
	}
}

func baseTime() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func timeStep(i int) time.Duration { return time.Duration(i) * time.Minute }
