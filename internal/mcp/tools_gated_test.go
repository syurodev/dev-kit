package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"devkit/internal/apperrors"
	"devkit/internal/database"
	"devkit/internal/interaction"
	"devkit/internal/kafka"
	sshservice "devkit/internal/ssh"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeSSH struct {
	profiles []sshservice.ProfileSummary
	activeID string
	runOut   sshservice.CommandResult
	runErr   error
}

func (f *fakeSSH) ListProfiles() ([]sshservice.ProfileSummary, error)   { return f.profiles, nil }
func (f *fakeSSH) TestConnection(context.Context, string, string) error { return nil }
func (f *fakeSSH) AgentActiveProfileID() string                         { return f.activeID }
func (f *fakeSSH) SetAgentActiveProfile(id string) error {
	f.activeID = id
	return nil
}
func (f *fakeSSH) RunCommand(context.Context, string, string, string) (sshservice.CommandResult, error) {
	return f.runOut, f.runErr
}

type fakeDB struct {
	profiles []database.ProfileSummary
	activeID string
	queryOut database.QueryResult
	queryErr error
	// queriedID is the profile Query ran on; onQuery runs inside Query.
	queriedID string
	onQuery   func(f *fakeDB)
}

func (f *fakeDB) ListProfiles() ([]database.ProfileSummary, error) { return f.profiles, nil }
func (f *fakeDB) TestConnection(context.Context, string) error     { return nil }
func (f *fakeDB) AgentActiveProfileID() string                     { return f.activeID }
func (f *fakeDB) SetAgentActiveProfile(id string) error {
	f.activeID = id
	return nil
}
func (f *fakeDB) ListForeignKeys(context.Context, string, string) ([]database.ForeignKey, error) {
	return nil, nil
}

func (f *fakeDB) GetSchemaCache(context.Context, string) (database.SchemaSnapshot, error) {
	return database.SchemaSnapshot{}, nil
}

func (f *fakeDB) Explain(context.Context, string, string) (database.QueryPlan, error) {
	return database.QueryPlan{}, nil
}

func (f *fakeDB) ListQueryHistory(string) ([]database.HistoryEntry, error) {
	return nil, nil
}

func (f *fakeDB) ListSavedQueries(string) ([]database.SavedQuery, error) {
	return nil, nil
}

func (f *fakeDB) Query(_ context.Context, id string, _ string, _ int) (database.QueryResult, error) {
	f.queriedID = id
	if f.onQuery != nil {
		f.onQuery(f)
	}
	return f.queryOut, f.queryErr
}

type fakeKafka struct {
	profiles  []kafka.ProfileSummary
	status    kafka.ConnectionStatus
	connectID string
}

func (f *fakeKafka) ListProfiles() ([]kafka.ProfileSummary, error) { return f.profiles, nil }
func (f *fakeKafka) Status() kafka.ConnectionStatus                { return f.status }
func (f *fakeKafka) Connect(_ context.Context, id string) error {
	f.connectID = id
	f.status = kafka.ConnectionStatus{Connected: true, ProfileID: id}
	return nil
}
func (f *fakeKafka) ListTopics(context.Context) ([]kafka.TopicInfo, error) { return nil, nil }
func (f *fakeKafka) DescribeTopic(context.Context, string) (kafka.TopicDetail, error) {
	return kafka.TopicDetail{}, nil
}
func (f *fakeKafka) PeekMessages(context.Context, string, int32, int, int64) ([]kafka.Message, error) {
	return nil, nil
}
func (f *fakeKafka) ListConsumerGroups(context.Context) ([]kafka.ConsumerGroupInfo, error) {
	return nil, nil
}
func (f *fakeKafka) PublishMessage(context.Context, kafka.PublishRequest) (kafka.PublishResult, error) {
	return kafka.PublishResult{}, nil
}

func TestMapInteractErr(t *testing.T) {
	cases := []struct {
		in   error
		code string
		msg  string
	}{
		{interaction.ErrInteractionTimeout, "operation_cancelled", "Operation cancelled."},
		{interaction.ErrConfirmationDenied, "permission_denied", "Permission denied."},
		{interaction.ErrSelectionRequired, "validation_failed", "Selection required."},
		{interaction.ErrConfirmationRequired, "validation_failed", "Confirmation required."},
	}
	for _, tc := range cases {
		err := mapInteractErr(tc.in)
		var app *apperrors.AppError
		if !errors.As(err, &app) || app.Code != tc.code || app.Message != tc.msg {
			t.Fatalf("mapInteractErr(%v) = %+v, want %s %q", tc.in, err, tc.code, tc.msg)
		}
	}
	if mapInteractErr(errors.New("other")) == nil {
		t.Fatal("expected passthrough")
	}
}

