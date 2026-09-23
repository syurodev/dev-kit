package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func writeResource(t *testing.T, service, name, content string) {
	t.Helper()
	dir := filepath.Join(service, "src", "main", "resources")
	mkdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSpringConfig_YAMLFlattensAndSkipsProfileDocuments(t *testing.T) {
	svc := t.TempDir()
	writeResource(t, svc, "application.yml", `
spring:
  application:
    name: billing-service
server:
  port: 8081
clients:
  refund:
    url: http://refund-service/api
  hosts: [a, b]
---
spring:
  config:
    activate:
      on-profile: prod
clients:
  refund:
    url: http://refund.prod.example.com
`)
	cfg, err := ReadSpringConfig(svc)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ApplicationName(); got != "billing-service" {
		t.Errorf("ApplicationName = %q", got)
	}
	if got := cfg.ServerPort(); got != "8081" {
		t.Errorf("ServerPort = %q", got)
	}
	if got := cfg.Props["clients.refund.url"]; got != "http://refund-service/api" {
		t.Errorf("clients.refund.url = %q, want the default-profile value", got)
	}
	if got := cfg.Props["clients.hosts[1]"]; got != "b" {
		t.Errorf("clients.hosts[1] = %q", got)
	}
}

func TestReadSpringConfig_PropertiesOverrideYAMLAndBootstrap(t *testing.T) {
	svc := t.TempDir()
	writeResource(t, svc, "bootstrap.yml", "spring.application.name: from-bootstrap\nother: kept\n")
	writeResource(t, svc, "application.yml", "spring:\n  application:\n    name: from-yml\n")
	writeResource(t, svc, "application.properties", "# comment\nspring.application.name = from-properties\nlong.value=a\\\n  b\n")
	cfg, err := ReadSpringConfig(svc)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ApplicationName(); got != "from-properties" {
		t.Errorf("ApplicationName = %q, want from-properties", got)
	}
	if got := cfg.Props["other"]; got != "kept" {
		t.Errorf("other = %q, want the bootstrap value kept", got)
	}
	if got := cfg.Props["long.value"]; got != "ab" {
		t.Errorf("long.value = %q, want the continued line joined", got)
	}
}

func TestReadSpringConfig_NoFilesIsEmptyAndDefaultPort(t *testing.T) {
	cfg, err := ReadSpringConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApplicationName() != "" || cfg.ServerPort() != "8080" {
		t.Fatalf("name %q port %q, want empty and Spring's default 8080", cfg.ApplicationName(), cfg.ServerPort())
	}
}

func TestReadSpringConfig_BrokenFileIsAnError(t *testing.T) {
	svc := t.TempDir()
	writeResource(t, svc, "application.yml", "a: [unclosed\n")
	if _, err := ReadSpringConfig(svc); err == nil {
		t.Fatal("want an error for a YAML file that does not parse")
	}
}

func TestSpringConfigResolve(t *testing.T) {
	cfg := SpringConfig{Props: map[string]string{
		"billing.host": "billing-service",
		"billing.url":  "http://${billing.host}:${billing.port:8080}",
		"loop":         "${loop}",
	}}
	cases := []struct {
		in, want string
		complete bool
	}{
		{"${billing.url}/refunds", "http://billing-service:8080/refunds", true},
		{"${BILLING_URL:http://billing-service}/x", "http://billing-service/x", true},
		{"${BILLING_URL}/x", "${BILLING_URL}/x", false},
		{"${A:${billing.host}}", "billing-service", true},
		{"plain", "plain", true},
		{"${loop}", "${loop}", false},
	}
	for _, c := range cases {
		got, complete := cfg.Resolve(c.in)
		if got != c.want || complete != c.complete {
			t.Errorf("Resolve(%q) = %q, %v; want %q, %v", c.in, got, complete, c.want, c.complete)
		}
	}
}

func TestAddWorkspace_NamesServicesFromConfig(t *testing.T) {
	root := t.TempDir()
	billing := filepath.Join(root, "billing")
	mkGitDir(t, billing)
	writeResource(t, billing, "application.yml", "spring:\n  application:\n    name: billing-service\n")
	mkGitDir(t, filepath.Join(root, "web"))

	svc := NewService(NewMemoryRepository())
	_, services, err := svc.AddWorkspace("", root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range services {
		got[s.Name] = s.NameSource
	}
	if got["billing-service"] != NameSourceConfig || got["web"] != NameSourceFolder {
		t.Fatalf("names/sources = %v, want billing-service from config and web from its folder", got)
	}
}
