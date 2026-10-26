package agenthub_test

import (
	"encoding/json"
	"testing"

	"devkit/internal/agenthub"
)

// claudeMeta is the raw meta of a real Claude turn: the numbers sit under
// quota.token_count, with a per-model split of the same totals.
const claudeMeta = `{"quota":{"token_count":{"totalTokens":2440835,"inputTokens":8,"cachedInputTokens":1832413,"cachedWriteTokens":601633,"outputTokens":6781,"reasoningOutputTokens":0},"model_usage":[{"model":"claude-opus-5-5","token_count":{"totalTokens":2440835,"inputTokens":8,"cachedInputTokens":1832413,"cachedWriteTokens":601633,"outputTokens":6781,"reasoningOutputTokens":0}}]}}`

func usageJSONOf(t *testing.T, structured agenthub.NormalizedUsage, raw string) json.RawMessage {
	t.Helper()
	if raw != "" {
		structured.RawMeta = json.RawMessage(raw)
	}
	out, err := json.Marshal(structured)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func wantInt(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: unknown, want %d", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %d, want %d", name, *got, want)
	}
}

func wantUnknown(t *testing.T, name string, got *int) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %d, want unknown", name, *got)
	}
}

// A row stored while the normalizer did not understand the meta carries 0 for
// input and output next to the real numbers in the raw meta. The raw meta wins.
func TestDeriveTurnUsageIgnoresAFalseZeroNextToRealRawNumbers(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	stored := usageJSONOf(t, agenthub.NormalizedUsage{InputTokens: 0, OutputTokens: 0}, claudeMeta)
	got := agenthub.DeriveTurnUsage(adapter, stored, json.RawMessage(claudeMeta))
	wantInt(t, "total", got.Total, 2440835)
	wantInt(t, "fresh input", got.FreshInput, 8)
	wantInt(t, "output", got.Output, 6781)
	wantInt(t, "cache read", got.CacheRead, 1832413)
	wantInt(t, "cache write", got.CacheWrite, 601633)
	wantInt(t, "reasoning", got.Reasoning, 0)
	if len(got.Models) != 1 || got.Models[0].Model != "claude-opus-5-5" || got.Models[0].Total != 2440835 || got.SplitPartial {
		t.Errorf("models = %+v partial=%v", got.Models, got.SplitPartial)
	}
}

// A 0 the raw meta really says is a known 0, not a missing number.
func TestDeriveTurnUsageKeepsARealZero(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	raw := `{"quota":{"token_count":{"totalTokens":0,"inputTokens":0,"cachedInputTokens":0,"cachedWriteTokens":0,"outputTokens":0,"reasoningOutputTokens":0}}}`
	got := agenthub.DeriveTurnUsage(adapter, usageJSONOf(t, agenthub.NormalizedUsage{}, raw), json.RawMessage(raw))
	wantInt(t, "total", got.Total, 0)
	wantInt(t, "fresh input", got.FreshInput, 0)
	wantInt(t, "output", got.Output, 0)
	wantInt(t, "cache read", got.CacheRead, 0)
}

// A field the raw meta does not have is unknown, and the fields it has stay known.
func TestDeriveTurnUsageLeavesAnAbsentFieldUnknown(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	raw := `{"quota":{"token_count":{"totalTokens":100,"inputTokens":5,"outputTokens":7}}}`
	got := agenthub.DeriveTurnUsage(adapter, usageJSONOf(t, agenthub.NormalizedUsage{}, raw), json.RawMessage(raw))
	wantInt(t, "total", got.Total, 100)
	wantInt(t, "fresh input", got.FreshInput, 5)
	wantInt(t, "output", got.Output, 7)
	wantUnknown(t, "cache read", got.CacheRead)
	wantUnknown(t, "cache write", got.CacheWrite)
	wantUnknown(t, "reasoning", got.Reasoning)
}

// A meta that says nothing about tokens is no data, not a turn that cost 0.
func TestDeriveTurnUsageTreatsAMetaWithoutTokensAsUnknown(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	got := agenthub.DeriveTurnUsage(adapter, usageJSONOf(t, agenthub.NormalizedUsage{}, `{}`), json.RawMessage(`{}`))
	wantUnknown(t, "total", got.Total)
	wantUnknown(t, "fresh input", got.FreshInput)
	wantUnknown(t, "output", got.Output)
}

// With no raw meta in the row at all, the stored fields are all there is.
func TestDeriveTurnUsageFallsBackToStoredFieldsOnlyWithoutRawMeta(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	total, read, write := 5394506, 5379544, 11505
	stored := usageJSONOf(t, agenthub.NormalizedUsage{
		InputTokens: 12, OutputTokens: 3, TotalTokens: &total, CachedInputTokens: &read, CachedWriteTokens: &write,
	}, "")
	got := agenthub.DeriveTurnUsage(adapter, stored, nil)
	wantInt(t, "total", got.Total, 5394506)
	wantInt(t, "fresh input", got.FreshInput, 12)
	wantInt(t, "cache read", got.CacheRead, 5379544)

	zero := usageJSONOf(t, agenthub.NormalizedUsage{}, "")
	none := agenthub.DeriveTurnUsage(adapter, zero, nil)
	wantUnknown(t, "stored zeros without raw: total", none.Total)
	wantUnknown(t, "stored zeros without raw: fresh input", none.FreshInput)
}

// The raw meta is also kept inside the stored usage, for rows whose own raw column is empty.
func TestDeriveTurnUsageReadsTheRawMetaKeptInsideTheStoredUsage(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	stored := usageJSONOf(t, agenthub.NormalizedUsage{}, claudeMeta)
	got := agenthub.DeriveTurnUsage(adapter, stored, nil)
	wantInt(t, "total", got.Total, 2440835)
	wantInt(t, "output", got.Output, 6781)
}

// A malformed, larger or uncomparable split must be marked partial.
func TestDeriveTurnUsageMarksUnreadableOrUncomparableSplits(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	for _, raw := range []string{
		`{"quota":{"token_count":{"totalTokens":20},"model_usage":[{"model":"a","token_count":{}}]}}`,
		`{"quota":{"token_count":{"totalTokens":20},"model_usage":[{"model":"a","token_count":{"totalTokens":30}}]}}`,
		`{"quota":{"model_usage":[{"model":"a","token_count":{"totalTokens":10}}]}}`,
	} {
		if got := agenthub.DeriveTurnUsage(adapter, nil, json.RawMessage(raw)); !got.SplitPartial {
			t.Errorf("unreadable, larger or uncomparable split must be partial: %s", raw)
		}
	}
}

func TestDeriveTurnUsageMarksASplitThatDoesNotAddUpAsPartial(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	raw := `{"quota":{"token_count":{"totalTokens":20,"inputTokens":1,"outputTokens":1},"model_usage":[{"model":"a","token_count":{"totalTokens":10}}]}}`
	got := agenthub.DeriveTurnUsage(adapter, usageJSONOf(t, agenthub.NormalizedUsage{}, raw), json.RawMessage(raw))
	if len(got.Models) != 1 || !got.SplitPartial {
		t.Errorf("models = %+v partial=%v", got.Models, got.SplitPartial)
	}
	wantInt(t, "total", got.Total, 20)
}
