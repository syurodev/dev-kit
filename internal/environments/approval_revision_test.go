package environments

import "testing"

func TestPolicyRevisionChangesAcrossAbaPolicyMutation(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)
	tag, err := svc.CreateTag("approval-test", false)
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.PolicyRevision(tag.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTag(tag.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTag(tag.ID, false); err != nil {
		t.Fatal(err)
	}
	after, err := svc.PolicyRevision(tag.Name)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("environment policy revision did not change after false → true → false")
	}
}
