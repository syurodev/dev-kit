package kafka

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

type stubEnvLookup map[string]bool

func (s stubEnvLookup) Exists(name string) bool { return s[name] }

var testEnvs = stubEnvLookup{"dev": true, "stg": true, "prod": true}

func newService(t *testing.T) *Service {
	t.Helper()
	return newServiceWithDial(t, nil)
}

func newServiceWithDial(t *testing.T, dial DialFunc) *Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v), testEnvs, dial)
}

type fakeBroker struct {
	id            int
	closed        bool
	topics        []TopicInfo
	pingErr       error
	listTopicsErr error
}

func (f *fakeBroker) Ping(ctx context.Context) error { return f.pingErr }
func (f *fakeBroker) Close() error                   { f.closed = true; return nil }
func (f *fakeBroker) ListTopics(ctx context.Context) ([]TopicInfo, error) {
	if f.listTopicsErr != nil {
		return nil, f.listTopicsErr
	}
	return f.topics, nil
}
func (f *fakeBroker) DescribeTopic(ctx context.Context, topic string) (TopicDetail, error) {
	return TopicDetail{Name: topic}, nil
}
func (f *fakeBroker) PeekMessages(ctx context.Context, topic string, partition int32, limit int, offset int64) ([]Message, error) {
	return nil, nil
}
func (f *fakeBroker) ListConsumerGroups(ctx context.Context) ([]ConsumerGroupInfo, error) {
	return nil, nil
}
func (f *fakeBroker) Publish(ctx context.Context, req PublishRequest) (PublishResult, error) {
	return PublishResult{}, nil
}

func createPlaintextProfile(t *testing.T, svc *Service, name, bootstrap string) ProfileSummary {
	t.Helper()
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:             name,
		BootstrapServers: []string{bootstrap},
		SecurityProtocol: "PLAINTEXT",
		Secret:           "secret",
		Env:              "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile(%s): %v", name, err)
	}
	return summary
}

func TestCreateProfileRequiresEnv(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{
		Name:             "events",
		BootstrapServers: []string{"127.0.0.1:9092"},
		SecurityProtocol: "PLAINTEXT",
	}); err == nil {
		t.Fatal("missing env must error")
	}
}

func TestCreateProfileRejectsUnknownEnv(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{
		Name:             "events",
		BootstrapServers: []string{"127.0.0.1:9092"},
		SecurityProtocol: "PLAINTEXT",
		Env:              "canary",
	}); err == nil {
		t.Fatal("unknown env must error")
	}
}

func TestCreateListDeleteProfile(t *testing.T) {
	svc := newService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:             "events",
		BootstrapServers: []string{"127.0.0.1:9092"},
		SecurityProtocol: "PLAINTEXT",
		Secret:           "secret",
		Env:              "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if summary.Name != "events" {
		t.Fatalf("Name = %q, want events", summary.Name)
	}
	if len(summary.BootstrapServers) != 1 || summary.BootstrapServers[0] != "127.0.0.1:9092" {
		t.Fatalf("BootstrapServers = %v", summary.BootstrapServers)
	}
	if summary.Env != "dev" {
		t.Fatalf("Env = %q, want dev", summary.Env)
	}

	list, err := svc.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].ID != summary.ID {
		t.Fatalf("list ID = %q, want %q", list[0].ID, summary.ID)
	}

	if err := svc.DeleteProfile(summary.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	list, err = svc.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("len(list) after delete = %d, want 0", len(list))
	}
}

func TestConnectReplacesPreviousConnection(t *testing.T) {
	ctx := context.Background()
	var brokers []*fakeBroker
	dial := func(ctx context.Context, cfg ConnConfig) (BrokerClient, error) {
		b := &fakeBroker{id: len(brokers), topics: []TopicInfo{{Name: cfg.ProfileID}}}
		brokers = append(brokers, b)
		return b, nil
	}
	svc := newServiceWithDial(t, dial)
	p1 := createPlaintextProfile(t, svc, "local-a", "127.0.0.1:9092")
	p2 := createPlaintextProfile(t, svc, "local-b", "localhost:9092")

	if err := svc.Connect(ctx, p1.ID); err != nil {
		t.Fatalf("Connect p1: %v", err)
	}
	if st := svc.Status(); !st.Connected || st.ProfileID != p1.ID {
		t.Fatalf("Status after p1 = %+v, want connected %s", st, p1.ID)
	}

	if err := svc.Connect(ctx, p2.ID); err != nil {
		t.Fatalf("Connect p2: %v", err)
	}
	if st := svc.Status(); !st.Connected || st.ProfileID != p2.ID {
		t.Fatalf("Status after p2 = %+v, want connected %s", st, p2.ID)
	}
	if len(brokers) != 2 {
		t.Fatalf("dial count = %d, want 2", len(brokers))
	}
	if !brokers[0].closed {
		t.Fatal("first broker must be closed after second Connect")
	}
	if brokers[1].closed {
		t.Fatal("active broker must not be closed")
	}
}

func TestListTopicsRequiresConnection(t *testing.T) {
	ctx := context.Background()
	svc := newServiceWithDial(t, func(ctx context.Context, cfg ConnConfig) (BrokerClient, error) {
		return &fakeBroker{topics: []TopicInfo{{Name: "events"}}}, nil
	})
	if _, err := svc.ListTopics(ctx); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("ListTopics without connect = %v, want ErrNotConnected", err)
	}

	p := createPlaintextProfile(t, svc, "local", "127.0.0.1:9092")
	if err := svc.Connect(ctx, p.ID); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	topics, err := svc.ListTopics(ctx)
	if err != nil {
		t.Fatalf("ListTopics: %v", err)
	}
	if len(topics) != 1 || topics[0].Name != "events" {
		t.Fatalf("topics = %v, want [events]", topics)
	}
}

func TestRejectRemotePLAINTEXT(t *testing.T) {
	ctx := context.Background()
	dial := func(ctx context.Context, cfg ConnConfig) (BrokerClient, error) {
		t.Fatal("dial must not be called for remote PLAINTEXT")
		return nil, nil
	}
	svc := newServiceWithDial(t, dial)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:             "remote",
		BootstrapServers: []string{"203.0.113.1:9092"},
		SecurityProtocol: "PLAINTEXT",
		Secret:           "secret",
		Env:              "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	if err := svc.TestConnection(ctx, summary.ID); !errors.Is(err, ErrInsecureRemotePLAINTEXT) {
		t.Fatalf("TestConnection error = %v, want ErrInsecureRemotePLAINTEXT", err)
	}
	if err := svc.Connect(ctx, summary.ID); !errors.Is(err, ErrInsecureRemotePLAINTEXT) {
		t.Fatalf("Connect error = %v, want ErrInsecureRemotePLAINTEXT", err)
	}
	if st := svc.Status(); st.Connected {
		t.Fatalf("Status = %+v, want not connected", st)
	}
}
