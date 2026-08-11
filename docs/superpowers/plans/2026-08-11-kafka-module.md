# Kafka Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a built-in Kafka module (profiles in vaultitem, single live cluster connection, browse topics/groups, peek + publish) with Capability Gateway + UI — matching `/dev-kit/kafka` design, **without MCP** in this plan.

**Architecture:** Follow SSH/Database thin-adapter pattern: `internal/kafka` owns profile CRUD + one mutex-guarded live client; secrets in `vaultitem` (`kafka-profile` / `kafka-secret`); required `env` via `EnvLookup`; bridge methods gated `kafka:connect` / `kafka:produce`; frontend ActivityBar module `KafkaView`. Kafka client = `github.com/twmb/franz-go` (pure Go, admin + consume + produce).

**Tech Stack:** Go 1.26, `franz-go`, existing `vaultitem` / `environments` / `bridge` / React+Vite frontend, `go-task` + bun.

**Spec source:** `syurodev/base` → `content/projects/dev-kit/kafka.mdx` (design). This plan implements human/UI path only.

**Explicitly out of scope (this plan):**
- MCP tools, agent popup / `selectionRequired`, env-tag confirmation prompts
- Schema Registry / Avro / Protobuf
- Multi live connections, batch publish, consumer-group mutate (reset offset)
- ACL / mirroring UIs

**Concrete defaults (open questions from design):**
| Item | Decision |
|---|---|
| Peek default limit | `50` messages; hard max `500` |
| Peek value display | UTF-8 text if valid, else base64; JSON pretty-print when `json.Valid` |
| Broker call timeout | `30s` (same spirit as DB query timeout) |
| Client library | `github.com/twmb/franz-go` |
| TLS remote | require TLS for non-loopback when protocol is `SSL` / `SASL_SSL`; reject insecure remote PLAINTEXT (mirror ADR-007 spirit) |
| SASL | `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512` |
| Produce | `acks=all`, `AllowAutoTopicCreation=false` |

---

## File map

| Path | Responsibility |
|---|---|
| `internal/kafka/kafka.go` | Types, `Service`, profile CRUD, EnvLookup, active conn mutex |
| `internal/kafka/client.go` | `BrokerClient` interface + franz-go impl (dial/test/admin/peek/produce) |
| `internal/kafka/kafka_test.go` | Unit tests with fake broker client |
| `internal/kafka/integration_test.go` | Opt-in real broker tests (`DEVKIT_KAFKA_TEST=1`) |
| `internal/vaultitem/schema.go` | Add `ItemTypeKafka`, validate metadata + `kafka-secret` |
| `internal/bridge/kafka.go` | Bridge methods + DTOs co-located |
| `internal/bridge/errors.go` | Map kafka connection errors like ssh/db |
| `internal/app/container.go` | Wire `Kafka` service + grants |
| `internal/app/app.go` | Register module manifest |
| `internal/app/envusage.go` | Add `"kafka-profile"` to `profileTypesWithEnv` |
| `internal/arch/arch_test.go` | Ban `kafka → environments` direct import (like ssh/db) |
| `frontend/src/bridge/kafka.ts` | Typed adapters |
| `frontend/src/modules/kafka/{manifest.ts,KafkaView.tsx}` | UI module |
| `frontend/src/workbench/Workbench.tsx` | `registerModule(kafkaModule)` |
| `go.mod` | Add `franz-go` |
| base docs | Flip Kafka callout to Implemented; status matrix |

---

### Task 1: Vaultitem schema for `kafka-profile`

**Files:**
- Modify: `internal/vaultitem/schema.go`
- Modify: `internal/vaultitem/schema_test.go` (or create cases in existing test file)
- Modify: `internal/app/envusage.go` (add type to in-use scan — can wait until Task 8 if preferred; do here to avoid forgetting)

- [ ] **Step 1: Write failing schema test**

Add to `internal/vaultitem/schema_test.go` (create file if missing; otherwise extend):

