package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/pdfdoc"
	"devkit/internal/storage"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	pdfEngine     *pdfdoc.Engine
	pdfEngineOnce sync.Once
)

func sharedPDFEngine() *pdfdoc.Engine {
	pdfEngineOnce.Do(func() { pdfEngine = pdfdoc.New(pdfdoc.Options{}) })
	return pdfEngine
}

type pdfRig struct {
	srv    *Server
	caller *fakeCaller
	lib    *agenthub.DocumentLibrary
	dir    string
	a, b   string // two sessions; a holds doc1 (webprint) and doc2 (scan), b holds doc1 (mixed)
}

func newPDFRig(t *testing.T) *pdfRig {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := agenthub.NewSQLStore(db)
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	hub := agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	dir := filepath.Join(t.TempDir(), "docs")
	lib := agenthub.NewDocumentLibrary(store, dir, sharedPDFEngine())
	hub.Documents = lib

	cwd := t.TempDir()
	newSession := func() string {
		s, err := hub.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: cwd})
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	attach := func(session, name, fixture string) {
		data, err := os.ReadFile(filepath.Join("..", "pdfdoc", "testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(cwd, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := lib.Import(context.Background(), session, path); err != nil {
			t.Fatal(err)
		}
	}
	r := &pdfRig{lib: lib, dir: dir, caller: &fakeCaller{}}
	r.a, r.b = newSession(), newSession()
	attach(r.a, "Thông tư mẫu.pdf", "webprint.pdf")
	attach(r.a, "biên bản scan.pdf", "scan.pdf")
	attach(r.b, "other.pdf", "mixed.pdf")

	d := testDeps(r.caller, nil)
	d.PDFDocuments = lib
	r.srv = NewServer(d)
	return r
}

// client connects over the real protocol, as the given identity.
func (r *pdfRig) client(t *testing.T, caller identity.Caller) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	ss, err := r.srv.sdkServerFor(caller).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *sdk.ClientSession, tool string, args map[string]any) (*sdk.CallToolResult, error) {
	t.Helper()
	return cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
}

func text(res *sdk.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func structuredMap(t *testing.T, res *sdk.CallToolResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("structured content %s: %v", raw, err)
	}
	return m
}

func TestPDFToolsAreOfferedReadOnlyWithDescribedInputs(t *testing.T) {
	r := newPDFRig(t)
	cs := r.client(t, identity.Agent(r.a))
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"pdf.list": false, "pdf.info": false, "pdf.search": false, "pdf.readText": false, "pdf.viewPages": false}
	for _, tool := range list.Tools {
		if _, ok := want[tool.Name]; !ok {
			continue
		}
		want[tool.Name] = true
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
		schema, _ := json.Marshal(tool.InputSchema)
		if tool.Name != "pdf.list" && !strings.Contains(string(schema), `"documentId"`) {
			t.Errorf("%s has no documentId input: %s", tool.Name, schema)
		}
		if tool.Name == "pdf.readText" && !strings.Contains(string(schema), "range") {
			t.Errorf("pdf.readText does not describe its pages input: %s", schema)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("%s is not offered", name)
		}
	}
	if got := r.srv.PreApprovedTools(); len(got) != 5 {
		t.Errorf("pre-approved = %v", got)
	}
}

func TestPDFToolsAreNotOfferedWithoutADocumentStore(t *testing.T) {
	srv := NewServer(testDeps(&fakeCaller{}, nil))
	if srv.PreApprovedTools() != nil {
		t.Fatal("a tool is pre-approved that does not exist")
	}
	st, ct := sdk.NewInMemoryTransports()
	ss, err := srv.sdkServerFor(identity.Agent("11111111-2222-3333-4444-555555555555")).Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, _ := cs.ListTools(context.Background(), nil)
	for _, tool := range list.Tools {
		if strings.HasPrefix(tool.Name, "pdf.") {
			t.Fatalf("%s is offered with nothing behind it", tool.Name)
		}
	}
}

