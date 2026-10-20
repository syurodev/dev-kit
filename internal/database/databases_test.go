package database

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestDatabaseContextInheritsPolicyAndHasNoCredentialPayload(t *testing.T) {
	svc, store := newServiceWithStore(t)
	root, err := svc.CreateProfile(CreateProfileInput{Name: "server", Engine: EnginePostgres, Host: "127.0.0.1", Port: 5432, Username: "reader", Password: "fixture-secret", Database: "postgres", Env: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(metadata{ParentProfileID: root.ID, Database: "orders"})
	child, err := store.Create(itemType, passwordKeyID, raw, []byte{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateProfileOptions(root.ID, UpdateProfileOptionsInput{Env: "prod", ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	target, err := svc.target(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if target.cfg.Database != "orders" || target.cfg.Password != "fixture-secret" || !target.cfg.ReadOnly {
		t.Fatal("database target did not inherit parent")
	}
	sum, err := svc.profileSummary(child)
	if err != nil {
		t.Fatal(err)
	}
	if sum.ParentProfileID != root.ID || sum.Env != "prod" || !sum.ReadOnly || sum.Name != "server / orders" {
		t.Fatalf("summary=%+v", sum)
	}
	profiles, err := svc.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	var inherited bool
	for _, profile := range profiles {
		if profile.ID == child.ID {
			inherited = profile.Env == "prod" && profile.ReadOnly && profile.ParentProfileID == root.ID
		}
	}
	if !inherited {
		t.Fatal("public profile listing lost the child's parent policy")
	}
	payload, err := store.OpenPayload(child, passwordKeyID)
	if err != nil || len(payload) != 0 {
		t.Fatal("child duplicated credentials")
	}
	if _, err := svc.UpdateProfileOptions(child.ID, UpdateProfileOptionsInput{Env: "dev", ReadOnly: false}); err == nil {
		t.Fatal("child policy override allowed")
	}
	if err := svc.DeleteProfile(root.ID); err != nil {
		t.Fatal(err)
	}
	if profiles, err := svc.ListProfiles(); err != nil || len(profiles) != 0 {
		t.Fatalf("cascade profiles=%d error=%v", len(profiles), err)
	}
}
func TestDatabaseContextFailsClosedForNestedParent(t *testing.T) {
	svc, store := newServiceWithStore(t)
	first, _ := json.Marshal(metadata{ParentProfileID: "missing", Database: "a"})
	a, err := store.Create(itemType, passwordKeyID, first, []byte{})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := json.Marshal(metadata{ParentProfileID: a.ID, Database: "b"})
	b, err := store.Create(itemType, passwordKeyID, second, []byte{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.target(b.ID); err == nil {
		t.Fatal("nested context accepted")
	}
}

// Use a disposable PostgreSQL server; never enable this against production.
func TestPostgresDatabaseCatalogLive(t *testing.T) {
	if os.Getenv("DEVKIT_PG_CATALOG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_CATALOG_TEST=1 with a disposable PostgreSQL server")
	}
	port, err := strconv.Atoi(os.Getenv("DEVKIT_PG_CATALOG_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	password := os.Getenv("DEVKIT_PG_CATALOG_PASSWORD")
	if password == "" {
		t.Fatal("set DEVKIT_PG_CATALOG_PASSWORD for the disposable server")
	}
	cfg := ConnConfig{Engine: EnginePostgres, Host: "127.0.0.1", Port: port, Database: "postgres", Username: "postgres", Password: password, SSLMode: "disable"}
	admin, err := postgresDriver{}.Dial(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	exec := func(conn Conn, sql string) {
		t.Helper()
		rows, err := conn.Query(ctx, sql)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	dbA, dbB := "devkit_a_"+suffix, "devkit_b_"+suffix
	for _, name := range []string{dbA, dbB} {
		exec(admin, `CREATE DATABASE "`+name+`"`)
	}
	defer func() {
		for _, name := range []string{dbA, dbB} {
			rows, err := admin.Query(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
			if err == nil {
				rows.Close()
			}
		}
	}()
	svc, _ := newServiceWithStore(t)
	defer svc.Close()
	svc.SetSchemaCache(NewMemorySchemaCache())
	root, err := svc.CreateProfile(CreateProfileInput{Name: "server", Engine: EnginePostgres, Host: "127.0.0.1", Port: port, Username: "postgres", Password: password, SSLMode: "disable", Env: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	names, err := svc.ListDatabases(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, name := range names {
		found[name] = true
	}
	if !found[dbA] || !found[dbB] {
		t.Fatalf("catalog=%v", names)
	}
	a, err := svc.OpenDatabase(ctx, root.ID, dbA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.OpenDatabase(ctx, root.ID, dbB)
	if err != nil {
		t.Fatal(err)
	}
	var concurrent sync.WaitGroup
	results := make(chan ProfileSummary, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		concurrent.Add(1)
		go func() {
			defer concurrent.Done()
			p, err := svc.OpenDatabase(ctx, root.ID, dbA)
			results <- p
			failures <- err
		}()
	}
	concurrent.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for p := range results {
		if p.ID != a.ID {
			t.Fatal("concurrent open duplicated context")
		}
	}
	if a.ID == b.ID || a.ID == root.ID {
		t.Fatal("database pool keys collided")
	}
	again, err := svc.OpenDatabase(ctx, root.ID, dbA)
	if err != nil || again.ID != a.ID {
		t.Fatal("context ID is not stable")
	}
	for _, pair := range []struct{ id, value string }{{a.ID, "alpha"}, {b.ID, "beta"}} {
		if _, err := svc.Query(ctx, pair.id, "CREATE TABLE public.same_name(value text)", 0); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Query(ctx, pair.id, "INSERT INTO public.same_name VALUES ('"+pair.value+"')", 0); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.IndexSchema(ctx, pair.id); err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range []struct{ id, value string }{{a.ID, "alpha"}, {b.ID, "beta"}} {
		result, err := svc.Query(ctx, pair.id, "SELECT current_database(), value FROM public.same_name", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 1 || *result.Rows[0][1] != pair.value {
			t.Fatalf("wrong database result=%+v", result)
		}
	}
	if _, err := svc.Query(ctx, a.ID, "CREATE TABLE public.only_a(id integer)", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IndexSchema(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	cacheB, err := svc.GetSchemaCache(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := cacheB.Namespace["public"]["only_a"]; found {
		t.Fatal("schema cache crossed database boundary")
	}
	if err := svc.SetTxMode(ctx, a.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Query(ctx, a.ID, "INSERT INTO public.same_name VALUES ('uncommitted')", 0); err != nil {
		t.Fatal(err)
	}
	if !svc.TxOpen(a.ID) || svc.TxOpen(b.ID) {
		t.Fatal("transaction crossed database boundary")
	}
	if err := svc.RollbackTx(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTxMode(ctx, a.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateProfileOptions(root.ID, UpdateProfileOptionsInput{Env: "prod", ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Query(ctx, a.ID, "INSERT INTO public.same_name VALUES ('forbidden')", 0); err == nil {
		t.Fatal("parent read-only did not protect child database")
	}
	if _, err := svc.OpenDatabase(ctx, root.ID, "not_present"); err == nil {
		t.Fatal("unlisted database opened")
	}
}
