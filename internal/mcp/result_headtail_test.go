package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"devkit/internal/harness"
	"devkit/internal/usagemetrics"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- the cutter ------------------------------------------------------------

func textResult(blocks ...string) *sdk.CallToolResult {
	r := &sdk.CallToolResult{}
	for _, b := range blocks {
		r.Content = append(r.Content, &sdk.TextContent{Text: b})
	}
	return r
}

func blocksOf(r *sdk.CallToolResult) []string {
	var out []string
	for _, c := range r.Content {
		out = append(out, c.(*sdk.TextContent).Text)
	}
	return out
}

func totalBytes(parts []string) int {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	return n
}

// checkHeadTail asserts what every head-and-tail cut must be: within the budget
// with its marker, valid UTF-8, the original's beginning then one marker then the
// original's end — in order, no byte twice — and an omitted count that is exact.
func checkHeadTail(t *testing.T, name string, blocks []string, budget int) *sdk.CallToolResult {
	t.Helper()
	r := textResult(blocks...)
	original := strings.Join(blocks, "")
	kind := limitResult("some.tool", r, budget, harness.CutHeadTail)
	if len(original) <= budget {
		if kind != cutNone || strings.Join(blocksOf(r), "") != original {
			t.Fatalf("%s: a result within the budget must be returned unchanged (kind %q)", name, kind)
		}
		return r
	}
	if kind != cutHeadTail {
		t.Fatalf("%s: kind = %q, want headTail", name, kind)
	}
	parts := blocksOf(r)
	if got := totalBytes(parts); got > budget {
		t.Fatalf("%s: emitted %d bytes with its marker, budget %d", name, got, budget)
	}
	markerAt := -1
	for i, p := range parts {
		if !utf8.ValidString(p) {
			t.Fatalf("%s: block %d is not valid UTF-8: %q", name, i, p)
		}
		if strings.Contains(p, "bytes omitted from the middle") {
			if markerAt >= 0 {
				t.Fatalf("%s: more than one marker", name)
			}
			markerAt = i
		}
	}
	if markerAt < 0 {
		t.Fatalf("%s: no marker between the ends", name)
	}
	head, tail := strings.Join(parts[:markerAt], ""), strings.Join(parts[markerAt+1:], "")
	if !strings.HasPrefix(original, head) || !strings.HasSuffix(original, tail) {
		t.Fatalf("%s: the ends are not the original's beginning and end:\n head %q\n tail %q", name, head, tail)
	}
	if len(head)+len(tail) > len(original) {
		t.Fatalf("%s: the ends overlap", name)
	}
	var omitted int
	if _, err := fmt.Sscanf(strings.TrimPrefix(parts[markerAt], "\n[... "), "%d bytes omitted", &omitted); err != nil || omitted != len(original)-len(head)-len(tail) {
		t.Fatalf("%s: marker says %d omitted, truth is %d (%v)", name, omitted, len(original)-len(head)-len(tail), err)
	}
	if head == "" || tail == "" {
		t.Fatalf("%s: both ends must be kept (head %d bytes, tail %d)", name, len(head), len(tail))
	}
	if r.StructuredContent != nil {
		t.Fatalf("%s: structured content held the whole result", name)
	}
	return r
}

func lines(n int, f func(i int) string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(f(i))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestHeadTailKeepsBothEndsOfPlainTextWithinTheBudget(t *testing.T) {
	long := lines(2000, func(i int) string { return fmt.Sprintf("line %04d the quick brown fox jumps over the lazy dog", i) })
	build := long + "FAIL: exit status 1\n"
	crlf := strings.ReplaceAll(long, "\n", "\r\n") + "FAIL\r\n"
	vi := strings.Repeat("Tiếng Việt có dấu 🙂 — xin chào thế giới! ", 600)
	for name, blocks := range map[string][]string{
		"log with a failure at the end": {build},
		"no newline at all":             {strings.Repeat("abcdefghij", 5000)},
		"crlf":                          {crlf},
		"vietnamese and emoji":          {vi},
		"many blocks":                   {long[:9000], long[9000:20000], long[20000:], "tail block with the verdict\n"},
		"a huge middle":                 {"HEAD\n" + strings.Repeat("x", 200000) + "\nTAIL\n"},
		"many small blocks":             manyBlocks(400),
	} {
		for _, budget := range []int{2 * 1024, 4 * 1024, 24 * 1024, 64 * 1024} {
			checkHeadTail(t, fmt.Sprintf("%s @%d", name, budget), blocks, budget)
		}
	}
}

func manyBlocks(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("block %03d: %s\n", i, strings.Repeat("·", 40)) // multi-byte runes in every block
	}
	return out
}

