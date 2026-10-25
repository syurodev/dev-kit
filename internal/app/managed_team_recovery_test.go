package app

import (
	"errors"
	"testing"

	"devkit/internal/agentwork"
	"devkit/internal/tasks"
	"devkit/internal/vaultitem"
)

type managedFailingRead struct {
	tasks.ItemStore
	id string
}

func (s managedFailingRead) Get(id string) (vaultitem.Record, error) {
	if id == s.id {
		return vaultitem.Record{}, errors.New("transient read failure")
	}
	return s.ItemStore.Get(id)
}

func TestManagedReconciliationDoesNotTombstoneUncertainTaskReads(t *testing.T) {
	for _, parentFails := range []bool{false, true} {
		name := "child"
		if parentFails {
			name = "parent"
		}
		t.Run(name, func(t *testing.T) {
			c, _, store, db, _, _, parent, child, hub := managedTeamFixture(t)
			ctx := managedTeamContext()
			group, err := c.ManagedTeams.CreateGroup(ctx, parent.ID, managedStatuses())
			if err != nil {
				t.Fatal(err)
			}
			item, err := c.ManagedTeams.TrackWorkItem(ctx, group.ID, child.ID, "implementer")
			if err != nil {
				t.Fatal(err)
			}
			failedID := child.ID
			if parentFails {
				failedID = parent.ID
			}
			c.Tasks = tasks.NewService(managedFailingRead{ItemStore: c.Items, id: failedID})
			c.Tasks.SetScope(newTaskScope(c.Workspace, hub), db, "task_board_bindings")
			if parentFails {
				_, err = c.ManagedTeams.ReconcileGroup(ctx, group.ID)
			} else {
				_, err = c.ManagedTeams.ReconcileWorkItem(ctx, item.ID)
			}
			if !errors.Is(err, agentwork.ErrManagedTaskInvalid) {
				t.Fatalf("uncertain read = %v", err)
			}
			afterGroup, err := store.GetWorkGroup(ctx, group.ID)
			if err != nil {
				t.Fatal(err)
			}
			afterItem, err := store.GetWorkItem(ctx, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if afterGroup.Revision != group.Revision || afterGroup.EndedAt != nil || afterItem.Revision != item.Revision || afterItem.EndedAt != nil || afterItem.TombstoneReason != "" {
				t.Fatal("uncertain repository read changed durable ledger state")
			}
		})
	}
}

func TestManagedHostWithNoSessionHubFailsClosed(t *testing.T) {
	c, host, _, _, _, _, parent, child, _ := managedTeamFixture(t)
	ctx := managedTeamContext()
	group, err := c.ManagedTeams.CreateGroup(ctx, parent.ID, managedStatuses())
	if err != nil {
		t.Fatal(err)
	}
	c.AgentHub = nil
	host.hubOverride = nil
	if _, err := host.ResolveManagedParent(ctx, parent.ID); !errors.Is(err, agentwork.ErrManagedHostUnavailable) {
		t.Fatalf("parent without session host = %v", err)
	}
	if _, err := host.LoadManagedTask(ctx, group, child.ID); !errors.Is(err, agentwork.ErrManagedHostUnavailable) {
		t.Fatalf("task without session host = %v", err)
	}
}
