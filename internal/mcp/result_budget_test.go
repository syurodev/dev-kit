package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/harness"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type bigRow struct {
	NodeID   string `json:"nodeId"`
	Label    string `json:"label"`
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
}

type bigPage struct {
	Result     []bigRow `json:"result"`
	Total      int      `json:"total"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

func bigSearchPage(rows int) bigPage {
	page := bigPage{Total: 9000, NextCursor: "abc"}
	for i := 0; i < rows; i++ {
		page.Result = append(page.Result, bigRow{
			NodeID:   fmt.Sprintf("com.acme.billing.internal.service.InvoiceReconciliationService#reconcile%03d(java.lang.String,java.util.List)", i),
			Label:    fmt.Sprintf("InvoiceReconciliationService.reconcile%03d", i),
			FilePath: "billing-service/src/main/java/com/acme/billing/internal/service/InvoiceReconciliationService.java",
			Line:     100 + i,
		})
	}
	return page
}

func resultFor(t *testing.T, v any) *sdk.CallToolResult {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(raw)}}, StructuredContent: json.RawMessage(raw)}
}

func textOf(r *sdk.CallToolResult) int {
	n := 0
	for _, c := range r.Content {
		if t, ok := c.(*sdk.TextContent); ok {
			n += len(t.Text)
		}
	}
	return n
}

func TestResultUnderBudgetIsUntouched(t *testing.T) {
	r := resultFor(t, bigSearchPage(10))
	before := r.Content[0].(*sdk.TextContent).Text
	limitResult("codegraph.searchSymbol", r, ResultBudget, harness.CutHead)
	if r.Content[0].(*sdk.TextContent).Text != before || strings.Contains(before, "_truncated") {
		t.Fatal("a result inside the budget changed")
	}
}

func TestStructuredResultIsCutAtItemsAndStaysAnObject(t *testing.T) {
	page := bigSearchPage(200)
	r := resultFor(t, page)
	before := textOf(r)
	limitResult("codegraph.searchSymbol", r, ResultBudget, harness.CutHead)
	after := textOf(r)
	t.Logf("codegraph.searchSymbol, 200 matches: %d -> %d bytes", before, after)
	if before <= ResultBudget || after > ResultBudget {
		t.Fatalf("%d -> %d, budget %d", before, after, ResultBudget)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(r.Content[0].(*sdk.TextContent).Text), &got); err != nil {
		t.Fatalf("the cut result is not JSON: %v", err)
	}
	rows := got["result"].([]any)
	if len(rows) == 0 || len(rows) >= 200 {
		t.Fatalf("kept %d rows", len(rows))
	}
	first := rows[0].(map[string]any)
	if first["label"] != "InvoiceReconciliationService.reconcile000" {
		t.Errorf("not cut from the tail: %v", first)
	}
	if got["total"].(float64) != 9000 {
		t.Error("the total, which says how much there is, was lost")
	}
	if _, has := got["nextCursor"]; has {
		t.Error("a cursor beside cut rows would skip them silently")
	}
	note, _ := got["_truncated"].(string)
	for _, want := range []string{"24 KB", fmt.Sprintf("%d more items of \"result\"", 200-len(rows)), "smaller limit", "nextCursor removed"} {
		if !strings.Contains(note, want) {
			t.Errorf("marker %q lacks %q", note, want)
		}
	}
	// The structured value is the same object as the text, for a client that reads it instead.
	sc, _ := json.Marshal(r.StructuredContent)
	if string(sc) != r.Content[0].(*sdk.TextContent).Text {
		t.Error("structuredContent and the text diverged")
	}
}

func TestACutRowSetSaysItIsTruncated(t *testing.T) {
	rows := make([][]string, 0, 2000)
	for i := 0; i < 2000; i++ {
		rows = append(rows, []string{fmt.Sprint(i), "a fairly long cell value to take some room", "x"})
	}
	r := resultFor(t, map[string]any{"columns": []string{"id", "v", "w"}, "rows": rows, "row_count": 2000, "truncated": false})
	limitResult("db.query", r, ResultBudget, harness.CutHead)
	var got map[string]any
	if err := json.Unmarshal([]byte(r.Content[0].(*sdk.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got["truncated"] != true || got["row_count"].(float64) != 2000 || len(got["rows"].([]any)) >= 2000 {
		t.Errorf("truncated=%v row_count=%v rows=%d", got["truncated"], got["row_count"], len(got["rows"].([]any)))
	}
	if !strings.Contains(got["_truncated"].(string), "maxRows") {
		t.Errorf("db.query's hint does not name maxRows: %v", got["_truncated"])
	}
}

func TestOnePageUnderTheBudgetKeepsItsCursor(t *testing.T) {
	// 25 matches (the default page) fit: nothing cut, so the cursor stays.
	r := resultFor(t, bigSearchPage(25))
	limitResult("codegraph.searchSymbol", r, ResultBudget, harness.CutHead)
	if !strings.Contains(r.Content[0].(*sdk.TextContent).Text, `"nextCursor":"abc"`) {
		t.Fatalf("a default page lost its cursor: %d bytes", textOf(r))
	}
}

func TestOneHugeStringIsShortenedNotLost(t *testing.T) {
	r := resultFor(t, map[string]any{"path": "a.txt", "content": strings.Repeat("line of text\n", 6000)})
	limitResult("some.tool", r, ResultBudget, harness.CutHead)
	if textOf(r) > ResultBudget {
		t.Fatalf("still %d bytes", textOf(r))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(r.Content[0].(*sdk.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got["path"] != "a.txt" || !strings.Contains(got["_truncated"].(string), "shortened") || !strings.Contains(got["_truncated"].(string), "Ask for less") {
		t.Errorf("got %v", got["_truncated"])
	}
	if !strings.HasSuffix(got["content"].(string), "…") {
		t.Error("a cut text does not say it was cut")
	}
}

func TestPlainTextIsCutAtALineAndSaysHowMuchWasLeft(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&b, "line %05d of a long answer\n", i)
	}
	full := b.String()
	r := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: full}}}
	limitResult("git.diff", r, ResultBudget, harness.CutHead)
	text := r.Content[0].(*sdk.TextContent).Text
	if len(text) > ResultBudget {
		t.Fatalf("%d bytes", len(text))
	}
	body, marker, _ := strings.Cut(text, "\n[Result cut")
	if !strings.HasSuffix(body, "of a long answer") {
		t.Errorf("cut mid-line: ...%q", body[len(body)-30:])
	}
	if !strings.Contains(marker, fmt.Sprintf("%d bytes left out", len(full)-len(body))) || !strings.Contains(marker, "rev") {
		t.Errorf("marker = %q", marker)
	}
}

func TestMultibyteTextIsNotCutInsideARune(t *testing.T) {
	r := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("é", 20000)}}}
	limitResult("x", r, ResultBudget, harness.CutHead)
	if text := r.Content[0].(*sdk.TextContent).Text; !strings.Contains(text, "[Result cut") || strings.ContainsRune(text, '�') {
		t.Fatal("broken rune or no marker")
	}
}

func TestImagesAreNeverCut(t *testing.T) {
	r := &sdk.CallToolResult{Content: []sdk.Content{
		&sdk.TextContent{Text: strings.Repeat("caption ", 5000)},
		&sdk.ImageContent{Data: []byte(strings.Repeat("x", 50000)), MIMEType: "image/png"},
	}}
	before := textOf(r)
	limitResult("pdf.viewPages", r, ResultBudget, harness.CutHead)
	if textOf(r) != before || len(r.Content) != 2 {
		t.Fatal("a result with an image was cut")
	}
}

func TestErrorsAreNeverCut(t *testing.T) {
	call := &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("e", 40000)}}}
	b := withResultBudget(ResultBudget)(func(context.Context, string, sdk.Request) (sdk.Result, error) { return call, nil })
	res, _ := b(context.Background(), "tools/call", &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "x"}})
	if textOf(res.(*sdk.CallToolResult)) != 40000 {
		t.Fatal("an error was cut")
	}
}

// Exempt tools must exist (a typo would exempt nothing and cut a PDF), and the
// exemption must be honoured by the wrapper itself.
func TestBudgetExemptionsAreRealToolsWithAReason(t *testing.T) {
	have := map[string]bool{}
	for _, tool := range listAllTools(t) {
		have[tool.Name] = true
	}
	for name, why := range budgetExempt {
		if !have[name] {
			t.Errorf("exempt tool %q is not registered", name)
		}
		if why == "" {
			t.Errorf("exempt tool %q has no reason", name)
		}
	}
	for _, must := range []string{"pdf.viewPages", "pdf.readText", "codegraph.readFile"} {
		if _, ok := budgetExempt[must]; !ok {
			t.Errorf("%s must stay whole", must)
		}
	}
	big := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("p", 40000)}}}
	next := func(context.Context, string, sdk.Request) (sdk.Result, error) { return big, nil }
	run := func(name string) int {
		big.Content = []sdk.Content{&sdk.TextContent{Text: strings.Repeat("p", 40000)}}
		res, _ := withResultBudget(ResultBudget)(next)(context.Background(), "tools/call", &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: name}})
		return textOf(res.(*sdk.CallToolResult))
	}
	if n := run("pdf.readText"); n != 40000 {
		t.Errorf("pdf.readText was cut to %d", n)
	}
	if n := run("notes.list"); n > ResultBudget {
		t.Errorf("an ordinary tool kept %d bytes", n)
	}
}

// The whole path: a registered tool, the server's own middleware, a client.
func TestServerCutsAnOversizedToolResult(t *testing.T) {
	ms := sdk.NewServer(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	ms.AddReceivingMiddleware(withResultBudget(ResultBudget))
	sdk.AddTool(ms, &sdk.Tool{Name: "big.page"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
		return nil, bigSearchPage(300), nil
	})
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	ss, err := ms.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "big.page"})
	if err != nil {
		t.Fatal(err)
	}
	if n := textOf(res); n > ResultBudget || n == 0 {
		t.Fatalf("client received %d bytes", n)
	}
	if !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "_truncated") {
		t.Fatal("no marker reached the client")
	}
}