func TestAnAgentListsInspectsSearchesAndReadsItsDocument(t *testing.T) {
	r := newPDFRig(t)
	cs := r.client(t, identity.Agent(r.a))

	res, err := call(t, cs, "pdf.list", nil)
	if err != nil || res.IsError {
		t.Fatalf("list: %v %v", err, res)
	}
	docs := structuredMap(t, res)["documents"].([]any)
	if len(docs) != 2 || docs[0].(map[string]any)["id"] != "doc1" || docs[0].(map[string]any)["pages"].(float64) != 6 {
		t.Fatalf("documents = %v", docs)
	}

	res, err = call(t, cs, "pdf.info", map[string]any{"documentId": "doc1"})
	if err != nil || res.IsError {
		t.Fatalf("info: %v %v", err, text(res))
	}
	info := structuredMap(t, res)
	pdfInfo := info["pdf"].(map[string]any)
	if pdfInfo["pages"].(float64) != 6 || !strings.Contains(fmt.Sprint(pdfInfo["pageChrome"]), "left sidebar") {
		t.Fatalf("info = %v", info)
	}
	if ocr := pdfInfo["ocr"].(map[string]any); ocr["available"] != false {
		t.Fatalf("ocr = %v", ocr)
	}

	res, err = call(t, cs, "pdf.search", map[string]any{"documentId": "doc1", "query": "XAC THUC khach hang"})
	if err != nil || res.IsError {
		t.Fatalf("search: %v %v", err, text(res))
	}
	result := structuredMap(t, res)["result"].(map[string]any)
	hits := result["hits"].([]any)
	if len(hits) < 6 || result["complete"] != true {
		t.Fatalf("hits = %d, result = %v", len(hits), result)
	}
	first := hits[0].(map[string]any)
	if first["page"].(float64) != 1 || !strings.Contains(first["snippet"].(string), "xác thực khách hàng") {
		t.Fatalf("first hit = %v", first)
	}
	// The sidebar is chrome: not searched by default.
	res, _ = call(t, cs, "pdf.search", map[string]any{"documentId": "doc1", "query": "tư vấn pháp luật"})
	if h := structuredMap(t, res)["result"].(map[string]any)["hits"].([]any); len(h) != 0 {
		t.Fatalf("sidebar text was found: %v", h)
	}
	res, _ = call(t, cs, "pdf.search", map[string]any{"documentId": "doc1", "query": "tư vấn pháp luật", "includeChrome": true})
	if h := structuredMap(t, res)["result"].(map[string]any)["hits"].([]any); len(h) == 0 {
		t.Fatal("includeChrome did not search the sidebar")
	}

	// Pages are accepted as a range, a list, a number or a string.
	for _, pages := range []any{"2-4", "2,3,4", []any{2, 3, 4}} {
		res, err = call(t, cs, "pdf.readText", map[string]any{"documentId": "doc1", "pages": pages})
		if err != nil || res.IsError {
			t.Fatalf("readText %v: %v %v", pages, err, text(res))
		}
		body := text(res)
		for _, want := range []string{"--- page 2 of 6 ---", "--- page 3 of 6 ---", "--- page 4 of 6 ---", "Field-01", "Field-24", "Tham số\tLoại\tBắt buộc\tMô tả", "left out"} {
			if !strings.Contains(body, want) {
				t.Errorf("readText %v lacks %q", pages, want)
			}
		}
		// The note under each page quotes a few omitted lines on purpose, so the
		// agent can judge the call; outside those notes the chrome must not appear.
		var kept []string
		for _, line := range strings.Split(body, "\n") {
			if !strings.Contains(line, "page chrome left out") {
				kept = append(kept, line)
			}
		}
		for _, chrome := range []string{"TƯ VẤN PHÁP LUẬT", "example.test", "9/15/26"} {
			if strings.Contains(strings.Join(kept, "\n"), chrome) {
				t.Errorf("chrome %q leaked into %v", chrome, pages)
			}
		}
		// The text is the whole result: structured content would be what a vendor
		// shows the model instead of it.
		if res.StructuredContent != nil {
			t.Errorf("readText carries structuredContent, which Claude Code gives the model in place of the text: %v", res.StructuredContent)
		}
		if !strings.Contains(body, "Text of pages 2-4") || strings.Count(body, "page chrome left out") != 3 {
			t.Errorf("header or chrome notes missing:\n%s", body[:300])
		}
	}
	res, _ = call(t, cs, "pdf.readText", map[string]any{"documentId": "doc1", "pages": 3, "includeChrome": true})
	if body := text(res); !strings.Contains(body, "TƯ VẤN PHÁP LUẬT") || !strings.Contains(body, "example.test") {
		t.Fatalf("includeChrome: %s", body)
	}
}

