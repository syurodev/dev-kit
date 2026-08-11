package environments

import (
	"errors"
	"testing"
)

func TestNewMemoryRepositorySeedsBuiltinTags(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)

	tags, err := svc.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("len(tags) = %d, want 3", len(tags))
	}

	byName := map[string]Tag{}
	for _, tag := range tags {
		byName[tag.Name] = tag
	}
	dev, ok := byName["dev"]
	if !ok || dev.RequiresConfirmation || !dev.IsBuiltin || dev.Locked {
		t.Fatalf("dev tag = %+v, want unlocked builtin, requires_confirmation=false", dev)
	}
	prod, ok := byName["prod"]
	if !ok || !prod.RequiresConfirmation || !prod.IsBuiltin || !prod.Locked {
		t.Fatalf("prod tag = %+v, want locked builtin, requires_confirmation=true", prod)
	}
}

func TestServiceExists(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)

	if !svc.Exists("dev") {
		t.Fatal("Exists(dev) = false, want true")
	}
	if svc.Exists("does-not-exist") {
		t.Fatal("Exists(does-not-exist) = true, want false")
	}
}

func TestCreateTagRejectsEmptyAndDuplicateName(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)

	if _, err := svc.CreateTag("", false); err == nil {
		t.Fatal("CreateTag(\"\") error = nil, want error")
	}
	if _, err := svc.CreateTag("qa", false); err != nil {
		t.Fatalf("CreateTag(qa): %v", err)
	}
	if _, err := svc.CreateTag("qa", true); err == nil {
		t.Fatal("CreateTag(qa) duplicate error = nil, want error")
	}
	if _, err := svc.CreateTag("dev", false); err == nil {
		t.Fatal("CreateTag(dev) duplicate of builtin error = nil, want error")
	}
}

func TestCreateTagProducesUnlockedNonBuiltinTag(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)

	tag, err := svc.CreateTag("qa", true)
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if tag.IsBuiltin || tag.Locked {
		t.Fatalf("new tag = %+v, want IsBuiltin=false Locked=false", tag)
	}
	if !tag.RequiresConfirmation {
		t.Fatal("new tag did not keep the requested RequiresConfirmation=true")
	}
	if !svc.Exists("qa") {
		t.Fatal("Exists(qa) = false after CreateTag")
	}
}

func TestUpdateTagRejectsLockedProd(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)
	tags, err := svc.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
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

	if _, err := svc.UpdateTag(prodID, false); err == nil {
		t.Fatal("UpdateTag(prod) error = nil, want error")
	}
}

func TestUpdateTagAllowsUnlockedDev(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)
	tags, err := svc.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	var devID string
	for _, tag := range tags {
		if tag.Name == "dev" {
			devID = tag.ID
		}
	}

	updated, err := svc.UpdateTag(devID, true)
	if err != nil {
		t.Fatalf("UpdateTag(dev): %v", err)
	}
	if !updated.RequiresConfirmation {
		t.Fatal("UpdateTag(dev) did not persist RequiresConfirmation=true")
	}
}

func TestDeleteTagRejectsBuiltinAndLocked(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)
	tags, err := svc.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	for _, tag := range tags {
		if err := svc.DeleteTag(tag.ID); err == nil {
			t.Fatalf("DeleteTag(%s) error = nil, want error (builtin)", tag.Name)
		}
	}
}

func TestDeleteTagAllowsCustomTag(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)
	tag, err := svc.CreateTag("qa", false)
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := svc.DeleteTag(tag.ID); err != nil {
		t.Fatalf("DeleteTag(qa): %v", err)
	}
	if svc.Exists("qa") {
		t.Fatal("Exists(qa) = true after DeleteTag")
	}
}

type stubUsage map[string]bool

func (s stubUsage) EnvInUse(name string) (bool, error) { return s[name], nil }

func TestDeleteTagRejectsTagInUse(t *testing.T) {
	svc := NewService(NewMemoryRepository(), stubUsage{"qa": true})
	tag, err := svc.CreateTag("qa", false)
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := svc.DeleteTag(tag.ID); !errors.Is(err, ErrTagInUse) {
		t.Fatalf("DeleteTag(qa in use) error = %v, want ErrTagInUse", err)
	}
	if !svc.Exists("qa") {
		t.Fatal("Exists(qa) = false after rejected DeleteTag; tag must remain")
	}
}

func TestDeleteTagAllowsUnusedCustomTagWithUsageChecker(t *testing.T) {
	svc := NewService(NewMemoryRepository(), stubUsage{})
	tag, err := svc.CreateTag("qa", false)
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := svc.DeleteTag(tag.ID); err != nil {
		t.Fatalf("DeleteTag(unused qa): %v", err)
	}
}

func TestRequiresConfirmationProdTrue(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)
	if !svc.RequiresConfirmation("prod") {
		t.Fatal("RequiresConfirmation(prod) = false, want true")
	}
}

func TestRequiresConfirmationUnknownFailClosed(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)
	if !svc.RequiresConfirmation("does-not-exist") {
		t.Fatal("RequiresConfirmation(unknown) = false, want true (fail closed)")
	}
}

func TestRequiresConfirmationDevDefaultFalse(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)
	if svc.RequiresConfirmation("dev") {
		t.Fatal("RequiresConfirmation(dev) = true, want false")
	}
}

func TestRequiresConfirmationEmptyFailClosed(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil)
	if !svc.RequiresConfirmation("") {
		t.Fatal("RequiresConfirmation(\"\") = false, want true (fail closed)")
	}
}