func TestHeadTailPreservesTheErrorAtTheEnd(t *testing.T) {
	r := checkHeadTail(t, "verdict", []string{lines(3000, func(i int) string { return fmt.Sprintf("compiling unit %d", i) }) + "error: build failed (exit 2)\n"}, 8*1024)
	if !strings.Contains(strings.Join(blocksOf(r), ""), "error: build failed (exit 2)") {
		t.Fatal("the useful summary at the end of a long output was cut away")
	}
	if !strings.Contains(strings.Join(blocksOf(r), ""), "compiling unit 0\n") {
		t.Fatal("the beginning was cut away")
	}
}

func TestHeadTailCutsOnUTF8AndLineBoundariesOnly(t *testing.T) {
	// A budget that lands inside a multi-byte rune at either end, over many sizes.
	text := strings.Repeat("ế🙂é", 4000)
	for budget := 2048; budget < 2048+64; budget++ {
		checkHeadTail(t, fmt.Sprintf("rune split @%d", budget), []string{text}, budget)
	}
	// A line break near the edge of each allowance is preferred to a mid-line cut.
	r := checkHeadTail(t, "lines", []string{lines(4000, func(i int) string { return fmt.Sprintf("row-%04d", i) })}, 4096)
	parts := blocksOf(r)
	if head := parts[0]; strings.HasSuffix(head, "\n") || !strings.HasSuffix(head, "9") && !strings.Contains(head[strings.LastIndexByte(head, '\n')+1:], "row-") {
		t.Fatalf("head ends mid-line: %q", head[len(head)-12:])
	}
	if tail := parts[len(parts)-1]; !strings.HasPrefix(tail, "row-") {
		t.Fatalf("tail starts mid-line: %q", tail[:12])
	}
}

func TestHeadTailIsExactAtTheLimitAndDeterministic(t *testing.T) {
	for _, n := range []int{ResultBudget - 1, ResultBudget, ResultBudget + 1} {
		blocks := []string{strings.Repeat("a", n)}
		r := textResult(blocks...)
		kind := limitResult("some.tool", r, ResultBudget, harness.CutHeadTail)
		if (n <= ResultBudget) != (kind == cutNone) {
			t.Errorf("%d bytes: kind %q", n, kind)
		}
	}
	in := []string{lines(5000, func(i int) string { return fmt.Sprint("n", i) })}
	a, b := textResult(in...), textResult(in...)
	limitResult("x", a, 8192, harness.CutHeadTail)
	limitResult("x", b, 8192, harness.CutHeadTail)
	if strings.Join(blocksOf(a), "|") != strings.Join(blocksOf(b), "|") {
		t.Fatal("the same result must cut to the same bytes")
	}
}

// One budget for the whole result: several blocks do not each get their own.
func TestHeadTailSpendsOneBudgetAcrossBlocks(t *testing.T) {
	blocks := make([]string, 30)
	for i := range blocks {
		blocks[i] = lines(80, func(j int) string { return fmt.Sprintf("b%02d line %03d", i, j) })
	}
	r := checkHeadTail(t, "thirty blocks", blocks, 4096)
	if got := totalBytes(blocksOf(r)); got > 4096 {
		t.Fatalf("%d bytes for a 4096-byte budget", got)
	}
}

func TestHeadTailKeepsMetadataOfSurvivingBlocks(t *testing.T) {
	r := &sdk.CallToolResult{Content: []sdk.Content{
		&sdk.TextContent{Text: lines(500, func(i int) string { return fmt.Sprint("a", i, strings.Repeat("-", 30)) }), Meta: sdk.Meta{"k": "head"}},
		&sdk.TextContent{Text: lines(500, func(i int) string { return fmt.Sprint("z", i, strings.Repeat("-", 30)) }), Meta: sdk.Meta{"k": "tail"}},
	}}
	limitResult("x", r, 4096, harness.CutHeadTail)
	first, last := r.Content[0].(*sdk.TextContent), r.Content[len(r.Content)-1].(*sdk.TextContent)
	if first.Meta["k"] != "head" || last.Meta["k"] != "tail" {
		t.Fatalf("metadata did not follow its block: %v / %v", first.Meta, last.Meta)
	}
}