func TestPageImagesReachTheClientAsImageContent(t *testing.T) {
	r := newPDFRig(t)
	cs := r.client(t, identity.Agent(r.a))

	res, err := call(t, cs, "pdf.viewPages", map[string]any{"documentId": "doc1", "pages": "2-3"})
	if err != nil || res.IsError {
		t.Fatalf("viewPages: %v %v", err, text(res))
	}
	if len(res.Content) != 3 {
		t.Fatalf("content = %d blocks, want a caption and two images", len(res.Content))
	}
	caption, ok := res.Content[0].(*sdk.TextContent)
	if !ok || !strings.Contains(caption.Text, "image 1 = page 2") || !strings.Contains(caption.Text, "image 2 = page 3") {
		t.Fatalf("the caption does not say which image is which: %v", res.Content[0])
	}
	for i, c := range res.Content[1:] {
		img, ok := c.(*sdk.ImageContent)
		if !ok || img.MIMEType != "image/png" {
			t.Fatalf("block %d = %T", i+1, c)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(img.Data))
		if err != nil || format != "png" || cfg.Width != 935 || cfg.Height != 1210 {
			t.Fatalf("image %d: %v %v %+v", i+1, format, err, cfg)
		}
	}

	// A region is a zoom: more pixels per point, within the ceiling.
	res, err = call(t, cs, "pdf.viewPages", map[string]any{"documentId": "doc1", "pages": 2, "region": map[string]any{"x0": 0.2, "y0": 0.1, "x1": 0.95, "y1": 0.4}})
	if err != nil || res.IsError {
		t.Fatalf("zoom: %v %v", err, text(res))
	}
	img := res.Content[1].(*sdk.ImageContent)
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(img.Data))
	if cfg.Width > 1568 || cfg.Width < 1200 || !strings.Contains(text(res), "zoomed") {
		t.Fatalf("zoom = %+v; %s", cfg, text(res))
	}

	// The wire form is what the vendors read: base64 under "data" with a mimeType.
	raw, _ := json.Marshal(res)
	var wire struct {
		Content []struct {
			Type, Data, MimeType string
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Content[1].Type != "image" || wire.Content[1].MimeType != "image/png" {
		t.Fatalf("wire = %+v", wire.Content[1])
	}
	if _, err := base64.StdEncoding.DecodeString(wire.Content[1].Data); err != nil {
		t.Fatalf("data is not base64: %v", err)
	}

	// A scan can be looked at, though it cannot be read as text.
	res, err = call(t, cs, "pdf.viewPages", map[string]any{"documentId": "doc2", "pages": 1})
	if err != nil || res.IsError || len(res.Content) != 2 {
		t.Fatalf("scan view: %v %v", err, text(res))
	}
	res, _ = call(t, cs, "pdf.readText", map[string]any{"documentId": "doc2", "pages": "1-2"})
	if body := text(res); !strings.Contains(body, "No text layer") || !strings.Contains(body, "pdf.viewPages") {
		t.Fatalf("a scan read as if it had text: %s", body)
	}
	if strings.Count(text(res), "No text layer on this page") != 2 {
		t.Fatalf("both scan pages should say they have no text: %s", text(res))
	}
	res, _ = call(t, cs, "pdf.search", map[string]any{"documentId": "doc2", "query": "biên bản"})
	if body := text(res); !strings.Contains(body, "no text layer") || !strings.Contains(body, "OCR") {
		t.Fatalf("search of a scan: %s", body)
	}
}

func TestSessionsCannotReadEachOthersDocuments(t *testing.T) {
	r := newPDFRig(t)
	ctx := context.Background()
	notFound := "No such document in this session"

	// B has its own doc1 (mixed.pdf, 2 pages). A's doc1 is webprint (6 pages): B must see its own.
	b := r.client(t, identity.Agent(r.b))
	res, err := call(t, b, "pdf.info", map[string]any{"documentId": "doc1"})
	if err != nil || res.IsError || structuredMap(t, res)["pdf"].(map[string]any)["pages"].(float64) != 2 {
		t.Fatalf("B's own doc1: %v %v", err, text(res))
	}
	// B asks for A's doc2 (a scan): it does not exist for B.
	res, err = call(t, b, "pdf.info", map[string]any{"documentId": "doc2"})
	if err == nil && !res.IsError {
		t.Fatal("B read A's document")
	}
	if msg := errText(err, res); !strings.Contains(msg, notFound) {
		t.Fatalf("B got %q", msg)
	}
	// Naming A's session id does not help: same answer as for a missing document.
	for tool, args := range map[string]map[string]any{
		"pdf.list":      {"sessionId": r.a},
		"pdf.info":      {"sessionId": r.a, "documentId": "doc2"},
		"pdf.search":    {"sessionId": r.a, "documentId": "doc2", "query": "x"},
		"pdf.readText":  {"sessionId": r.a, "documentId": "doc2", "pages": 1},
		"pdf.viewPages": {"sessionId": r.a, "documentId": "doc2", "pages": 1},
	} {
		res, err := call(t, b, tool, args)
		if err == nil && !res.IsError {
			t.Errorf("%s: B named A's session and got an answer", tool)
		}
		if msg := errText(err, res); !strings.Contains(msg, notFound) {
			t.Errorf("%s: %q", tool, msg)
		}
	}

	// A terminal caller must say whose documents; with a session it can read them.
	term := r.client(t, identity.UserTerminal())
	res, err = call(t, term, "pdf.list", nil)
	if err == nil && !res.IsError {
		t.Fatal("a terminal caller listed documents without naming a session")
	}
	if msg := errText(err, res); !strings.Contains(msg, "pass sessionId") {
		t.Fatalf("terminal: %q", msg)
	}
	res, err = call(t, term, "pdf.list", map[string]any{"sessionId": r.a})
	if err != nil || res.IsError {
		t.Fatalf("terminal with a session: %v %v", err, text(res))
	}

	// An agent identity without a session (never produced by SessionHost) is refused.
	orphan := r.client(t, identity.Caller{Kind: identity.KindAgent})
	if res, err := call(t, orphan, "pdf.list", nil); err == nil && !res.IsError {
		t.Fatal("an agent with no session read documents")
	}

	// The gateway is asked, scoped to the caller's own session, and its audit line
	// carries a handle — never the file's name or the query.
	if _, err := call(t, r.client(t, identity.Agent(r.a)), "pdf.search", map[string]any{"documentId": "doc1", "query": "mật khẩu bí mật"}); err != nil {
		t.Fatal(err)
	}
	last := r.caller.last
	if last.subject != "builtin.agenthub" || last.capability != "agenthub:read" || last.scope != "session:"+r.a || last.caller != "agent:"+r.a {
		t.Fatalf("gateway call = %+v", last)
	}
	audit := fmt.Sprint(last.metadata)
	for _, secret := range []string{"mật khẩu", "Thông tư mẫu", ".pdf"} {
		if strings.Contains(audit, secret) {
			t.Errorf("audit metadata carries %q: %s", secret, audit)
		}
	}
	_ = ctx
}

func errText(err error, res *sdk.CallToolResult) string {
	if err != nil {
		return err.Error()
	}
	if res != nil {
		return text(res)
	}
	return ""
}

func TestPDFToolsRefuseWhatTheyCannotDoOutLoud(t *testing.T) {
	r := newPDFRig(t)
	cs := r.client(t, identity.Agent(r.a))
	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"page past the end", "pdf.readText", map[string]any{"documentId": "doc1", "pages": 9}, "page 9 of a 6-page"},
		{"page zero", "pdf.readText", map[string]any{"documentId": "doc1", "pages": 0}, "not a page number"},
		{"no pages", "pdf.readText", map[string]any{"documentId": "doc1"}, "pages"},
		{"bad range", "pdf.readText", map[string]any{"documentId": "doc1", "pages": "5-2"}, "not a valid range"},
		{"too many pages", "pdf.readText", map[string]any{"documentId": "doc1", "pages": "1-30"}, "limit"},
		{"too many images", "pdf.viewPages", map[string]any{"documentId": "doc1", "pages": "1-5"}, "limit"},
		{"bad dpi", "pdf.viewPages", map[string]any{"documentId": "doc1", "pages": 1, "dpi": 5000}, "dpi must be"},
		{"bad region", "pdf.viewPages", map[string]any{"documentId": "doc1", "pages": 1, "region": map[string]any{"x0": 0.6, "y0": 0, "x1": 0.5, "y1": 1}}, "region must be"},
		{"empty query", "pdf.search", map[string]any{"documentId": "doc1", "query": "  "}, "query is required"},
		{"unknown document", "pdf.info", map[string]any{"documentId": "doc77"}, "No such document"},
	}
	for _, c := range cases {
		res, err := call(t, cs, c.tool, c.args)
		if err == nil && (res == nil || !res.IsError) {
			t.Errorf("%s: answered as if it had read the document: %s", c.name, text(res))
			continue
		}
		if msg := errText(err, res); !strings.Contains(msg, c.want) {
			t.Errorf("%s: %q does not say %q", c.name, msg, c.want)
		}
	}

	// A stored copy that no longer matches is not read.
	stored := filepath.Join(r.dir, r.a, "doc1.pdf")
	if err := os.WriteFile(stored, []byte("%PDF-1.4 changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := call(t, cs, "pdf.info", map[string]any{"documentId": "doc1"})
	if err == nil && !res.IsError {
		t.Fatal("a changed stored copy was read")
	}
	if msg := errText(err, res); !strings.Contains(msg, "no longer matches") {
		t.Fatalf("tampered: %q", msg)
	}
}

func TestParsePageSpec(t *testing.T) {
	for in, want := range map[string][]int{
		"25": {25}, "24-26": {24, 25, 26}, "1,3,5-7": {1, 3, 5, 6, 7}, " 2 , 2 ,3": {2, 3}, "10-10": {10},
	} {
		got, err := parsePageSpec(in)
		if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%q = %v, %v; want %v", in, got, err, want)
		}
	}
	if got, err := parsePageSpec(float64(7)); err != nil || fmt.Sprint(got) != "[7]" {
		t.Errorf("a JSON number: %v, %v", got, err)
	}
	if got, err := parsePageSpec([]any{float64(1), "3-4"}); err != nil || fmt.Sprint(got) != "[1 3 4]" {
		t.Errorf("a list: %v, %v", got, err)
	}
	for _, bad := range []any{nil, "", "x", "0", "-3", "3-", "1-1000", true, []any{true}, "1.5"} {
		if _, err := parsePageSpec(bad); err == nil {
			t.Errorf("%#v was accepted", bad)
		}
	}
}