func TestSSHListProfilesSubjectCapScope(t *testing.T) {
	caller := &fakeCaller{}
	sshAPI := &fakeSSH{profiles: []sshservice.ProfileSummary{{ID: "p1", Name: "bastion", Env: "dev"}}}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{},
		SSH:    sshAPI,
		DB:     &fakeDB{},
		Kafka:  &fakeKafka{},
	})

	_, v, err := srv.toolSSHListProfiles(context.Background(), &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatalf("ssh.listProfiles: %v", err)
	}
	summaries, ok := unwrap(v).([]sshservice.ProfileSummary)
	if !ok || len(summaries) != 1 || summaries[0].Name != "bastion" {
		t.Fatalf("output = %#v", v)
	}
	if caller.last.subject != "builtin.ssh" || caller.last.capability != "ssh:connect" || caller.last.scope != "profile:list" {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["origin"] != OriginMCP || caller.last.metadata["action"] != "list_profiles" {
		t.Fatalf("meta = %v", caller.last.metadata)
	}
}

func TestDBListProfilesSubjectCapScope(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{},
		SSH:    &fakeSSH{},
		DB:     &fakeDB{profiles: []database.ProfileSummary{{ID: "d1", Name: "primary", Env: "dev"}}},
		Kafka:  &fakeKafka{},
	})

	_, _, err := srv.toolDBListProfiles(context.Background(), &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatalf("db.listProfiles: %v", err)
	}
	if caller.last.subject != "builtin.database" || caller.last.capability != "db:query" || caller.last.scope != "profile:list" {
		t.Fatalf("Call = %+v", caller.last)
	}
}

func TestKafkaListProfilesSubjectCapScope(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{},
		SSH:    &fakeSSH{},
		DB:     &fakeDB{},
		Kafka:  &fakeKafka{profiles: []kafka.ProfileSummary{{ID: "k1", Name: "events", Env: "dev"}}},
	})

	_, _, err := srv.toolKafkaListProfiles(context.Background(), &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatalf("kafka.listProfiles: %v", err)
	}
	if caller.last.subject != "builtin.kafka" || caller.last.capability != "kafka:connect" || caller.last.scope != "profile:list" {
		t.Fatalf("Call = %+v", caller.last)
	}
}

func TestSSHRunCommandHeadlessSelectionRequired(t *testing.T) {
	hub := &interaction.Hub{Headless: true}
	caller := &fakeCaller{}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{req: map[string]bool{"dev": false}},
		SSH:    &fakeSSH{}, // no active profile
		DB:     &fakeDB{},
		Kafka:  &fakeKafka{},
		Hub:    hub,
	})

	_, _, err := srv.toolSSHRunCommand(context.Background(), &sdk.CallToolRequest{}, sshRunCommandInput{Command: "ls"})
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Code != "validation_failed" || app.Message != "Selection required." {
		t.Fatalf("err = %v, want Selection required", err)
	}
	if caller.last.subject != "" {
		t.Fatalf("Caller should not run when gate fails, got %+v", caller.last)
	}
}

func TestSSHRunCommandHeadlessConfirmationRequired(t *testing.T) {
	hub := &interaction.Hub{Headless: true}
	caller := &fakeCaller{}
	sshAPI := &fakeSSH{
		activeID: "0123456789abcdef01234567",
		profiles: []sshservice.ProfileSummary{{ID: "0123456789abcdef01234567", Env: "dev"}},
	}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{req: map[string]bool{"dev": false}},
		SSH:    sshAPI,
		DB:     &fakeDB{},
		Kafka:  &fakeKafka{},
		Hub:    hub,
	})

	_, _, err := srv.toolSSHRunCommand(context.Background(), &sdk.CallToolRequest{}, sshRunCommandInput{Command: "ls"})
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Code != "validation_failed" || app.Message != "Confirmation required." {
		t.Fatalf("err = %v, want Confirmation required", err)
	}
}

func TestDBQueryGateDeny(t *testing.T) {
	hub := interaction.NewHub(interaction.NoopPrompter{})
	caller := &fakeCaller{}
	dbAPI := &fakeDB{
		activeID: "0123456789abcdef01234567",
		profiles: []database.ProfileSummary{{ID: "0123456789abcdef01234567", Env: "prod"}},
	}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{req: map[string]bool{"prod": true}},
		SSH:    &fakeSSH{},
		DB:     dbAPI,
		Kafka:  &fakeKafka{},
		Hub:    hub,
	})

	go func() {
		waitForPending(t, hub)
		p := hub.Pending()
		if p == nil {
			t.Error("expected pending confirmation")
			return
		}
		_ = hub.Complete(p.ID, "deny")
	}()

	_, _, err := srv.toolDBQuery(context.Background(), &sdk.CallToolRequest{}, dbQueryInput{SQL: "select 1"})
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Code != "permission_denied" {
		t.Fatalf("err = %v, want permission_denied", err)
	}
	if caller.last.subject != "" {
		t.Fatalf("Caller should not run on deny, got %+v", caller.last)
	}
}

