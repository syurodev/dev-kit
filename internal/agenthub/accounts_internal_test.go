package agenthub

import (
	"reflect"
	"testing"
)

func TestMergeWindowsReport(t *testing.T) {
	w := func(label string, pct float64) LimitWindow { return LimitWindow{Label: label, UsedPercent: pct} }
	wr := func(label string, pct float64, resets int64, status string) LimitWindow {
		return LimitWindow{Label: label, UsedPercent: pct, ResetsAt: resets, Status: status}
	}
	for _, tc := range []struct {
		name       string
		old, fresh []LimitWindow
		want       []LimitWindow
	}{
		{"empty old", nil, []LimitWindow{w("a", 1)}, []LimitWindow{w("a", 1)}},
		{"replace keeps order", []LimitWindow{w("a", 1), w("b", 2)}, []LimitWindow{w("a", 9)}, []LimitWindow{w("a", 9), w("b", 2)}},
		{"new label last", []LimitWindow{w("a", 1), w("b", 2)}, []LimitWindow{w("c", 3)}, []LimitWindow{w("a", 1), w("b", 2), w("c", 3)}},
		{"a lower reading of the same window is stale",
			[]LimitWindow{wr("a", 80, 100, "")}, []LimitWindow{wr("a", 60, 100, "")}, []LimitWindow{wr("a", 80, 100, "")}},
		{"a stored reached window wins over a lower reading of it",
			[]LimitWindow{wr("a", 100, 100, "reached")}, []LimitWindow{wr("a", 40, 100, "allowed")}, []LimitWindow{wr("a", 100, 100, "reached")}},
		{"a higher reading of the same window replaces",
			[]LimitWindow{wr("a", 60, 100, "")}, []LimitWindow{wr("a", 80, 100, "allowed_warning")}, []LimitWindow{wr("a", 80, 100, "allowed_warning")}},
		{"an equal reading replaces (fresher status)",
			[]LimitWindow{wr("a", 80, 100, "")}, []LimitWindow{wr("a", 80, 100, "reached")}, []LimitWindow{wr("a", 80, 100, "reached")}},
		{"a lower reading with another reset is a new window",
			[]LimitWindow{wr("a", 100, 100, "reached")}, []LimitWindow{wr("a", 5, 200, "")}, []LimitWindow{wr("a", 5, 200, "")}},
		{"a lower reading with no reset replaces as before",
			[]LimitWindow{wr("a", 80, 100, "")}, []LimitWindow{wr("a", 10, 0, "")}, []LimitWindow{wr("a", 10, 0, "")}},
		{"a stored window with no reset is replaced by a lower reading",
			[]LimitWindow{wr("a", 80, 0, "")}, []LimitWindow{wr("a", 10, 100, "")}, []LimitWindow{wr("a", 10, 100, "")}},
		{"stale one among several, the others still merge",
			[]LimitWindow{wr("a", 80, 100, ""), wr("b", 2, 100, "")}, []LimitWindow{wr("a", 10, 100, ""), wr("b", 9, 100, ""), wr("c", 1, 100, "")},
			[]LimitWindow{wr("a", 80, 100, ""), wr("b", 9, 100, ""), wr("c", 1, 100, "")}},
		{"mixed", []LimitWindow{w("a", 1), w("b", 2)}, []LimitWindow{w("c", 3), w("b", 8)}, []LimitWindow{w("a", 1), w("b", 8), w("c", 3)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := append([]LimitWindow(nil), tc.old...)
			got, _ := mergeWindowsReport(tc.old, tc.fresh)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("merge = %+v, want %+v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.old, old) {
				t.Errorf("old was modified: %+v, was %+v", tc.old, old)
			}
		})
	}
}

func TestMergeWindowsReportSaysWhetherAnythingWasAccepted(t *testing.T) {
	stored := []LimitWindow{{Label: "5-hour", UsedPercent: 90, ResetsAt: 1000}}
	if _, accepted := mergeWindowsReport(stored, []LimitWindow{{Label: "5-hour", UsedPercent: 10, ResetsAt: 1000}}); accepted {
		t.Error("a lower reading of the same window is stale: nothing accepted")
	}
	if _, accepted := mergeWindowsReport(stored, []LimitWindow{{Label: "5-hour", UsedPercent: 95, ResetsAt: 1000}}); !accepted {
		t.Error("a higher reading is accepted")
	}
	if _, accepted := mergeWindowsReport(stored, []LimitWindow{{Label: "Weekly", UsedPercent: 1, ResetsAt: 2000}}); !accepted {
		t.Error("a new window is accepted")
	}
	if _, accepted := mergeWindowsReport(stored, nil); accepted {
		t.Error("an empty reading accepts nothing")
	}
}
