package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUsageMetricsMigrationPreservesLegacyBytesAndGeneration(t *testing.T) {
	db, e := Open(filepath.Join(t.TempDir(), "old.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx := context.Background()
	for _, q := range []string{
		`CREATE TABLE agent_accounts(id TEXT PRIMARY KEY,home_dir TEXT)`,
		`CREATE TABLE agent_turns(id TEXT PRIMARY KEY,session_id TEXT,usage_json TEXT,raw_meta TEXT)`,
		`CREATE TABLE agent_transcript(id TEXT PRIMARY KEY,kind TEXT,created_at TEXT)`,
		`INSERT INTO agent_accounts VALUES('claude-default',''),('work','/private/home')`,
		`INSERT INTO agent_turns VALUES('old','s','{"InputTokens":0}',' { "private": "retain exact bytes" } ')`,
	} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if e = migrateUsageMetrics(ctx, db); e != nil {
		t.Fatal(e)
	}
	var builtin, gen string
	if e = db.QueryRow(`SELECT usage_generation FROM agent_accounts WHERE id='claude-default'`).Scan(&builtin); e != nil || builtin != "builtin" {
		t.Fatalf("builtin %s %v", builtin, e)
	}
	if e = db.QueryRow(`SELECT usage_generation FROM agent_accounts WHERE id='work'`).Scan(&gen); e != nil || gen == "" {
		t.Fatalf("generation %s %v", gen, e)
	}
	if e = migrateUsageMetrics(ctx, db); e != nil {
		t.Fatal(e)
	}
	var repeat string
	db.QueryRow(`SELECT usage_generation FROM agent_accounts WHERE id='work'`).Scan(&repeat)
	if gen != repeat {
		t.Fatal("migration changed existing generation")
	}
	var usage, raw string
	var legacy bool
	if e = db.QueryRow(`SELECT usage_json,raw_meta,account_id IS NULL AND account_generation IS NULL AND started_at IS NULL AND usage_workspace_id IS NULL AND usage_context_version IS NULL AND harness_profile_revision IS NULL AND harness_override_revision IS NULL FROM agent_turns WHERE id='old'`).Scan(&usage, &raw, &legacy); e != nil || !legacy || usage != `{"InputTokens":0}` || raw != ` { "private": "retain exact bytes" } ` {
		t.Fatalf("legacy changed %s %s %v %v", usage, raw, legacy, e)
	}
}
