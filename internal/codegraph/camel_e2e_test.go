package codegraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This is the shipped Java subprocess -> validated SQLite index -> Go query
// path, not an extractor mock. The shared fixture contains no live services.
func TestCamelJavaIndexAndFlows(t *testing.T) {
	toolchain := builtJavaAnalyzer(t)
	project, err := filepath.Abs("../../analyzer/src/test/resources/camel-project")
	if err != nil {
		t.Fatal(err)
	}
	config := t.TempDir()
	svc := newTestService(t, toolchain.Analyzer())
	svc.SetConfigDir(config)
	db := ProjectDBPath(config, project)
	if err := os.MkdirAll(filepath.Dir(db), 0700); err != nil {
		t.Fatal(err)
	}
	if err := svc.OpenProjectAt(t.Context(), db, project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	entries, err := svc.ListEntryPoints(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Fatalf("entries = %d, want 19 Camel templates + 1 HTTP: %+v", len(entries), entries)
	}
	expected := map[string]string{
		"webhook-dispatch-{domain.name()}": "fixture.Business.persistWebhook",
		"fanout-{domain.name()}":           "fixture.Business.persistFanout",
		"literal":                          "fixture.Business.persistLiteral", "constant": "fixture.Business.persistConstant",
		"reference": "fixture.Business.persistReference", "object": "fixture.Business.persistFanout",
		"inherited": "fixture.Business.persistFanout", "sender": "fixture.Business.persistInternal",
		"producer": "fixture.Business.persistReference", "duplicate": "fixture.Business.persistFanout",
		"local-endpoint":         "fixture.Business.persistFanout",
		"kafka:helper-reference": "fixture.Business.persistReference", "kafka:helper-lambda": "fixture.Business.persistLiteral",
		"inherited-configure": "fixture.Business.persistConstant", "runtime-uri": "fixture.Business.persistReference",
		"construction": "fixture.Business.persistLiteral",
	}
	duplicate := 0
	for _, ep := range entries {
		want := expected[ep.RouteID]
		if ep.Topic == "no-id" {
			if ep.RouteID != "" || ep.ConsumerGroup != "standalone" {
				t.Fatalf("entry without routeId lost group: %+v", ep)
			}
			want = "fixture.Business.persistReference"
		}
		if ep.Kind == EntryKindHTTP {
			if ep.Verb != "GET" || ep.Target != "/http" {
				t.Fatalf("HTTP changed: %+v", ep)
			}
			want = "fixture.Business.persistLiteral"
		} else {
			if ep.RouteID == "runtime-uri" {
				if ep.Kind != EntryKindCamel || ep.Target != "{uri}" {
					t.Fatalf("runtime URI guessed: %+v", ep)
				}
			} else if ep.RouteID != "unresolved" && (ep.Kind != EntryKindMessage || ep.Verb != "MESSAGE" || !strings.HasPrefix(ep.Target, "kafka:") || want == "") {
				t.Fatalf("unexpected entry: %+v", ep)
			}
			if ep.RouteID == "duplicate" {
				duplicate++
			}
			if ep.RouteID == "webhook-dispatch-{domain.name()}" {
				if ep.Topic != "{domain.source().topic()}" || strings.Contains(ep.Topic, "webhook-dispatch") || ep.ConsumerGroup != "{source.groupId()}.{domain.name()}" {
					t.Fatalf("A: setter topic/group lost or inferred from route ID: %+v", ep)
				}
			}
			if ep.RouteID == "fanout-{domain.name()}" && (ep.Topic != "{domain.source().topic()}" || ep.ConsumerGroup != "{domain.source().groupId()}") {
				t.Fatalf("B: helper source expressions lost: %+v", ep)
			}
		}
		flow, err := svc.GetApiFlow(t.Context(), ep.ID)
		if err != nil {
			t.Fatal(err)
		}
		if ep.RouteID == "unresolved" {
			if len(flow.Steps) != 1 {
				t.Fatalf("unresolved callback produced invented flow: %+v", flow)
			}
			continue
		}
		reached := false
		for _, step := range flow.Steps {
			if step.Label == want {
				reached = true
			}
			if strings.Contains(step.Label, "persist") && step.Label != want {
				t.Fatalf("%s reaches wrong business handler: %+v", ep.RouteID, flow)
			}
		}
		if !reached {
			t.Fatalf("%s did not reach %s: %+v", ep.RouteID, want, flow)
		}
	}
	if duplicate != 2 {
		t.Fatalf("duplicate IDs collapsed: %d", duplicate)
	}
	view, err := ReadStoredEntryPoints(t.Context(), config, project)
	if err != nil || view.State != ViewIndexed || len(view.Entries) != 20 || view.EntryPointSupport == "" || len(view.EntryPointWarnings) == 0 {
		t.Fatalf("disk view loses entries/support: %+v %v", view, err)
	}
	snapshot, err := ReadStoredSnapshot(t.Context(), config, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Endpoints) != 1 {
		t.Fatalf("Camel consumers treated as HTTP: %+v", snapshot.Endpoints)
	}
	foundA, foundProducer := false, false
	for _, k := range snapshot.Kafka {
		if strings.Contains(k.From, "WebhookDispatchRouteBuilder.<camel:") && k.Topic == "{domain.source().topic()}" && k.GroupID == "{source.groupId()}.{domain.name()}" && k.Client == KafkaCamel {
			foundA = true
		}
		if k.Role == KafkaProduce && k.Topic == "outbound" {
			foundProducer = true
		}
	}
	if !foundA || !foundProducer {
		t.Fatalf("Kafka cross-service accesses lost: %+v", snapshot.Kafka)
	}
}

func TestCamelEntryMetadataMigratesLegacyIndex(t *testing.T) {
	store := newTestStore(t)
	for _, col := range []string{"route_id", "topic", "consumer_group"} {
		if _, err := store.db.Exec("ALTER TABLE entry_points DROP COLUMN " + col); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec(`INSERT INTO entry_points(id, verb, target, entry_node) VALUES ('http', 'GET', '/old', 'handler')`); err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(store.db); err != nil {
		t.Fatal(err)
	}
	ep, err := store.GetEntryPoint(t.Context(), "http")
	if err != nil || ep.Target != "/old" || ep.RouteID != "" {
		t.Fatalf("legacy HTTP entry was not preserved: %+v %v", ep, err)
	}
	if err := migrateSchema(store.db); err != nil {
		t.Fatalf("migration is not repeatable: %v", err)
	}
}
