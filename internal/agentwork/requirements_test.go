package agentwork

import "testing"

func TestRequirementsDigestCanonicalAndDoesNotMutateInput(t *testing.T) {
	snapshot := RequirementsSnapshot{
		WorkspaceID: "workspace", BoardID: "board", TaskID: "task", Description: "requirements",
		Branch: "feature/task", Dependencies: []string{"b", "a", "b"}, Scope: []string{"two", "one"},
	}
	got, err := RequirementsDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Dependencies[0] != "b" || snapshot.Scope[0] != "two" {
		t.Fatal("digest mutated caller's snapshot")
	}
	snapshot.Dependencies = []string{"a", "b"}
	snapshot.Scope = []string{"one", "two"}
	want, err := RequirementsDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("set ordering changed requirements revision")
	}
	if len(got) < len("sha256:v2:") || got[:len("sha256:v2:")] != "sha256:v2:" {
		t.Fatalf("digest does not identify canonical version 2: %q", got)
	}
}

func TestRequirementsDigestIgnoresDisplayAndProgressFields(t *testing.T) {
	base := RequirementsSnapshot{
		WorkspaceID: "workspace", BoardID: "board", TaskID: "task", Description: "objective and acceptance criteria",
		Branch: "main", Scope: []string{"folder"}, Dependencies: []string{"dep"},
	}
	want, err := RequirementsDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Title = "rewritten title"
	base.Progress = "75%"
	base.RoleContract = "legacy role contract field"
	base.Acceptance = []string{"legacy separate acceptance field"}
	base.DependencyInputs = []DependencyInput{{TaskID: "dep", RequirementsRevision: "r1", ResultRevision: "head1"}}
	got, err := RequirementsDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("title, progress, or later-phase compatibility fields changed requirements revision")
	}
}

func TestRequirementsDigestBindsIdentityRequirementsAndScope(t *testing.T) {
	base := RequirementsSnapshot{
		WorkspaceID: "workspace", BoardID: "board", TaskID: "task", Description: "objective and acceptance",
		Branch: "main", Scope: []string{"folder"}, Dependencies: []string{"dep"},
	}
	want, err := RequirementsDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RequirementsSnapshot){
		func(s *RequirementsSnapshot) { s.WorkspaceID = "other" },
		func(s *RequirementsSnapshot) { s.BoardID = "other" },
		func(s *RequirementsSnapshot) { s.TaskID = "other" },
		func(s *RequirementsSnapshot) { s.Description += " revised" },
		func(s *RequirementsSnapshot) { s.Branch = "release" },
		func(s *RequirementsSnapshot) { s.Scope = []string{"other-folder"} },
		func(s *RequirementsSnapshot) { s.Dependencies = []string{"other-task"} },
	} {
		snapshot := base
		change(&snapshot)
		got, err := RequirementsDigest(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if got == want {
			t.Fatal("requirements-relevant change did not invalidate digest")
		}
	}
}

func TestRequirementsDigestIgnoresDependencyResultBindings(t *testing.T) {
	base := RequirementsSnapshot{
		WorkspaceID: "w", BoardID: "b", TaskID: "t", Dependencies: []string{"dep"},
		DependencyInputs: []DependencyInput{{TaskID: "dep", RequirementsRevision: "r1", ResultRevision: "head1"}},
	}
	want, err := RequirementsDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	base.DependencyInputs = []DependencyInput{{TaskID: "dep", RequirementsRevision: "r2", ResultRevision: "head2"}}
	got, err := RequirementsDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("later immutable dependency input bindings changed requirements digest")
	}
}

func TestRequirementsDigestRejectsMissingIdentity(t *testing.T) {
	if _, err := RequirementsDigest(RequirementsSnapshot{TaskID: "task"}); err == nil {
		t.Fatal("missing scope identity accepted")
	}
}