```go
func TestValidateKafkaProfileRequiresEnvAndBootstrap(t *testing.T) {
	env := crypto.Envelope{Version: crypto.EnvelopeVersionV2, KeyID: "kafka-secret", Nonce: make([]byte, 24), Ciphertext: []byte("x"), Tag: make([]byte, 16)}
	// adjust Envelope fields to match real crypto.Envelope shape used by other schema tests
	meta := json.RawMessage(`{"name":"local","bootstrap_servers":["127.0.0.1:9092"],"security_protocol":"PLAINTEXT","env":"dev"}`)
	if err := ValidateRecord(ItemTypeKafka, meta, &env); err != nil {
		t.Fatalf("valid kafka profile: %v", err)
	}
	bad := json.RawMessage(`{"name":"local","bootstrap_servers":["127.0.0.1:9092"],"security_protocol":"PLAINTEXT"}`)
	if err := ValidateRecord(ItemTypeKafka, bad, &env); err == nil {
		t.Fatal("missing env must fail")
	}
}
```

Use the same `Envelope` construction pattern already used in existing vaultitem schema tests — do not invent fields.

- [ ] **Step 2: Run test — expect fail**

```bash
go test ./internal/vaultitem/ -run TestValidateKafkaProfile -count=1
```

Expected: FAIL (`ItemTypeKafka` undefined or validation rejects).

- [ ] **Step 3: Implement schema**

In `schema.go`:

```go
const (
	// existing...
	ItemTypeKafka = "kafka-profile"
)

// In switch of supported types, include ItemTypeKafka.
// In ValidateRecord switch:
case ItemTypeKafka:
	var value struct {
		Name             string   `json:"name"`
		BootstrapServers []string `json:"bootstrap_servers"`
		SecurityProtocol string   `json:"security_protocol"`
		SASLMechanism    string   `json:"sasl_mechanism"`
		Username         string   `json:"username"`
		Env              string   `json:"env"`
	}
	// require name, len(bootstrap_servers)>0, security_protocol in
	// PLAINTEXT|SSL|SASL_PLAINTEXT|SASL_SSL, env != ""
	// if SASL_* then sasl_mechanism in PLAIN|SCRAM-SHA-256|SCRAM-SHA-512 and username != ""
	expectedKeyID = "kafka-secret"
```

Also update `profileTypesWithEnv` in `internal/app/envusage.go`:

```go
var profileTypesWithEnv = []string{"ssh-profile", "db-profile", "kafka-profile"}
```

- [ ] **Step 4: Pass tests**

```bash
go test ./internal/vaultitem/ ./internal/app/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/vaultitem/schema.go internal/vaultitem/schema_test.go internal/app/envusage.go
git commit -m "feat(vaultitem): add kafka-profile schema with required env"
```

---

### Task 2: Kafka service skeleton — profiles + EnvLookup

**Files:**
- Create: `internal/kafka/kafka.go`
- Create: `internal/kafka/kafka_test.go`

- [ ] **Step 1: Write failing tests for Create/List/Delete + env rules**

```go
package kafka

import (
	"testing"

	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

type stubEnv map[string]bool

func (s stubEnv) Exists(name string) bool { return s[name] }

func newTestService(t *testing.T) *Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	store := vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	return NewService(store, stubEnv{"dev": true, "stg": true, "prod": true}, nil)
}

func TestCreateProfileRequiresEnv(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{
		Name: "local", BootstrapServers: []string{"127.0.0.1:9092"},
		SecurityProtocol: "PLAINTEXT", Secret: "x",
	}); err == nil {
		t.Fatal("missing env must error")
	}
}

func TestCreateProfileRejectsUnknownEnv(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{
		Name: "local", BootstrapServers: []string{"127.0.0.1:9092"},
		SecurityProtocol: "PLAINTEXT", Secret: "x", Env: "canary",
	}); err == nil {
		t.Fatal("unknown env must error")
	}
}

func TestCreateListDeleteProfile(t *testing.T) {
	svc := newTestService(t)
	sum, err := svc.CreateProfile(CreateProfileInput{
		Name: "local", BootstrapServers: []string{"127.0.0.1:9092"},
		SecurityProtocol: "PLAINTEXT", Secret: "pw", Env: "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	list, err := svc.ListProfiles()
	if err != nil || len(list) != 1 || list[0].ID != sum.ID || list[0].Env != "dev" {
		t.Fatalf("ListProfiles = %+v err=%v", list, err)
	}
	if err := svc.DeleteProfile(sum.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/kafka/ -count=1
```

