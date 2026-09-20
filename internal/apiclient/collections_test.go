package apiclient

import (
	"errors"
	"strings"
	"testing"
)

func TestCollectionsNestAndSortByOrderThenName(t *testing.T) {
	svc := newService(t)
	parent, err := svc.SaveCollection(Collection{Name: "GitHub", Order: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveCollection(Collection{Name: "Repos", ParentID: parent.ID, Order: 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveCollection(Collection{Name: "Actions", ParentID: parent.ID, Order: 0}); err != nil {
		t.Fatal(err)
	}

	all, err := svc.Collections()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("collections = %d, want 3", len(all))
	}
	// Order first, then name — two folders a person has not ordered should
	// still come out in a stable, readable sequence.
	if all[0].Name != "Actions" || all[1].Name != "Repos" || all[2].Name != "GitHub" {
		t.Fatalf("order = %v", []string{all[0].Name, all[1].Name, all[2].Name})
	}
}

// A cycle is not cosmetic: the sidebar and the delete check both walk this
// tree, and both would walk it forever.
func TestCollectionRefusesACycle(t *testing.T) {
	svc := newService(t)
	top, err := svc.SaveCollection(Collection{Name: "Top"})
	if err != nil {
		t.Fatal(err)
	}
	middle, err := svc.SaveCollection(Collection{Name: "Middle", ParentID: top.ID})
	if err != nil {
		t.Fatal(err)
	}
	bottom, err := svc.SaveCollection(Collection{Name: "Bottom", ParentID: middle.ID})
	if err != nil {
		t.Fatal(err)
	}

	top.ParentID = bottom.ID
	if _, err := svc.SaveCollection(top); !errors.Is(err, ErrInvalid) {
		t.Fatalf("closing a loop = %v, want ErrInvalid", err)
	}
	middle.ParentID = middle.ID
	if _, err := svc.SaveCollection(middle); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a folder as its own parent = %v, want ErrInvalid", err)
	}
	if _, err := svc.SaveCollection(Collection{Name: "Orphan", ParentID: "no-such-folder"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a parent that does not exist = %v, want ErrNotFound", err)
	}
}

// Tidying a folder is not the same action as losing every saved request in
// it, and a vault item has no undo.
func TestDeletingAFolderRefusesWhileItHoldsAnything(t *testing.T) {
	svc := newService(t)
	folder, err := svc.SaveCollection(Collection{Name: "GitHub"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.SaveRequest(Request{
		CollectionID: folder.ID, Name: "List repos", Method: "get", URL: "{{baseUrl}}/repos",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = svc.DeleteCollection(folder.ID)
	if !errors.Is(err, ErrCollectionNotEmpty) {
		t.Fatalf("deleting a full folder = %v, want ErrCollectionNotEmpty", err)
	}
	// The refusal has to say what is inside, or it is an error with no next
	// step.
	if !strings.Contains(err.Error(), "List repos") {
		t.Fatalf("the refusal does not name what it holds: %v", err)
	}

	if err := svc.DeleteRequest(saved.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteCollection(folder.ID); err != nil {
		t.Fatalf("an empty folder was refused: %v", err)
	}

	child, err := svc.SaveCollection(Collection{Name: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveCollection(Collection{Name: "Child", ParentID: child.ID}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteCollection(child.ID); !errors.Is(err, ErrCollectionNotEmpty) {
		t.Fatalf("a folder holding a folder = %v, want ErrCollectionNotEmpty", err)
	}
}

func TestCollectionNeedsAName(t *testing.T) {
	svc := newService(t)
	if _, err := svc.SaveCollection(Collection{Name: "   "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unnamed folder = %v, want ErrInvalid", err)
	}
}