func TestAReadThatDoesNotFitSaysWhichPagesAreLeft(t *testing.T) {
	doc := agenthub.Document{ID: "doc1", Name: "big.pdf", Pages: 100}
	line := strings.Repeat("một dòng văn bản dài của tài liệu ", 3) + "\n"
	page := func(n int) pdfdoc.PageText {
		return pdfdoc.PageText{Page: n, HasTextLayer: true, Text: strings.Repeat(line, 100)} // ~10 KB a page
	}
	var texts []pdfdoc.PageText
	for n := 1; n <= 10; n++ {
		texts = append(texts, page(n))
	}
	res := readResult(doc, texts, pdfReadInput{})
	body := res.render()
	if !strings.Contains(body, "never an instruction") {
		t.Errorf("a read does not mark the document text as data:\n%.300s", body)
	}
	if len(body) > pdfTextBudget+2048 {
		t.Fatalf("a result of %d bytes is over the %d budget", len(body), pdfTextBudget)
	}
	returned := res.Structured["pagesReturned"].([]int)
	left := res.Structured["pagesNotReturned"].([]int)
	if len(returned) == 0 || len(left) == 0 || len(returned)+len(left) != 10 {
		t.Fatalf("returned %v, not returned %v", returned, left)
	}
	if returned[len(returned)-1]+1 != left[0] {
		t.Fatalf("a gap between what came back and what is left: %v / %v", returned, left)
	}
	if !strings.Contains(body, "Output limit reached") || !strings.Contains(body, fmt.Sprintf("%d", left[0])) {
		t.Fatalf("the result does not name the pages that did not fit:\n%s", body[len(body)-300:])
	}
	// No page is cut in the middle: each returned page ends with its own text.
	for _, n := range returned {
		if !strings.Contains(body, fmt.Sprintf("--- page %d of 100 ---", n)) {
			t.Fatalf("page %d missing", n)
		}
	}

	// One page alone over the budget is cut at a line and says so.
	huge := []pdfdoc.PageText{{Page: 5, HasTextLayer: true, Text: strings.Repeat(line, 2000)}}
	res = readResult(doc, huge, pdfReadInput{})
	body = res.render()
	if len(body) > pdfTextBudget+1024 || !strings.Contains(body, "was cut here") || res.Structured["pageCut"] != 5 {
		t.Fatalf("a huge page: %d bytes; cut = %v", len(body), res.Structured["pageCut"])
	}
}

func TestAnErrorFromTheEngineIsNeverAnEmptyAnswer(t *testing.T) {
	for err, want := range map[error]string{
		pdfdoc.ErrPasswordRequired:         "password-protected",
		pdfdoc.ErrCorrupt:                  "damaged",
		pdfdoc.ErrUnavailable:              "could not start",
		agenthub.ErrDocumentNotFound:       "No such document",
		agenthub.ErrDocumentsUnavailable:   "not available",
		context.DeadlineExceeded:           "too long",
		errors.New("something unexpected"): "Could not read the document",
	} {
		got := pdfProblem(err)
		if got == nil || !strings.Contains(got.Error(), want) {
			t.Errorf("%v → %v, want %q", err, got, want)
		}
	}
}
