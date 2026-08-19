package sandbox

import (
	"strings"
	"testing"
)

func TestSeatbeltProfileAllowsWorkDir(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkNone}.withDefaults())
	if !strings.Contains(prof, "/tmp/proj") {
		t.Fatal(prof)
	}
	if !strings.Contains(prof, "(deny network-outbound)") {
		t.Fatalf("expected network-outbound denial in profile: %s", prof)
	}
	if !strings.Contains(prof, "(deny network-inbound)") {
		t.Fatalf("expected network-inbound denial in profile: %s", prof)
	}
}

func TestSeatbeltProfileDenyReadPaths(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{
		WorkDir:       "/tmp/proj",
		DenyReadPaths: []string{"/Users/alice/.ssh"},
		Env:           map[string]string{"HOME": "/Users/alice"},
	}.withDefaults())
	if !strings.Contains(prof, "/Users/alice/.ssh") {
		t.Fatalf("expected deny path in profile: %s", prof)
	}
	if !strings.Contains(prof, "(deny file-read*") {
		t.Fatalf("expected file-read deny rule: %s", prof)
	}
}

func TestSeatbeltProfileNetworkHostAllowsOutbound(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkHost}.withDefaults())
	if !strings.Contains(prof, "(allow network-outbound)") {
		t.Fatalf("expected network-outbound allowance for NetworkHost: %s", prof)
	}
	if strings.Contains(prof, "(deny network-outbound)") {
		t.Fatalf("NetworkHost must not deny outbound: %s", prof)
	}
	// DNS + logging mach services must be reachable under (deny default).
	for _, service := range seatbeltNetworkMachServices {
		if !strings.Contains(prof, service) {
			t.Fatalf("expected mach-lookup for %q: %s", service, prof)
		}
	}
}