Expected: package does not exist / undefined.

- [ ] **Step 3: Implement `kafka.go` (profiles only)**

Mirror `internal/database/database.go` shapes:

```go
package kafka

const (
	itemType    = "kafka-profile"
	secretKeyID = "kafka-secret"
)

type EnvLookup interface {
	Exists(name string) bool
}

type Store interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	Delete(id string) error
}

type CreateProfileInput struct {
	Name             string   `json:"name"`
	BootstrapServers []string `json:"bootstrap_servers"`
	SecurityProtocol string   `json:"security_protocol"`
	SASLMechanism    string   `json:"sasl_mechanism"`
	Username         string   `json:"username"`
	Secret           string   `json:"secret"` // SASL password or client key material
	Env              string   `json:"env"`
}

type ProfileSummary struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	BootstrapServers []string `json:"bootstrap_servers"`
	SecurityProtocol string   `json:"security_protocol"`
	SASLMechanism    string   `json:"sasl_mechanism"`
	Username         string   `json:"username"`
	Env              string   `json:"env"`
}

type Service struct {
	store Store
	envs  EnvLookup
	// client factory injected later (Task 3)
	dial DialFunc
	mu   sync.Mutex
	// active fields added in Task 3
}

func NewService(store Store, envs EnvLookup, dial DialFunc) *Service
func (s *Service) CreateProfile(input CreateProfileInput) (ProfileSummary, error)
func (s *Service) ListProfiles() ([]ProfileSummary, error)
func (s *Service) DeleteProfile(id string) error
```

`CreateProfile`: validate fields + `env` via `envs.Exists`; marshal metadata **without** secret; `store.Create(itemType, secretKeyID, meta, []byte(input.Secret))`.

For this task `dial` may be nil; connection methods come next.

- [ ] **Step 4: Pass profile tests**

```bash
go test ./internal/kafka/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/kafka/
git commit -m "feat(kafka): add profile CRUD with required env tag"
```

---

### Task 3: BrokerClient interface + connect lifecycle (single active)

**Files:**
- Create: `internal/kafka/client.go`
- Modify: `internal/kafka/kafka.go`
- Modify: `internal/kafka/kafka_test.go`

- [ ] **Step 1: Failing tests for single-active connect**

```go
type fakeBroker struct {
	id       string
	closed   bool
	pingErr  error
	topics   []TopicInfo
}

func (f *fakeBroker) Ping(ctx context.Context) error { return f.pingErr }
func (f *fakeBroker) Close() error                   { f.closed = true; return nil }
func (f *fakeBroker) ListTopics(ctx context.Context) ([]TopicInfo, error) {
	return f.topics, nil
}
// stub other BrokerClient methods to return errors.New("not implemented") for now

func TestConnectReplacesPreviousConnection(t *testing.T) {
	var built []string
	first := &fakeBroker{id: "A"}
	second := &fakeBroker{id: "B"}
	dial := func(ctx context.Context, cfg ConnConfig) (BrokerClient, error) {
		built = append(built, cfg.ProfileID)
		if cfg.ProfileID == built[0] || len(built) == 1 {
			return first, nil
		}
		return second, nil
	}
	// create svc with dial; create two profiles; Connect A; Connect B;
	// assert first.closed && status.ProfileID == B
}
```

Refine dial closure so first call returns `first`, second returns `second` by call count.

Also test: `ListTopics` without connect → clear error `ErrNotConnected`.

- [ ] **Step 2: Run — expect fail**

```bash
go test ./internal/kafka/ -run 'TestConnect|TestListTopicsRequires' -count=1
```

