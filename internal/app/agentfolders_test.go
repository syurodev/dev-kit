package app

import (
	"testing"

	"devkit/internal/agenthub"
)

func TestAgentWorkspaceFoldersNamesTheWorkspaceAndItsServices(t *testing.T) {
	name, folders, err := AgentWorkspaceFolders(briefFixture())("sec")
	if err != nil {
		t.Fatal(err)
	}
	if name != "fsap-security" || len(folders) != 3 {
		t.Fatalf("name = %q, folders = %v", name, folders)
	}
	if folders[1] != (agenthub.WorkspaceFolder{Name: "fsap-authentication-service", Path: "/w/auth"}) {
		t.Fatalf("folders[1] = %+v", folders[1])
	}
}

func TestAgentWorkspaceFoldersForAnUnknownWorkspaceIsEmpty(t *testing.T) {
	name, folders, err := AgentWorkspaceFolders(briefFixture())("nope")
	if err != nil || name != "" || len(folders) != 0 {
		t.Fatalf("got %q, %v, %v", name, folders, err)
	}
}
