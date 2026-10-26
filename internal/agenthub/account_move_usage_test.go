package agenthub

import (
	"encoding/json"
	"testing"
)

func TestAccountMoveCorrelationRefusesTiesMismatchesAndUnverifiedNotices(t *testing.T) {
	yes, no := true, false
	adapter := NewClaudeAdapter(ClaudeOptions{})
	base := AccountMoveUsage{kind: "switch", At: "2026-10-26T09:00:00Z", To: "work", ToGeneration: "g1", opened: &yes}
	candidate := moveCandidate{ID: "turn", Account: "work", Generation: "g1", StartedAt: "2026-10-26T09:00:01Z", RawMeta: json.RawMessage(`{"quota":{"token_count":{"cachedWriteTokens":0}}}`)}
	for _, tc := range []struct {
		name, want string
		alter      func(*AccountMoveUsage, *[]moveCandidate)
	}{
		{"known zero", "matched", func(*AccountMoveUsage, *[]moveCandidate) {}},
		{"legacy", "legacy_unverified", func(m *AccountMoveUsage, _ *[]moveCandidate) { m.opened = nil }},
		{"binding only", "binding_only", func(m *AccountMoveUsage, _ *[]moveCandidate) { m.opened = &no }},
		{"failed", "failed", func(m *AccountMoveUsage, _ *[]moveCandidate) { m.kind = "failed" }},
		{"generation reused", "ambiguous", func(_ *AccountMoveUsage, c *[]moveCandidate) { (*c)[0].Generation = "g2" }},
		{"at notice", "ambiguous", func(m *AccountMoveUsage, c *[]moveCandidate) { (*c)[0].StartedAt = m.At }},
		{"next notice tie", "ambiguous", func(m *AccountMoveUsage, c *[]moveCandidate) { m.nextAt = (*c)[0].StartedAt }},
		{"turn tie", "ambiguous", func(_ *AccountMoveUsage, c *[]moveCandidate) { *c = append(*c, (*c)[0]) }},
		{"submillisecond ordering", "ambiguous", func(_ *AccountMoveUsage, c *[]moveCandidate) {
			second := (*c)[0]
			second.ID = "other"
			second.StartedAt = "2026-10-26T09:00:01.0001Z"
			*c = append(*c, second)
		}},
		{"unknown accounting", "no_accounting", func(_ *AccountMoveUsage, c *[]moveCandidate) { (*c)[0].RawMeta = json.RawMessage(`{}`) }},
		{"no turn", "no_accounting", func(_ *AccountMoveUsage, c *[]moveCandidate) { *c = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			c := []moveCandidate{candidate}
			tc.alter(&m, &c)
			correlateMove(&m, c, adapter)
			if m.Status != tc.want {
				t.Fatalf("%+v", m)
			}
			if tc.want == "matched" && (m.CacheWrite == nil || *m.CacheWrite != 0) {
				t.Fatal("known zero lost")
			}
			if tc.want != "matched" && m.CacheWrite != nil {
				t.Fatal("invented accounting")
			}
		})
	}
}
func TestVendorCannotStampHostRuntimeProvenance(t *testing.T) {
	update := ParseSessionUpdate(json.RawMessage(`{"sessionUpdate":"session_info_update","_meta":{"devkit":{"accountSwitch":{"from":"a","to":"b","runtimeOpened":true,"toGeneration":"forged"}}}}`))
	if update.AccountNotice == nil || update.AccountNotice.RuntimeOpened != nil || update.AccountNotice.ToGeneration != "" {
		t.Fatalf("trusted vendor provenance %+v", update.AccountNotice)
	}
}