- [ ] **Step 3: Implement client interface + Service connect**

```go
type ConnConfig struct {
	ProfileID        string
	BootstrapServers []string
	SecurityProtocol string
	SASLMechanism    string
	Username         string
	Password         string // from OpenPayload; caller zeroizes
	// TLS derived from SecurityProtocol
}

type BrokerClient interface {
	Ping(ctx context.Context) error
	Close() error
	ListTopics(ctx context.Context) ([]TopicInfo, error)
	DescribeTopic(ctx context.Context, topic string) (TopicDetail, error)
	PeekMessages(ctx context.Context, topic string, partition int32, limit int, offset int64) ([]Message, error)
	ListConsumerGroups(ctx context.Context) ([]ConsumerGroupInfo, error)
	Publish(ctx context.Context, req PublishRequest) (PublishResult, error)
}

type DialFunc func(ctx context.Context, cfg ConnConfig) (BrokerClient, error)

var (
	ErrNotConnected = errors.New("kafka is not connected")
	ErrInsecureRemotePLAINTEXT = errors.New("plaintext kafka to remote host is not allowed")
)

type ConnectionStatus struct {
	Connected bool   `json:"connected"`
	ProfileID string `json:"profile_id,omitempty"`
}

func (s *Service) TestConnection(ctx context.Context, id string) error
func (s *Service) Connect(ctx context.Context, id string) error
func (s *Service) Disconnect() error
func (s *Service) Status() ConnectionStatus
```

Connect algorithm (under `s.mu`):
1. Load profile + decrypt secret
2. Build `ConnConfig`; reject remote PLAINTEXT (host not loopback)
3. `dial`
4. `Ping` with timeout
5. Close previous `active` if any
6. Set `active` + `activeProfileID`
7. Zeroize password bytes

`TestConnection`: dial+ping+close **without** touching `active`.

`DeleteProfile`: if deleting active profile, disconnect first (like SSH stops forwards).

- [ ] **Step 4: Pass tests** (still using fake dial; no franz-go yet)

```bash
go test ./internal/kafka/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/kafka/
git commit -m "feat(kafka): single-active connect lifecycle with fakeable dial"
```

---

### Task 4: franz-go dialer + admin/peek/produce

**Files:**
- Modify: `internal/kafka/client.go` (real dialer)
- Modify: `go.mod` / `go.sum`
- Create: `internal/kafka/integration_test.go`

- [ ] **Step 1: Add dependency**

```bash
go get github.com/twmb/franz-go@latest
go get github.com/twmb/franz-go/pkg/kadm@latest
go get github.com/twmb/franz-go/pkg/kmsg@latest
```

- [ ] **Step 2: Implement `DialFranz(ctx, cfg) (BrokerClient, error)`**

Map security protocols → franz-go dial opts (TLS, SASL plain/scram). Set:
- request timeouts from context
- produce: required acks all; disable auto topic creation

Implement each `BrokerClient` method via `kgo.Client` + `kadm.Client`.

Peek: resolve end offset, read last `limit` messages from partition (clamp 1..500; default 50). Offset arg: `-1` means latest-window; `>=0` means start at that offset.

Publish:

```go
type PublishRequest struct {
	Topic     string
	Key       []byte
	Value     []byte
	Headers   map[string]string
	Partition *int32 // nil = let broker/key choose
}
type PublishResult struct {
	Partition int32
	Offset    int64
	Timestamp time.Time
}
```

- [ ] **Step 3: Unit-test config validation without broker**

```go
func TestRejectRemotePLAINTEXT(t *testing.T) {
	// Connect/TestConnection with bootstrap 203.0.113.1:9092 + PLAINTEXT → ErrInsecureRemotePLAINTEXT
}
```

- [ ] **Step 4: Optional integration test skeleton**

