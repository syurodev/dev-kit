package servermode

import "testing"

func TestValidateHostDefaultsBlankHostToLoopback(t *testing.T) {
	host, err := ValidateHost("")
	if err != nil {
		t.Fatalf("ValidateHost(\"\") error = %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("ValidateHost(\"\") = %q, want %q", host, "127.0.0.1")
	}
}

func TestValidateHostAcceptsLoopbackHosts(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			got, err := ValidateHost(host)
			if err != nil {
				t.Fatalf("ValidateHost(%q) error = %v", host, err)
			}
			if got != host {
				t.Fatalf("ValidateHost(%q) = %q, want %q", host, got, host)
			}
		})
	}
}

func TestValidateHostRejectsNonLoopbackHosts(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "192.168.1.10", "example.com"} {
		t.Run(host, func(t *testing.T) {
			if _, err := ValidateHost(host); err == nil {
				t.Fatalf("ValidateHost(%q) error = nil, want non-loopback rejection", host)
			}
		})
	}
}
