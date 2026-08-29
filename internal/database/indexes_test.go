package database

import "testing"

func TestAppendIndexColumnGroupsRowsByIndex(t *testing.T) {
	var out []IndexInfo
	out = appendIndexColumn(out, IndexInfo{Table: "orders", Name: "PRIMARY", Unique: true, Primary: true}, "id")
	out = appendIndexColumn(out, IndexInfo{Table: "orders", Name: "uniq_customer", Unique: true}, "customer_id")
	out = appendIndexColumn(out, IndexInfo{Table: "orders", Name: "uniq_customer", Unique: true}, "tenant_id")
	out = appendIndexColumn(out, IndexInfo{Table: "invoices", Name: "PRIMARY", Unique: true, Primary: true}, "id")

	if len(out) != 3 {
		t.Fatalf("want 3 indexes, got %d: %+v", len(out), out)
	}
	if got := out[1].Columns; len(got) != 2 || got[0] != "customer_id" || got[1] != "tenant_id" {
		t.Fatalf("multi-column index lost its order or columns: %+v", got)
	}
	if out[2].Table != "invoices" {
		t.Fatalf("an index of the same name on another table was folded in: %+v", out)
	}
}

// A unique index whose columns cannot all be read must vanish, not survive with
// the columns that happened to arrive first. A two-column unique index read as
// covering only its first column claims a one-to-one the database never
// promised — the ERD would draw a relation that is simply false.
func TestDropIndexesRemovesPartiallyReadIndexes(t *testing.T) {
	var out []IndexInfo
	out = appendIndexColumn(out, IndexInfo{Table: "orders", Name: "uniq_expr", Unique: true}, "customer_id")
	out = appendIndexColumn(out, IndexInfo{Table: "orders", Name: "PRIMARY", Unique: true, Primary: true}, "id")

	kept := dropIndexes(out, map[string]bool{"orders\x00uniq_expr": true})

	for _, idx := range kept {
		if idx.Name == "uniq_expr" {
			t.Fatalf("a functional index was kept with only the columns that could be read: %+v", idx)
		}
	}
	if len(kept) != 1 || kept[0].Name != "PRIMARY" {
		t.Fatalf("want only PRIMARY kept, got %+v", kept)
	}
}