const prodDBID = "0123456789abcdef01234567"

// A db.query result names the profile it ran on — the one the gate confirmed,
// not the sticky target read again afterwards — so a transcript card can say
// which connection answered and open its result there. The console's own
// QueryResult (the bridge path) does not grow the field.
func TestDBQueryResultNamesTheProfileItRanOn(t *testing.T) {
	const otherID = "fedcba9876543210fedcba98"
	cell := "1"
	dbAPI := &fakeDB{
		activeID: prodDBID,
		profiles: []database.ProfileSummary{{ID: prodDBID, Env: "dev"}, {ID: otherID, Env: "dev"}},
		queryOut: database.QueryResult{
			Columns: []string{"n"}, Rows: [][]*string{{&cell}}, RowCount: 1, Truncated: true, RowsAffected: -1,
		},
		// The person picks another connection while the statement runs.
		onQuery: func(f *fakeDB) { f.activeID = otherID },
	}
	hub := interaction.NewHub(interaction.NoopPrompter{})
	caller := &fakeCaller{}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{req: map[string]bool{"dev": false}},
		SSH:    &fakeSSH{},
		DB:     dbAPI,
		Kafka:  &fakeKafka{},
		Hub:    hub,
	})

	go func() {
		waitForPending(t, hub)
		_ = hub.Complete(hub.Pending().ID, "allow")
	}()
	_, out, err := srv.toolDBQuery(context.Background(), &sdk.CallToolRequest{}, dbQueryInput{SQL: "select 1"})
	if err != nil {
		t.Fatal(err)
	}
	if dbAPI.queriedID != prodDBID {
		t.Fatalf("ran on %q, want %q", dbAPI.queriedID, prodDBID)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["profile_id"] != prodDBID {
		t.Fatalf("profile_id = %v, want %q (result %s)", got["profile_id"], prodDBID, raw)
	}
	for _, key := range []string{"columns", "rows", "row_count", "truncated", "rows_affected"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("result lost %q: %s", key, raw)
		}
	}
	if got["row_count"] != float64(1) || got["truncated"] != true || got["rows_affected"] != float64(-1) {
		t.Fatalf("result fields changed: %s", raw)
	}
	// Audit metadata keeps the id opaque, as before.
	if caller.last.metadata["profile_id"] != recordID(prodDBID) || caller.last.scope != "profile:active" {
		t.Fatalf("audit = %+v", caller.last)
	}

	// The console path's type is untouched: no profile_id on the bridge's result.
	console, _ := json.Marshal(database.QueryResult{})
	if strings.Contains(string(console), "profile_id") {
		t.Fatalf("console QueryResult grew profile_id: %s", console)
	}
}

func newDBGateServer(env string, req map[string]bool, hub *interaction.Hub) (*Server, *fakeCaller) {
	caller := &fakeCaller{}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{req: req},
		SSH:    &fakeSSH{},
		DB:     &fakeDB{profiles: []database.ProfileSummary{{ID: prodDBID, Env: env}}},
		Kafka:  &fakeKafka{},
		Hub:    hub,
	})
	return srv, caller
}

// dbProfileTools runs each profile-scoped database tool the gate now covers.
func dbProfileTools(srv *Server) map[string]func() error {
	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	return map[string]func() error{
		"db.testConnection": func() error {
			_, _, err := srv.toolDBTestConnection(ctx, req, dbTestConnectionInput{ProfileID: prodDBID})
			return err
		},
		"db.listForeignKeys": func() error {
			_, _, err := srv.toolDBListForeignKeys(ctx, req, dbSchemaScopedInput{ProfileID: prodDBID, Schema: "public"})
			return err
		},
		"db.explain": func() error {
			_, _, err := srv.toolDBExplain(ctx, req, dbExplainInput{ProfileID: prodDBID, SQL: "select 1"})
			return err
		},
	}
}

func TestDBProfileToolsProdHeadlessRefused(t *testing.T) {
	srv, caller := newDBGateServer("prod", map[string]bool{"prod": true}, &interaction.Hub{Headless: true})
	for name, run := range dbProfileTools(srv) {
		err := run()
		var app *apperrors.AppError
		if !errors.As(err, &app) || app.Message != "Confirmation required." {
			t.Fatalf("%s: err = %v, want Confirmation required", name, err)
		}
		if caller.last.subject != "" {
			t.Fatalf("%s: reached the service without confirmation: %+v", name, caller.last)
		}
	}
}

