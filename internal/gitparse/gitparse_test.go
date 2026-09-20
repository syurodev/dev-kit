package gitparse

import (
	"reflect"
	"testing"
)

func TestPorcelain(t *testing.T) {
	out := []byte(" M services/refund/src/A.java\x00?? services/refund/src/B.java\x00R  services/refund/src/New.java\x00services/refund/src/Old.java\x00 D services/refund/pom.xml\x00M  other/C.java\x00")
	got := Porcelain(out, "services/refund/")
	want := map[string]string{
		"src/A.java":   "M",
		"src/B.java":   "A",
		"src/New.java": "R",
		"pom.xml":      "D",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Porcelain = %v, want %v", got, want)
	}
}

func TestPorcelainAtRepoRoot(t *testing.T) {
	got := Porcelain([]byte("A  src/main/New.java\x00"), "")
	if got["src/main/New.java"] != "A" {
		t.Fatalf("Porcelain = %v", got)
	}
}

// A rename's second field is its source path, not another entry.
func TestPorcelainDoesNotReadARenameSourceAsAnEntry(t *testing.T) {
	got := Porcelain([]byte("R  new.go\x00old.go\x00 M other.go\x00"), "")
	if _, ok := got["old.go"]; ok {
		t.Fatalf("rename source became an entry: %v", got)
	}
	if got["new.go"] != "R" || got["other.go"] != "M" {
		t.Fatalf("Porcelain = %v", got)
	}
}

func TestRawAndNumstat(t *testing.T) {
	raw := []byte(":100644 100644 aaa bbb M\x00keep.txt\x00:000000 100755 000 ccc A\x00new.sh\x00:100644 000000 ddd 000 D\x00gone.txt\x00")
	changes, order, err := Raw(raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"keep.txt", "new.sh", "gone.txt"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if changes["new.sh"].Kind != ChangeAdded || changes["new.sh"].Mode != "100755" {
		t.Fatalf("new.sh = %+v", changes["new.sh"])
	}
	// A delete carries the mode it had in the "from" tree; the "to" mode is zero.
	if changes["gone.txt"].Kind != ChangeDeleted || changes["gone.txt"].Mode != "100644" {
		t.Fatalf("gone.txt = %+v", changes["gone.txt"])
	}

	// Binary paths keep the -1/-1 Raw set, which is what Binary() reads.
	if err := Numstat([]byte("3\t1\tkeep.txt\x00-\t-\tnew.sh\x00"), changes); err != nil {
		t.Fatal(err)
	}
	if got := changes["keep.txt"]; got.Added != 3 || got.Removed != 1 || got.Binary() {
		t.Fatalf("keep.txt = %+v", got)
	}
	if !changes["new.sh"].Binary() {
		t.Fatalf("new.sh should be binary: %+v", changes["new.sh"])
	}
}

func TestRawRejectsAnUnreadableRecord(t *testing.T) {
	if _, _, err := Raw([]byte(":100644 100644\x00keep.txt\x00")); err == nil {
		t.Fatal("a short raw record was accepted")
	}
}
