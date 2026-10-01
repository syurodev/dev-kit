package app

import (
	"strings"
	"testing"

	"devkit/internal/workspace"
)

type briefWorkspaces struct {
	all      []workspace.Workspace
	services map[string][]workspace.WorkspaceService
}

func (b briefWorkspaces) ListWorkspaces() ([]workspace.Workspace, error) { return b.all, nil }
func (b briefWorkspaces) ListServices(id string) ([]workspace.WorkspaceService, error) {
	return b.services[id], nil
}

func briefFixture() briefWorkspaces {
	return briefWorkspaces{
		all: []workspace.Workspace{
			{ID: "sec", Name: "fsap-security", Root: "/w/kong-plugin"},
			{ID: "bill", Name: "billing", Root: "/w/billing"},
		},
		services: map[string][]workspace.WorkspaceService{
			"sec": {
				{Name: "kong-plugin", Path: "/w/kong-plugin"},
				{Name: "fsap-authentication-service", Path: "/w/auth"},
				{Name: "fsap-config-service", Path: "/w/config"},
			},
			"bill": {{Name: "billing", Path: "/w/billing"}},
		},
	}
}

func TestAgentSessionBriefNamesTheWorkspaceAndWhatMayBeWritten(t *testing.T) {
	brief := AgentSessionBrief(briefFixture())("sec", "/w/kong-plugin/kong", []string{"/w/kong-plugin/kong", "/w/auth"})
	for _, want := range []string{
		`Workspace "fsap-security"`,
		"- kong-plugin — /w/kong-plugin (primary, you are here, you may write only under your working directory)",
		"- fsap-authentication-service — /w/auth (you may write)",
		"- fsap-config-service — /w/config (read-only)",
		"`repo`",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief lacks %q:\n%s", want, brief)
		}
	}
	// Another workspace is none of this session's business.
	if strings.Contains(brief, "billing") {
		t.Errorf("brief lists another workspace:\n%s", brief)
	}
}

func TestAgentSessionBriefOutsideEveryWorkspace(t *testing.T) {
	brief := AgentSessionBrief(briefFixture())("", "/tmp/scratch", []string{"/tmp/scratch", "/opt/shared"})
	if !strings.Contains(brief, "no selected DevKit workspace") || !strings.Contains(brief, "/opt/shared") {
		t.Errorf("brief = %s", brief)
	}
	if got := AgentSessionBrief(nil)("", "/w", nil); got != "" {
		t.Errorf("no workspaces: brief = %q, want none so the prompt asks", got)
	}
}
