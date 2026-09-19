package codegraph

import (
	"reflect"
	"testing"
)

func TestParsePorcelain(t *testing.T) {
	out := []byte(" M services/refund/src/A.java\x00?? services/refund/src/B.java\x00R  services/refund/src/New.java\x00services/refund/src/Old.java\x00 D services/refund/pom.xml\x00M  other/C.java\x00")
	got := parsePorcelain(out, "services/refund/")
	want := map[string]string{
		"src/A.java":   "M",
		"src/B.java":   "A",
		"src/New.java": "R",
		"pom.xml":      "D",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePorcelain = %v, want %v", got, want)
	}
}

func TestParsePorcelainAtRepoRoot(t *testing.T) {
	got := parsePorcelain([]byte("A  src/main/New.java\x00"), "")
	if got["src/main/New.java"] != "A" {
		t.Fatalf("parsePorcelain = %v", got)
	}
}
