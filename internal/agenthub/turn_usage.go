package agenthub

import (
	"bytes"
	"encoding/json"
)

// TurnUsage is what one turn spent. A nil field means the vendor's meta did
// not say, which is not the same as 0: a report counts such a turn as "no
// data" and never adds it as 0.
type TurnUsage struct {
	FreshInput *int
	CacheRead  *int
	CacheWrite *int
	Output     *int
	Reasoning  *int
	Total      *int
	// Models is the per-model split of the same totals. It is never added to
	// the fields above.
	Models []ModelTurnUsage
	// SplitPartial is set for unequal totals, unreadable entries, or a nonempty
	// split that cannot be compared because the turn total is unknown.
	SplitPartial bool
}

// ModelTurnUsage is one model's share of a turn's total.
type ModelTurnUsage struct {
	Model string
	Total int
}

func (u TurnUsage) known() bool {
	return u.FreshInput != nil || u.CacheRead != nil || u.CacheWrite != nil ||
		u.Output != nil || u.Reasoning != nil || u.Total != nil
}

// DeriveTurnUsage reads what a turn spent.
//
// The stored structured fields are not trusted: NormalizedUsage.InputTokens
// and OutputTokens are plain numbers that json.Marshal always writes, so a row
// stored while the normalizer did not understand the vendor's meta holds 0
// beside real numbers in the raw meta. The raw meta (the row's own column, else
// the copy kept inside the stored usage) is therefore normalized again with the
// adapter's current NormalizeUsage. The stored fields are used only when the
// row has no raw meta at all.
func DeriveTurnUsage(adapter VendorAdapter, usageJSON, rawMeta json.RawMessage) TurnUsage {
	meta := rawMeta
	if len(meta) == 0 {
		meta = metaKeptInUsage(usageJSON)
	}
	if len(meta) > 0 {
		n, err := adapter.NormalizeUsage(meta)
		if err != nil {
			return TurnUsage{}
		}
		u := usageFromNormalized(n)
		u.Models, u.SplitPartial = modelSplit(meta, u.Total)
		return u
	}
	var stored NormalizedUsage
	if len(usageJSON) == 0 || json.Unmarshal(usageJSON, &stored) != nil {
		return TurnUsage{}
	}
	return usageFromNormalized(stored)
}

// usageFromNormalized decides what a normalizer's answer knows. InputTokens and
// OutputTokens cannot say "absent", so an answer is known only if anything in it
// is: a pointer field, or a non-zero number. A vendor that reported nothing
// gives all zeros and nil pointers, and that is "no data".
func usageFromNormalized(n NormalizedUsage) TurnUsage {
	known := n.TotalTokens != nil || n.CachedInputTokens != nil || n.CachedWriteTokens != nil ||
		n.ReasoningOutputTokens != nil || n.InputTokens != 0 || n.OutputTokens != 0
	if !known {
		return TurnUsage{}
	}
	in, out := n.InputTokens, n.OutputTokens
	return TurnUsage{
		FreshInput: &in,
		CacheRead:  n.CachedInputTokens,
		CacheWrite: n.CachedWriteTokens,
		Output:     &out,
		Reasoning:  n.ReasoningOutputTokens,
		Total:      n.TotalTokens,
	}
}

func metaKeptInUsage(usageJSON json.RawMessage) json.RawMessage {
	var kept struct {
		RawMeta json.RawMessage
	}
	if len(usageJSON) == 0 || json.Unmarshal(usageJSON, &kept) != nil {
		return nil
	}
	if len(kept.RawMeta) == 0 || bytes.Equal(bytes.TrimSpace(kept.RawMeta), []byte("null")) {
		return nil
	}
	return kept.RawMeta
}

// modelSplit reads quota.model_usage, Claude's per-model breakdown of the turn
// total. An entry without a model or a total is skipped and makes the split partial.
func modelSplit(meta json.RawMessage, total *int) ([]ModelTurnUsage, bool) {
	var root struct {
		Quota *struct {
			ModelUsage []struct {
				Model      string `json:"model"`
				TokenCount *struct {
					TotalTokens *int `json:"totalTokens"`
				} `json:"token_count"`
			} `json:"model_usage"`
		} `json:"quota"`
	}
	if json.Unmarshal(meta, &root) != nil || root.Quota == nil {
		return nil, false
	}
	var out []ModelTurnUsage
	partial := false
	sum := 0
	for _, m := range root.Quota.ModelUsage {
		if m.Model == "" || m.TokenCount == nil || m.TokenCount.TotalTokens == nil {
			partial = true
			continue
		}
		out = append(out, ModelTurnUsage{Model: m.Model, Total: *m.TokenCount.TotalTokens})
		sum += *m.TokenCount.TotalTokens
	}
	if len(out) == 0 {
		return nil, partial
	}
	if total == nil || sum != *total {
		partial = true
	}
	return out, partial
}