func TestHeadTailNeverTouchesStructuredNonTextOrHeadResults(t *testing.T) {
	// Structured JSON keeps its structural shrink under either mode, with the cursor rule.
	for _, mode := range []harness.CutMode{harness.CutHead, harness.CutHeadTail} {
		rows := make([]bigRow, 800)
		for i := range rows {
			rows[i] = bigRow{NodeID: fmt.Sprint("n", i), Label: strings.Repeat("l", 40), FilePath: strings.Repeat("p", 60), Line: i}
		}
		r := structuredResult(bigPage{Result: rows, NextCursor: "cur"})
		if kind := limitResult("codegraph.searchSymbol", r, ResultBudget, mode); kind != cutStructured {
			t.Fatalf("mode %q: structured result handled by %q", mode, kind)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(blocksOf(r)[0]), &got); err != nil {
			t.Fatalf("mode %q: the cut result is no longer valid JSON: %v", mode, err)
		}
		if _, ok := got["nextCursor"]; ok {
			t.Fatalf("mode %q: a cursor that would skip the rows left out survived", mode)
		}
		if _, ok := got["_truncated"]; !ok {
			t.Fatalf("mode %q: no truncation note", mode)
		}
	}
	// An image is never cut.
	img := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("x", 100000)}, &sdk.ImageContent{Data: []byte("png"), MIMEType: "image/png"}}}
	if kind := limitResult("some.tool", img, 2048, harness.CutHeadTail); kind != cutNone || len(img.Content) != 2 {
		t.Fatalf("non-text result was cut: %q", kind)
	}
	// The head mode is exactly what it was.
	head := textResult(strings.Repeat("abc\n", 30000))
	if kind := limitResult("some.tool", head, ResultBudget, harness.CutHead); kind != cutHead || !strings.Contains(blocksOf(head)[0], "Result cut to the 24 KB budget") {
		t.Fatalf("head cut changed: %q", kind)
	}
}

func structuredResult(v any) *sdk.CallToolResult {
	raw, _ := json.Marshal(v)
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(raw)}}, StructuredContent: json.RawMessage(raw)}
}

// ---- through the real SDK, under the policy the call was admitted with -------

func registerProbes(slow chan struct{}, entered chan struct{}) func(*Server) {
	big := func(tag string) string {
		return lines(3000, func(i int) string { return fmt.Sprintf("%s row %04d ........................................", tag, i) }) + "VERDICT: " + tag + " done\n"
	}
	return func(s *Server) {
		s.extraTools = func(ms *sdk.Server) {
			sdk.AddTool(ms, &sdk.Tool{Name: "probe.plain"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
				return textResult(big("plain")), nil, nil
			})
			sdk.AddTool(ms, &sdk.Tool{Name: "probe.slow"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
				entered <- struct{}{}
				<-slow
				return textResult(big("slow")), nil, nil
			})
			sdk.AddTool(ms, &sdk.Tool{Name: "probe.broken"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
				res := textResult(big("broken"))
				res.IsError = true
				return res, nil, nil
			})
			sdk.AddTool(ms, &sdk.Tool{Name: "probe.rows"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
				rows := make([]bigRow, 900)
				for i := range rows {
					rows[i] = bigRow{NodeID: fmt.Sprint("n", i), Label: strings.Repeat("l", 40), FilePath: strings.Repeat("p", 60), Line: i}
				}
				raw, _ := json.Marshal(bigPage{Result: rows, NextCursor: "next"})
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(raw)}}, StructuredContent: json.RawMessage(raw)}, nil, nil
			})
		}
	}
}

func budgetPolicy(group string, rule harness.BudgetRule, tools map[string]harness.BudgetRule) func(*harness.Inputs) {
	return func(in *harness.Inputs) {
		in.Workspace = &harness.Profile{Schema: 1, Revision: 5, ResultBudget: &harness.BudgetProfile{
			Groups: map[string]harness.BudgetRule{group: rule}, Tools: tools,
		}}
	}
}

func ptrInt(n int) *int { return &n }

