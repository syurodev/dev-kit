package agenthub

import (
	"reflect"
	"testing"
)

func TestMergeWindows(t *testing.T) {
	w := func(label string, pct float64) LimitWindow { return LimitWindow{Label: label, UsedPercent: pct} }
	for _, tc := range []struct {
		name       string
		old, fresh []LimitWindow
		want       []LimitWindow
	}{
		{"empty old", nil, []LimitWindow{w("a", 1)}, []LimitWindow{w("a", 1)}},
		{"replace keeps order", []LimitWindow{w("a", 1), w("b", 2)}, []LimitWindow{w("a", 9)}, []LimitWindow{w("a", 9), w("b", 2)}},
		{"new label last", []LimitWindow{w("a", 1), w("b", 2)}, []LimitWindow{w("c", 3)}, []LimitWindow{w("a", 1), w("b", 2), w("c", 3)}},
		{"mixed", []LimitWindow{w("a", 1), w("b", 2)}, []LimitWindow{w("c", 3), w("b", 8)}, []LimitWindow{w("a", 1), w("b", 8), w("c", 3)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := append([]LimitWindow(nil), tc.old...)
			got := mergeWindows(tc.old, tc.fresh)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("merge = %+v, want %+v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.old, old) {
				t.Errorf("old was modified: %+v, was %+v", tc.old, old)
			}
		})
	}
}
