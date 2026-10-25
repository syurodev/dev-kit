package agentwork

import "testing"

func TestRequirementsDigestCanonicalAndDoesNotMutateInput(t *testing.T) {
	snapshot := RequirementsSnapshot{WorkspaceID: "workspace", BoardID: "board", TaskID: "task", Description: "requirements", Dependencies: []string{"b", "a", "b"}, Scope: []string{"two", "one"}}
	snapshot.DependencyInputs = []DependencyInput{{TaskID: "b", RequirementsRevision: "b1"}, {TaskID: "a", RequirementsRevision: "a1"}}
	got, err := RequirementsDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Dependencies[0] != "b" || snapshot.Scope[0] != "two" {
		t.Fatal("digest mutated caller's snapshot")
	}
	snapshot.Dependencies = []string{"a", "b"}
	snapshot.Scope = []string{"one", "two"}
	snapshot.Acceptance = []string{}
	want, err := RequirementsDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("set ordering changed requirements revision")
	}
}

func TestRequirementsDigestBindsAllRequirementsAndScope(t *testing.T) {
	base := RequirementsSnapshot{WorkspaceID: "workspace", BoardID: "board", TaskID: "task", Title: "title", Description: "body", Acceptance: []string{"pass"}, Scope: []string{"folder"}, Dependencies: []string{"dep"}}
	base.DependencyInputs = []DependencyInput{{TaskID: "dep", RequirementsRevision: "dep1", ResultRevision: "head1"}}
	want, err := RequirementsDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RequirementsSnapshot){
		func(s *RequirementsSnapshot) { s.WorkspaceID = "other" },
		func(s *RequirementsSnapshot) { s.BoardID = "other" },
		func(s *RequirementsSnapshot) { s.TaskID = "other" },
		func(s *RequirementsSnapshot) { s.Title += "!" },
		func(s *RequirementsSnapshot) { s.Description += "!" },
		func(s *RequirementsSnapshot) { s.RoleContract = "review independently" },
		func(s *RequirementsSnapshot) { s.Acceptance = []string{"different"} },
		func(s *RequirementsSnapshot) { s.Scope = []string{"other"} },
		func(s *RequirementsSnapshot) {
			s.Dependencies = []string{"other"}
			s.DependencyInputs = []DependencyInput{{TaskID: "other", RequirementsRevision: "dep1", ResultRevision: "head1"}}
		},
		func(s *RequirementsSnapshot) {
			s.DependencyInputs = []DependencyInput{{TaskID: "dep", RequirementsRevision: "dep2", ResultRevision: "head1"}}
		},
		func(s *RequirementsSnapshot) {
			s.DependencyInputs = []DependencyInput{{TaskID: "dep", RequirementsRevision: "dep1", ResultRevision: "head2"}}
		},
	} {
		snapshot := base
		change(&snapshot)
		got, err := RequirementsDigest(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if got == want {
			t.Fatal("requirements change did not invalidate digest")
		}
	}
}

func TestRequirementsDigestRequiresDependencyRevisions(t *testing.T) {
	s := RequirementsSnapshot{WorkspaceID: "w", BoardID: "b", TaskID: "t", Dependencies: []string{"dep"}}
	if _, err := RequirementsDigest(s); err == nil {
		t.Fatal("unresolved dependency accepted")
	}
	s.DependencyInputs = []DependencyInput{{TaskID: "different", RequirementsRevision: "v1"}}
	if _, err := RequirementsDigest(s); err == nil {
		t.Fatal("mismatched dependency accepted")
	}
}

func TestRequirementsDigestRejectsMissingIdentity(t *testing.T) {
	if _, err := RequirementsDigest(RequirementsSnapshot{TaskID: "task"}); err == nil {
		t.Fatal("missing scope identity accepted")
	}
}
