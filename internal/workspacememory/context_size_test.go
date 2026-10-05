package workspacememory_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/workspacememory"
)

// TestContextIsSmallerThanTheJSONItReplaced renders the index of a workspace
// with 50 typical entries and compares it with the JSON snapshot it replaced,
// capped as that one was, so a regression in what a send costs fails here
// instead of showing up on the bill.
func TestContextIsSmallerThanTheJSONItReplaced(t *testing.T) {
	b := t
	s, _, _ := setup(t)
	for i := range 50 {
		in := input()
		in.Title = fmt.Sprintf("Decision %d: how the sync client handles conflicts", i)
		in.Summary = strings.Repeat("The sync client keeps both sides and asks the user which wins. ", 4)
		in.Body = strings.Repeat("long body ", 500)
		if _, err := s.Create(ctx, "a", in, workspacememory.Source{}); err != nil {
			b.Fatal(err)
		}
	}
	text, err := s.Context(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := s.List(ctx, "a", "", 100, 0)
	type item struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Title    string `json:"title"`
		Summary  string `json:"summary"`
		Revision int64  `json:"revision"`
	}
	old := struct {
		Entries []item `json:"entries"`
		Omitted bool   `json:"omitted"`
	}{Entries: []item{}}
	oldBytes := 0
	for _, e := range page.Entries {
		old.Entries = append(old.Entries, item{e.ID, e.Kind, e.Title, e.Summary, e.Revision})
		raw, _ := json.Marshal(old)
		if len(raw) > workspacememory.MaxContextBytes {
			old.Entries = old.Entries[:len(old.Entries)-1]
			break
		}
		oldBytes = len(raw)
	}
	newEntries := strings.Count(text, "\n- ") + 1
	t.Logf("same 8 KiB cap: index lists %d of 50 entries in %d bytes, the JSON snapshot %d in %d", newEntries, len(text), len(old.Entries), oldBytes)
	if newEntries <= len(old.Entries) {
		t.Fatalf("index fits %d entries, the JSON it replaced fit %d", newEntries, len(old.Entries))
	}
	// What each entry costs, which is what a workspace's size multiplies.
	if perNew, perOld := len(text)/newEntries, oldBytes/len(old.Entries); perNew >= perOld {
		t.Fatalf("an entry costs %d bytes in the index, %d in the JSON", perNew, perOld)
	}
}