func TestARealSDKClientGetsTheConfiguredCutAndTheRecordsSayWhatRan(t *testing.T) {
	r := newServingRig(t, registerProbes(nil, nil))
	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(budgetPolicy("probe", harness.BudgetRule{Bytes: ptrInt(4096), Cut: harness.CutHeadTail}, map[string]harness.BudgetRule{
		"probe.broken": {Bytes: ptrInt(2048)}, // size only: the mode still comes from the group
	})))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	call := func(tool string) *sdk.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: tool, Arguments: map[string]any{}})
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		return res
	}

	plain := call("probe.plain")
	text := resultText(plain)
	if len(text) > 4096 || !strings.Contains(text, "bytes omitted from the middle") || !strings.Contains(text, "plain row 0000") || !strings.Contains(text, "VERDICT: plain done") {
		t.Fatalf("headTail over the wire: %d bytes, ends kept = %v/%v", len(text), strings.Contains(text, "plain row 0000"), strings.Contains(text, "VERDICT: plain done"))
	}

	// A tool error is never cut, whatever the mode.
	broken := call("probe.broken")
	if !broken.IsError || strings.Contains(resultText(broken), "omitted from the middle") || !strings.Contains(resultText(broken), "VERDICT: broken done") {
		t.Fatalf("an error result must pass whole: error=%v", broken.IsError)
	}

	// Structured rows keep the structural shrink and valid JSON under headTail.
	rows := call("probe.rows")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(resultText(rows)), &parsed); err != nil || parsed["_truncated"] == nil || parsed["nextCursor"] != nil {
		t.Fatalf("structured result under headTail: %v %v", err, parsed["_truncated"])
	}

	modes := map[string]string{}
	r.mu.Lock()
	for _, o := range r.records {
		modes[o.Tool] = o.CutMode
	}
	r.mu.Unlock()
	want := map[string]string{"probe.plain": usagemetrics.CutHeadTail, "probe.broken": usagemetrics.CutError, "probe.rows": usagemetrics.CutStructured}
	for tool, mode := range want {
		if modes[tool] != mode {
			t.Errorf("%s was measured as %q, want %q", tool, modes[tool], mode)
		}
	}
}

// A call that was admitted under one policy finishes under it, even when a new
// policy (a different mode and size) is published while it runs; the next call
// takes the new one. The saved profile is never read again on the way out.
func TestACallAdmittedBeforeAPublicationFinishesUnderThePolicyItLoaded(t *testing.T) {
	slow, entered := make(chan struct{}), make(chan struct{}, 4)
	r := newServingRig(t, registerProbes(slow, entered))
	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(budgetPolicy("probe", harness.BudgetRule{Bytes: ptrInt(4096), Cut: harness.CutHeadTail}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)

	type answer struct {
		res *sdk.CallToolResult
		err error
	}
	done := make(chan answer, 1)
	go func() {
		res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "probe.slow", Arguments: map[string]any{}})
		done <- answer{res, err}
	}()
	<-entered // admitted under headTail @4 KiB

	next := r.resolve(budgetPolicy("probe", harness.BudgetRule{Bytes: ptrInt(2048), Cut: harness.CutHead}, nil))
	tk, err := ep.Live.Quarantine()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ep.Live.Install(tk, next, harness.ServingIdentity{Fingerprint: next.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	close(slow)
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	old := resultText(got.res)
	if len(old) > 4096 || len(old) <= 2048 || !strings.Contains(old, "omitted from the middle") || !strings.Contains(old, "VERDICT: slow done") {
		t.Fatalf("the admitted call must finish headTail at 4 KiB, got %d bytes", len(old))
	}

	fresh, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "probe.plain", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if text := resultText(fresh); len(text) > 2048 || strings.Contains(text, "omitted from the middle") || !strings.Contains(text, "Result cut to the 2 KB budget") {
		t.Fatalf("the next call must take the published head @2 KiB policy, got %d bytes", len(text))
	}

	modes := map[string]int{}
	r.mu.Lock()
	for _, o := range r.records {
		modes[o.Tool+"="+o.CutMode]++
	}
	r.mu.Unlock()
	if modes["probe.slow="+usagemetrics.CutHeadTail] != 1 || modes["probe.plain="+usagemetrics.CutHead] != 1 {
		t.Fatalf("each call is measured under the algorithm it ran: %v", modes)
	}
}

// The terminal and legacy endpoints have no policy and stay head-only.
func TestWithoutAPolicyTheCutIsTheHeadCut(t *testing.T) {
	r := newServingRig(t, registerProbes(nil, nil))
	ep, err := r.host.Open(sidA)
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "probe.plain", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if text := resultText(res); !strings.Contains(text, "Result cut to the 24 KB budget") || strings.Contains(text, "omitted from the middle") {
		t.Fatalf("legacy cut changed: %q", text[len(text)-200:])
	}
}
