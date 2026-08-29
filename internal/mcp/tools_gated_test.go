package mcp

import (
	"context"
	"errors"
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
}

func (f *fakeDB) ListProfiles() ([]database.ProfileSummary, error) { return f.profiles, nil }
func (f *fakeDB) TestConnection(context.Context, string) error     { return nil }
func (f *fakeDB) AgentActiveProfileID() string                     { return f.activeID }
func (f *fakeDB) SetAgentActiveProfile(id string) error {
	f.activeID = id
	return nil
}
func (f *fakeDB) ListQueryHistory(string) ([]database.HistoryEntry, error) {
	return nil, nil
}

func (f *fakeDB) ListSavedQueries(string) ([]database.SavedQuery, error) {
	return nil, nil
}

func (f *fakeDB) Query(context.Context, string, string) (database.QueryResult, error) {
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
	summaries, ok := v.([]sshservice.ProfileSummary)
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