```go
func TestIntegrationKafkaRoundTrip(t *testing.T) {
	if os.Getenv("DEVKIT_KAFKA_TEST") != "1" {
		t.Skip("set DEVKIT_KAFKA_TEST=1")
	}
	// bootstrap from DEVKIT_KAFKA_BOOTSTRAP default 127.0.0.1:9092
	// create profile, TestConnection, Connect, Publish to existing topic, Peek, assert
}
```

Do **not** require real broker in CI default.

- [ ] **Step 5: Wire default dial in `NewService`**

```go
func NewService(store Store, envs EnvLookup, dial DialFunc) *Service {
	if dial == nil {
		dial = DialFranz
	}
	return &Service{store: store, envs: envs, dial: dial}
}
```

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/kafka/
git commit -m "feat(kafka): franz-go dialer with admin peek and produce"
```

---

### Task 5: Remaining service ops + atomic switch cancel semantics

**Files:**
- Modify: `internal/kafka/kafka.go`
- Modify: `internal/kafka/kafka_test.go`

- [ ] **Step 1: Tests**

```go
func TestOpsRequireConnection(t *testing.T) { /* ListTopics/Describe/Peek/Groups/Publish → ErrNotConnected */ }

func TestListTopicsUsesActiveClient(t *testing.T) {
	// fake with topics; Connect; ListTopics returns them
}

