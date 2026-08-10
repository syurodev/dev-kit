package bridge

import (
	"testing"

	"devkit/internal/app"
)

func TestEnvListTagsReturnsBuiltinTags(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)

	tags, err := service.EnvListTags()
	if err != nil {
		t.Fatalf("EnvListTags: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("len(tags) = %d, want 3", len(tags))
	}
}

func TestEnvCreateTagThenUpdateRequiresConfirmation(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)

	created, err := service.EnvCreateTag("qa", false)
	if err != nil {
		t.Fatalf("EnvCreateTag: %v", err)
	}
	if created.RequiresConfirmation {
		t.Fatal("new tag should keep the requested RequiresConfirmation=false")
	}

	updated, err := service.EnvUpdateTag(created.ID, true)
	if err != nil {
		t.Fatalf("EnvUpdateTag: %v", err)
	}
	if !updated.RequiresConfirmation {
		t.Fatal("EnvUpdateTag did not persist RequiresConfirmation=true")
	}

	if err := service.EnvDeleteTag(created.ID); err != nil {
		t.Fatalf("EnvDeleteTag: %v", err)
	}
}

func TestEnvUpdateTagRejectsLockedProd(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)

	tags, err := service.EnvListTags()
	if err != nil {
		t.Fatalf("EnvListTags: %v", err)
	}
	var prodID string
	for _, tag := range tags {
		if tag.Name == "prod" {
			prodID = tag.ID
		}
	}
	if prodID == "" {
		t.Fatal("expected a builtin prod tag")
	}

	if _, err := service.EnvUpdateTag(prodID, false); err == nil {
		t.Fatal("EnvUpdateTag on prod must be rejected")
	}
}