func TestDBProfileToolsProdDenied(t *testing.T) {
	hub := interaction.NewHub(interaction.NoopPrompter{})
	srv, caller := newDBGateServer("prod", map[string]bool{"prod": true}, hub)
	for name, run := range dbProfileTools(srv) {
		go func() {
			waitForPending(t, hub)
			_ = hub.Complete(hub.Pending().ID, "deny")
		}()
		err := run()
		var app *apperrors.AppError
		if !errors.As(err, &app) || app.Code != "permission_denied" {
			t.Fatalf("%s: err = %v, want permission_denied", name, err)
		}
		if caller.last.subject != "" {
			t.Fatalf("%s: reached the service after deny: %+v", name, caller.last)
		}
	}
}

func TestDBProfileToolsProdAllowedReachService(t *testing.T) {
	hub := interaction.NewHub(interaction.NoopPrompter{})
	srv, caller := newDBGateServer("prod", map[string]bool{"prod": true}, hub)
	for name, run := range dbProfileTools(srv) {
		caller.last = callRecord{}
		go func() {
			waitForPending(t, hub)
			_ = hub.Complete(hub.Pending().ID, "allow")
		}()
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if caller.last.subject != "builtin.database" {
			t.Fatalf("%s: service not reached after allow: %+v", name, caller.last)
		}
	}
}

func TestDBProfileToolsDevDoNotPrompt(t *testing.T) {
	hub := interaction.NewHub(interaction.NoopPrompter{})
	srv, caller := newDBGateServer("dev", map[string]bool{"dev": false}, hub)
	for name, run := range dbProfileTools(srv) {
		caller.last = callRecord{}
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if hub.Pending() != nil {
			t.Fatalf("%s: dev must not prompt", name)
		}
		if caller.last.subject != "builtin.database" {
			t.Fatalf("%s: service not reached: %+v", name, caller.last)
		}
	}
}

func newSSHGateServer(env string, req map[string]bool, hub *interaction.Hub) (*Server, *fakeCaller) {
	caller := &fakeCaller{}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{req: req},
		SSH:    &fakeSSH{profiles: []sshservice.ProfileSummary{{ID: prodDBID, Env: env}}},
		DB:     &fakeDB{},
		Kafka:  &fakeKafka{},
		Hub:    hub,
	})
	return srv, caller
}

func TestSSHTestConnectionProdHeadlessRefused(t *testing.T) {
	srv, caller := newSSHGateServer("prod", map[string]bool{"prod": true}, &interaction.Hub{Headless: true})
	_, _, err := srv.toolSSHTestConnection(context.Background(), &sdk.CallToolRequest{}, sshTestConnectionInput{ProfileID: prodDBID})
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Message != "Confirmation required." {
		t.Fatalf("err = %v, want Confirmation required", err)
	}
	if caller.last.subject != "" {
		t.Fatalf("reached the service without confirmation: %+v", caller.last)
	}
}

func TestSSHTestConnectionDevDoesNotPrompt(t *testing.T) {
	hub := interaction.NewHub(interaction.NoopPrompter{})
	srv, caller := newSSHGateServer("dev", map[string]bool{"dev": false}, hub)
	if _, _, err := srv.toolSSHTestConnection(context.Background(), &sdk.CallToolRequest{}, sshTestConnectionInput{ProfileID: prodDBID}); err != nil {
		t.Fatalf("dev: %v", err)
	}
	if hub.Pending() != nil || caller.last.subject != "builtin.ssh" {
		t.Fatalf("dev must pass without a prompt: pending=%v call=%+v", hub.Pending(), caller.last)
	}
}

// db.schema.get indexes the live database on a cache miss, so it is gated
// like the other profile-scoped database tools.
func TestDBSchemaGetProdHeadlessRefused(t *testing.T) {
	srv, caller := newDBGateServer("prod", map[string]bool{"prod": true}, &interaction.Hub{Headless: true})
	_, _, err := srv.toolDBGetSchemaCache(context.Background(), &sdk.CallToolRequest{}, dbProfileScopedInput{ProfileID: prodDBID})
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Message != "Confirmation required." {
		t.Fatalf("err = %v, want Confirmation required", err)
	}
	if caller.last.subject != "" {
		t.Fatalf("reached the service without confirmation: %+v", caller.last)
	}
}

func TestDBSchemaGetDevDoesNotPrompt(t *testing.T) {
	hub := interaction.NewHub(interaction.NoopPrompter{})
	srv, caller := newDBGateServer("dev", map[string]bool{"dev": false}, hub)
	if _, _, err := srv.toolDBGetSchemaCache(context.Background(), &sdk.CallToolRequest{}, dbProfileScopedInput{ProfileID: prodDBID}); err != nil {
		t.Fatalf("dev: %v", err)
	}
	if hub.Pending() != nil || caller.last.subject != "builtin.database" {
		t.Fatalf("dev must pass without a prompt: pending=%v call=%+v", hub.Pending(), caller.last)
	}
}