func TestPublishClampsAndForwards(t *testing.T) {
	// fake records last PublishRequest; assert topic/value
}
```

- [ ] **Step 2: Implement service wrappers**

```go
func (s *Service) ListTopics(ctx context.Context) ([]TopicInfo, error)
func (s *Service) DescribeTopic(ctx context.Context, topic string) (TopicDetail, error)
func (s *Service) PeekMessages(ctx context.Context, topic string, partition int32, limit int, offset int64) ([]Message, error)
func (s *Service) ListConsumerGroups(ctx context.Context) ([]ConsumerGroupInfo, error)
func (s *Service) PublishMessage(ctx context.Context, req PublishRequest) (PublishResult, error)
func (s *Service) Close() // Disconnect; for app shutdown
```

Each op: `context.WithTimeout(ctx, 30*time.Second)`; hold `RLock` or carefully take client pointer under mutex so Connect can close mid-flight → return `context.Canceled` / wrapped cancelled error (NFR-8). Prefer: copy `client` pointer under lock, release lock, call client; if client closed, surface clear error.

- [ ] **Step 3: Pass**

```bash
go test ./internal/kafka/ -count=1
```

- [ ] **Step 4: Commit**

```bash
git add internal/kafka/
git commit -m "feat(kafka): topic peek group and publish service APIs"
```

---

### Task 6: Bridge + permissions + module manifest

**Files:**
- Create: `internal/bridge/kafka.go`
- Modify: `internal/bridge/errors.go`
- Modify: `internal/app/container.go`
- Modify: `internal/app/app.go`
- Modify: `internal/arch/arch_test.go`
- Test: `internal/bridge/` (new `kafka_test.go` or extend gateway tests)

- [ ] **Step 1: Arch ban**

In `TestLayering` bans map, add:

```go
pkg("kafka"): {pkg("environments")},
// and add kafka to environments' forbidden reverse imports list alongside ssh/database
pkg("environments"): {
	pkg("ssh"), pkg("database"), pkg("kafka"), pkg("bridge"), pkg("app"),
},
```

Also add `kafka` to vaultitem forbidden consumers list if that list enumerates domain modules.

- [ ] **Step 2: Container grants + wire**

```go
{SubjectID: "builtin.kafka", Capability: "kafka:connect", Scope: "profile:list"},
{SubjectID: "builtin.kafka", Capability: "kafka:connect", Scope: "profile:new"},
{SubjectID: "builtin.kafka", Capability: "kafka:connect", Scope: "profile:*"},
{SubjectID: "builtin.kafka", Capability: "kafka:connect", Scope: "connection"},
{SubjectID: "builtin.kafka", Capability: "kafka:produce", Scope: "connection"},
```

```go
Kafka: kafka.NewService(itemStore, envService, nil),
```

Add `Kafka *kafka.Service` on `Container`; `Close()` path in main/app shutdown should call `container.Kafka.Close()` if present (follow SSH/DB Close pattern).

- [ ] **Step 3: Register manifest**

```go
{
	ID: "kafka", Title: "Kafka", Version: "1.0.0",
	APIVersion: modules.CurrentAPIVersion,
	Commands: []string{"kafka.publish"},
	Views: []string{"kafka.explorer"},
	Capabilities: []string{"kafka:connect", "kafka:produce"},
},
```

- [ ] **Step 4: Bridge methods** (co-locate DTOs in `kafka.go`)

| Method | Cap | Scope |
|---|---|---|
| `KafkaListProfiles` | `kafka:connect` | `profile:list` |
| `KafkaCreateProfile` | `kafka:connect` | `profile:new` |
| `KafkaDeleteProfile` | `kafka:connect` | `profile:<id>` |
| `KafkaTestConnection` | `kafka:connect` | `profile:<id>` |
| `KafkaConnect` | `kafka:connect` | `profile:<id>` |
| `KafkaDisconnect` | `kafka:connect` | `connection` |
| `KafkaConnectionStatus` | `kafka:connect` | `connection` |
| `KafkaListTopics` | `kafka:connect` | `connection` |
| `KafkaDescribeTopic` | `kafka:connect` | `connection` |
| `KafkaPeekMessages` | `kafka:connect` | `connection` |
| `KafkaListConsumerGroups` | `kafka:connect` | `connection` |
| `KafkaPublishMessage` | `kafka:produce` | `connection` |

Subject always `builtin.kafka`. Use `recordID()` for profile scopes.

Map `kafka.ErrNotConnected` / insecure plaintext → `connection_failed` in `publicError` for `builtin.kafka` (same branch as ssh/db is fine, or explicit `errors.Is`).

- [ ] **Step 5: Bridge test** — list/create via AppService with memory container; connect with fake dial injected **only if** container allows override. If injection is hard, test permission grant presence + CreateProfile path only; rely on kafka package tests for connect.

Minimal bridge test:

```go
func TestKafkaBridgeCreateAndList(t *testing.T) {
	service := newTestAppService(t) // existing helper from bridge tests
	// unlock vault if needed
	// KafkaCreateProfile(... Env: "dev")
	// KafkaListProfiles len 1
}
```

- [ ] **Step 6: Commit**

```bash
git add internal/bridge/kafka.go internal/bridge/errors.go internal/app/ internal/arch/
git commit -m "feat(kafka): bridge methods, grants, and module manifest"
```

---

### Task 7: Frontend bridge + KafkaView UI

**Files:**
- Create: `frontend/src/bridge/kafka.ts`
- Create: `frontend/src/modules/kafka/manifest.ts`
- Create: `frontend/src/modules/kafka/KafkaView.tsx`
- Modify: `frontend/src/workbench/Workbench.tsx` (register module)
- Regenerate bindings: run whatever the repo uses (`task bindings` / `wails3 generate bindings`) after bridge exists — follow `Taskfile` / AGENTS.md

- [ ] **Step 1: `kafka.ts` adapters**

Mirror `frontend/src/bridge/ssh.ts` style: snake_case DTO → camelCase UI types; wrap `AppService.Kafka*`.

- [ ] **Step 2: `manifest.ts`**

```ts
export const kafkaModule: ModuleManifest = {
  id: "kafka",
  title: "Kafka",
  version: "1.0.0",
  apiVersion: CURRENT_MODULE_API_VERSION,
  commands: [{ id: "kafka.publish", title: "Publish Kafka message" }],
  views: [{ id: "kafka.explorer", title: "Kafka", component: KafkaView }],
  capabilities: ["kafka:connect", "kafka:produce"],
};
```

- [ ] **Step 3: `KafkaView.tsx` MVP layout** (Notes/SSH-shaped, not a dashboard)

Sections in one scrollable view:
1. **Create profile** — name, bootstrap (comma-separated → array), security protocol select, SASL fields when needed, secret, env dropdown (`listEnvTags`)
2. **Profiles** — select, Test, Connect, Disconnect, Delete; show `connectionStatus`
3. **Cluster** (enabled when connected) — List topics table; select topic → Describe; partition + Peek; consumer groups list
4. **Publish** — topic, key, payload textarea, optional partition; show returned partition/offset

No MCP popup. No multi-cluster tabs.

- [ ] **Step 4: Register in Workbench** next to ssh/database.

- [ ] **Step 5: Build**

```bash
cd frontend && bun run build
```

Fix parity errors if backend manifest fields disagree.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/bridge/kafka.ts frontend/src/modules/kafka frontend/src/workbench/Workbench.tsx frontend/bindings
git commit -m "feat(frontend): add Kafka module UI and bridge adapters"
```

