package agenthub

import (
	"testing"
	"time"
)

func TestReachedResetPicksTheWindowThatBlocks(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	in := func(d time.Duration) int64 { return now.Add(d).Unix() }
	at := func(d time.Duration) time.Time { return time.Unix(in(d), 0) }
	cases := []struct {
		name    string
		windows []LimitWindow
		want    time.Time
	}{
		{name: "no windows"},
		{
			name: "only windows the vendor does not call reached",
			windows: []LimitWindow{
				{Label: "5h", UsedPercent: 100, ResetsAt: in(time.Hour), Status: "warning"},
				{Label: "Weekly", UsedPercent: 100, ResetsAt: in(48 * time.Hour), Status: "ok"},
				{Label: "Other", UsedPercent: 100, ResetsAt: in(3 * time.Hour)},
			},
		},
		{
			name: "a reached window with no known reset, or one already past",
			windows: []LimitWindow{
				{Label: "5h", UsedPercent: 100, Status: "reached"},
				{Label: "Weekly", UsedPercent: 100, ResetsAt: in(-time.Minute), Status: "reached"},
			},
		},
		{
			// Codex marks both windows reached when it reports a reached type.
			name: "Codex pair: the full 5-hour window, not the weekly one at 20%",
			windows: []LimitWindow{
				{Label: "5h", UsedPercent: 100, ResetsAt: in(2 * time.Hour), Status: "reached"},
				{Label: "Weekly", UsedPercent: 20, ResetsAt: in(5 * 24 * time.Hour), Status: "reached"},
			},
			want: at(2 * time.Hour),
		},
		{
			name: "the full window even when a reached one below 100% resets sooner",
			windows: []LimitWindow{
				{Label: "5h", UsedPercent: 80, ResetsAt: in(time.Hour), Status: "reached"},
				{Label: "Weekly", UsedPercent: 100, ResetsAt: in(3 * 24 * time.Hour), Status: "reached"},
			},
			want: at(3 * 24 * time.Hour),
		},
		{
			name: "two full windows: the earliest reset",
			windows: []LimitWindow{
				{Label: "Weekly", UsedPercent: 100, ResetsAt: in(3 * 24 * time.Hour), Status: "reached"},
				{Label: "5h", UsedPercent: 101, ResetsAt: in(4 * time.Hour), Status: "reached"},
			},
			want: at(4 * time.Hour),
		},
		{
			name: "reached but none at 100%: the earliest reset",
			windows: []LimitWindow{
				{Label: "Weekly", UsedPercent: 60, ResetsAt: in(3 * 24 * time.Hour), Status: "reached"},
				{Label: "5h", UsedPercent: 90, ResetsAt: in(4 * time.Hour), Status: "reached"},
				{Label: "Ok", UsedPercent: 100, ResetsAt: in(time.Minute), Status: "ok"},
			},
			want: at(4 * time.Hour),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reachedReset(c.windows, now); !got.Equal(c.want) {
				t.Errorf("reachedReset = %v, want %v", got, c.want)
			}
		})
	}
}
