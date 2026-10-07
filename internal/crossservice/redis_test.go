package crossservice

import (
	"testing"

	"devkit/internal/codegraph"
)

func redis(key, access, command string) codegraph.SnapshotRedis {
	return codegraph.SnapshotRedis{
		RedisAccess: codegraph.RedisAccess{From: "node", Key: key, Access: access, Command: command, File: "F.java", Line: 1},
		Label:       "Cache." + command,
	}
}

func withRedis(s Service, uses ...codegraph.SnapshotRedis) Service {
	s.Snapshot.Redis = uses
	return s
}

func TestOverlaps(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"fsap:access:*", "fsap:access:*:*", true},
		{"fsap:access:*", "fsap:access:", true},
		{"fsap:access:*:*", "fsap:access:*", true},
		{"fsap:access:*", "fsap:enabled:*", false},
		{"fsap:*", "fsap:access:x", true},
		{"fsap:access:*", "*:access:*", true},
		{"a:*:c", "a:b:*", true},
		{"a:*:c", "a:b:d", false},
		{"session:*", "user:*", false},
		{"*", "anything", true},
		{"", "", true},
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"a*c", "a*d", false}, // one must end in c, the other in d
		{"a*c", "a*bc", true},
	} {
		if got := overlaps(c.a, c.b); got != c.want {
			t.Errorf("overlaps(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNormalizeAndSpecific(t *testing.T) {
	for in, want := range map[string]string{
		"fsap:access:{org}:{api}": "fsap:access:*:*",
		"fsap:access:${x}":        "fsap:access:*",
		"fsap:access:**":          "fsap:access:*",
		"plain":                   "plain",
		"{a}{b}":                  "*",
	} {
		if got := normalizeKey(in); got != want {
			t.Errorf("normalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
	for pattern, want := range map[string]bool{"fsap:access:*": true, "fs*": false, "*": false, "abc": true, "ab": false, "*:cache": false} {
		if got := specific(pattern); got != want {
			t.Errorf("specific(%q) = %v, want %v", pattern, got, want)
		}
	}
}

// The feedback's example: Java writes fsap:access:*, the Kong plugin reads it.
func TestRunRedisFindsTheKeyAJavaServiceWritesAndAPluginReads(t *testing.T) {
	access := withRedis(svc("access-service", "8081", nil),
		redis("fsap:access:{org}:{api}", "write", "set"),
		redis("fsap:access:{org}", "write", "delete"),
		redis("fsap:profile:{org}", "write", "put"))
	plugin := withRedis(svc("fsap-api-access", "", nil),
		redis("fsap:access:{org}:{api}", "read", "get"),
		redis("fsap:seen:{org}", "write", "setex"))
	billing := withRedis(svc("billing", "8082", nil), redis("billing:cache:{id}", "write", "set"))
	services := []Service{access, plugin, billing}

	report, err := RunRedis(services, RedisQuery{Key: "fsap:access:*"})
	if err != nil {
		t.Fatal(err)
	}
	if report.KeyTotal != 2 {
		t.Fatalf("keys = %+v", report.Keys)
	}
	var both *RedisKey
	for i := range report.Keys {
		if report.Keys[i].Pattern == "fsap:access:*:*" {
			both = &report.Keys[i]
		}
	}
	if both == nil || !both.CrossService || len(both.Writers) != 1 || len(both.Readers) != 1 {
		t.Fatalf("key = %+v", both)
	}
	if both.Writers[0].Service != "access-service" || both.Readers[0].Service != "fsap-api-access" || both.Readers[0].Key != "fsap:access:{org}:{api}" {
		t.Fatalf("writer/reader = %+v / %+v", both.Writers, both.Readers)
	}
	if len(both.Services) != 2 {
		t.Fatalf("services = %v: the other pattern is the same resource", both.Services)
	}
}

func TestRunRedisWithoutAKeyListsWhatIsShared(t *testing.T) {
	access := withRedis(svc("access-service", "8081", nil),
		redis("fsap:access:{org}", "write", "set"),
		redis("fsap:profile:{org}", "write", "put"))
	plugin := withRedis(svc("fsap-api-access", "", nil),
		redis("fsap:access:{org}", "read", "get"),
		redis("fsap:seen:{org}", "write", "setex"))
	report, err := RunRedis([]Service{access, plugin}, RedisQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if report.KeyTotal != 1 || report.Keys[0].Pattern != "fsap:access:*" || !report.Keys[0].CrossService {
		t.Fatalf("shared keys = %+v", report.Keys)
	}
}

func TestRunRedisTwoWritersAndNoReaderIsSharedButNotCrossService(t *testing.T) {
	a := withRedis(svc("a-service", "8081", nil), redis("lock:{id}", "write", "set"))
	b := withRedis(svc("b-service", "8082", nil), redis("lock:{id}", "write", "set"))
	report, err := RunRedis([]Service{a, b}, RedisQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if report.KeyTotal != 1 || report.Keys[0].CrossService || len(report.Keys[0].Services) != 2 {
		t.Fatalf("keys = %+v", report.Keys)
	}
}

func TestRunRedisOneServiceReadingAndWritingItsOwnKeyIsNotCrossService(t *testing.T) {
	a := withRedis(svc("a-service", "8081", nil), redis("cache:{id}", "write", "set"), redis("cache:{id}", "read", "get"))
	b := withRedis(svc("b-service", "8082", nil), redis("other:{id}", "write", "set"))
	report, err := RunRedis([]Service{a, b}, RedisQuery{Key: "cache:*"})
	if err != nil {
		t.Fatal(err)
	}
	if report.KeyTotal != 1 || report.Keys[0].CrossService || len(report.Keys[0].Services) != 1 {
		t.Fatalf("keys = %+v", report.Keys)
	}
}

func TestRunRedisRefusesAKeyThatMatchesEverythingAndIgnoresGenericOnes(t *testing.T) {
	a := withRedis(svc("a-service", "8081", nil), redis("{key}", "write", "set"), redis("x:{id}", "write", "set"), redis("ab*", "write", "set"))
	b := withRedis(svc("b-service", "8082", nil), redis("{key}", "read", "get"))
	for _, key := range []string{"*", "ab", "{x}:cache"} {
		if _, err := RunRedis([]Service{a, b}, RedisQuery{Key: key}); err == nil {
			t.Errorf("key %q must be refused as too generic", key)
		}
	}
	report, err := RunRedis([]Service{a, b}, RedisQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if report.KeyTotal != 0 {
		t.Fatalf("generic keys must not be linked: %+v", report.Keys)
	}
	if report.Keys == nil {
		t.Fatal("an empty list must be [], not null")
	}
}

func TestRunRedisNamesAServiceItCouldNotRead(t *testing.T) {
	a := withRedis(svc("a-service", "8081", nil), redis("lock:{id}", "write", "set"))
	gone := Service{Name: "reports-service", Path: "/ws/reports-service"}
	report, err := RunRedis([]Service{a, gone}, RedisQuery{})
	if err != nil {
		t.Fatal(err)
	}
	said := false
	for _, n := range report.Notes {
		if n == "reports-service is not-indexed: its Redis keys are not in this answer" {
			said = true
		}
	}
	if !said {
		t.Fatalf("notes = %v", report.Notes)
	}
	if _, err := RunRedis([]Service{a}, RedisQuery{Limit: -1}); err == nil {
		t.Fatal("a negative limit must be refused")
	}
}