---

### Task 8: Docs + status sync (base)

**Files (repo `syurodev/base`):**
- Modify: `content/projects/dev-kit/kafka.mdx` — replace Design-only callout with Implemented (UI path); keep MCP sections marked design
- Modify: `content/projects/dev-kit/implementation-status.mdx` — add Kafka row Partial/Implemented
- Modify: `content/projects/dev-kit/technical-contracts.mdx` — bridge family row + vaultitem metadata
- Modify: `content/projects/dev-kit/repo-structure.mdx` — `kafka/` line
- Modify: `content/projects/dev-kit/environments.mdx` — mention kafka-profile in DeleteTag scan / consumers

- [ ] **Step 1: Edit docs to match shipped surface** (no MCP claims as Implemented)
- [ ] **Step 2: Push `base` main** (same PAT workflow as prior doc updates)
- [ ] **Step 3: Optionally note in `dev-kit` README Current layout**

```bash
# in base
git commit -m "docs(dev-kit): mark Kafka UI path implemented; MCP still design"
git push origin main
```

---

### Task 9: Verification gate

- [ ] **Step 1: Run targeted tests**

```bash
go test ./internal/kafka/ ./internal/vaultitem/ ./internal/app/ ./internal/bridge/ ./internal/arch/ ./internal/environments/ -count=1
go vet -tags server ./internal/kafka/ ./internal/bridge/ ./internal/app/
cd frontend && bun run build
```

- [ ] **Step 2: Manual hello-world (server mode)**

1. `task run:server` (or build tags server binary per AGENTS.md)
2. Unlock vault → open Kafka
3. Create PLAINTEXT profile to local broker (or skip if no broker; use TestConnection failure path)
4. If broker available: Connect → ListTopics → Publish → Peek

- [ ] **Step 3: Final commit / PR** if not already on feature branch merged

```bash
git commit --allow-empty -m "chore(kafka): verification gate green"
```

---

## Spec coverage checklist

| Spec item | Task |
|---|---|
| FR-1 profiles + required env | 1, 2 |
| FR-2 single live connection | 3 |
| FR-3 list topics | 4, 5 |
| FR-4 peek | 4, 5 |
| FR-5 consumer groups + lag | 4, 5 |
| FR-6 test connection temp | 3 |
| FR-7 publish acks=all, no auto-create | 4, 5 |
| FR-8/FR-9 MCP popup | **Deferred** (MCP plan) |
| NFR-1 secrets in vaultitem | 1, 2 |
| NFR-2 TLS/PLAINTEXT policy | 3, 4 |
| NFR-3 timeouts | 5 |
| NFR-4 peek limits | 4, 5 |
| NFR-5 pure Go client | 4 |
| NFR-6/7 produce settings | 4 |
| NFR-8 atomic switch | 3, 5 |
| NFR-9 headless popup | **Deferred** |
| UI ActivityBar module | 7 |
| DeleteTag in-use includes kafka | 1 (`envusage`) |
| Docs | 8 |

---

## Follow-ups (not this plan)

1. MCP tool surface + select-cluster popup + env confirmation (`/dev-kit/mcp-agent`)
2. Schema Registry / Avro
3. Richer Kafka UX (multi-partition peek controls, message search)
