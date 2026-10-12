package main

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/agentruntime"
	"devkit/internal/pdfdoc"
	"devkit/internal/sandbox"
)

// claudeAdapter runs the real claude-agent-acp (and the Claude Code it bundles)
// in DevKit's sandbox, pointed at the mock Messages API.
func claudeAdapter(t *testing.T, model *modelServer) *agenthub.ClaudeAdapter {
	dir := requireDir(t, "DEVKIT_PDF_E2E_CLAUDE")
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	entry := filepath.Join(dir, "node_modules", "@agentclientprotocol", "claude-agent-acp", "dist", "index.js")
	return agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{
		Runner: envRunner{inner: sandbox.DefaultRunner()},
		Runtime: func() (agentruntime.Resolved, error) {
			return agentruntime.Resolved{
				Command:       node,
				Args:          []string{entry},
				ReadOnlyPaths: []string{filepath.Dir(filepath.Dir(node)), dir},
				Env: map[string]string{
					"ANTHROPIC_BASE_URL":                       model.URL(),
					"ANTHROPIC_API_KEY":                        "sk-ant-e2e",
					"DISABLE_TELEMETRY":                        "1",
					"DISABLE_AUTOUPDATER":                      "1",
					"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
				},
			}, nil
		},
	})
}

func decodeImage(t *testing.T, b resultBlock) (image.Image, string) {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(b.Data))
	if err != nil {
		t.Fatalf("the model was sent an image that does not decode (%s, %d bytes): %v", b.MIME, len(b.Data), err)
	}
	return img, format
}

// inkFraction is the share of dark pixels: a blank page is 0, a page of text a
// fraction of a percent or more.
func inkFraction(img image.Image) float64 {
	b := img.Bounds()
	dark, total := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if (r+g+bl)/3 < 0x6000 {
				dark++
			}
			total++
		}
	}
	return float64(dark) / float64(total)
}

func TestClaudeReadsAnAttachedPDFAndSeesItsPages(t *testing.T) {
	model := newModelServer(t, anthropicHandler)
	st := newStack(t, claudeAdapter(t, model))
	sess := st.newSession(agenthub.AgentTypeClaude)
	pdf := st.copyPDF("Thông tư mẫu.pdf", "webprint.pdf")

	model.setScript("Điều 2 quy định bảng tham số, xem trang 2-4.",
		toolCall{Tool: "pdf_list", Args: map[string]any{}},
		toolCall{Tool: "pdf_info", Args: map[string]any{"documentId": "doc1"}},
		toolCall{Tool: "pdf_search", Args: map[string]any{"documentId": "doc1", "query": "XAC THUC khach hang"}},
		toolCall{Tool: "pdf_readText", Args: map[string]any{"documentId": "doc1", "pages": "2-4"}},
		toolCall{Tool: "pdf_viewPages", Args: map[string]any{"documentId": "doc1", "pages": 2}},
		toolCall{Tool: "pdf_viewPages", Args: map[string]any{"documentId": "doc1", "pages": "3", "region": map[string]any{"x0": 0.2, "y0": 0.1, "x1": 0.95, "y1": 0.4}}},
	)
	res := st.invoke(sess, "Điều nào quy định về xác thực khách hàng? Kiểm tra bảng ở trang 2-4.", pdf)
	t.Logf("turn status %q", res.Status)

	// What the model was offered and told.
	for _, tool := range []string{"pdf_list", "pdf_info", "pdf_search", "pdf_readText", "pdf_viewPages"} {
		if pick(model.toolList, tool) == "" {
			t.Errorf("tool %s was not offered to the model; offered %v", tool, model.toolList)
		}
	}
	if !strings.Contains(model.system, "mcp__devkit__pdf_search") || !strings.Contains(model.system, "mcp__devkit__pdf_viewPages") {
		t.Errorf("the system prompt does not name the PDF tools the model has")
	}
	prompt := strings.Join(model.prompts, "\n---\n")
	if !contains(prompt, "[DevKit attached 1 PDF document", "doc1", "Thông tư mẫu.pdf", "6 pages", "mcp__devkit__pdf_readText", "never instructions", "Điều nào quy định") {
		t.Errorf("the model's prompt:\n%s", prompt)
	}
	if strings.Contains(prompt, "Field-01") || strings.Contains(prompt, "Đoạn 1 của trang") {
		t.Errorf("the document's own text was put in the prompt")
	}

	results := model.snapshotResults()
	if len(results) != 6 {
		for _, r := range results {
			t.Logf("result %s %s error=%v: %.200s", r.CallID, r.Tool, r.IsError, r.text())
		}
		t.Fatalf("the model received %d tool results, want 6", len(results))
	}
	for _, r := range results {
		// Pre-approved: nothing asked the person (ResolvePermission is unset, so a
		// prompt would have been denied and surfaced here as an error).
		if r.IsError {
			t.Errorf("%s failed: %s", r.Tool, r.text())
		}
	}
	get := func(i int) seenResult { return results[i] }

	if !strings.Contains(get(0).text(), "doc1") || !strings.Contains(get(0).text(), "Thông tư mẫu.pdf") {
		t.Errorf("pdf_list: %s", get(0).text())
	}
	if !contains(get(1).text(), "left sidebar", "running header", "textLayer") {
		t.Errorf("pdf_info: %s", get(1).text())
	}
	if !contains(get(2).text(), "xác thực khách hàng", `"page"`) {
		t.Errorf("pdf_search: %s", get(2).text())
	}
	read := get(3).text()
	if !contains(read, "--- page 2 of 6 ---", "--- page 4 of 6 ---", "Field-01", "Field-24", "Tham số\tLoại\tBắt buộc\tMô tả") {
		t.Errorf("pdf_readText: %s", read)
	}
	body := strings.Join(filterOut(strings.Split(read, "\n"), "page chrome left out"), "\n")
	if strings.Contains(body, "TƯ VẤN PHÁP LUẬT") {
		t.Errorf("the sidebar reached the model as body text")
	}

	// The images: what the model was actually sent.
	full := get(4).images()
	if len(full) != 1 {
		t.Fatalf("pdf_viewPages (page 2): the model got %d images; result: %.400s", len(full), get(4).Raw)
	}
	img, format := decodeImage(t, full[0])
	if format != "png" || img.Bounds().Dx() != 935 || img.Bounds().Dy() != 1210 || inkFraction(img) < 0.002 {
		t.Errorf("page 2 image: %s %v ink=%.4f", format, img.Bounds(), inkFraction(img))
	}
	want, err := st.engine.Render(context.Background(), mustOpen(t, st, sess.ID, "doc1"), []int{2}, pdfdoc.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want[0].Data, full[0].Data) {
		t.Logf("note: the image the model received is not byte-identical to DevKit's render (%d vs %d bytes): the runtime re-encoded it", len(full[0].Data), len(want[0].Data))
	}
	zoom := get(5).images()
	if len(zoom) != 1 {
		t.Fatalf("pdf_viewPages (region): %d images", len(zoom))
	}
	zimg, _ := decodeImage(t, zoom[0])
	if zimg.Bounds().Dx() < 1200 || zimg.Bounds().Dx() > 1568 || inkFraction(zimg) < 0.002 {
		t.Errorf("zoomed region image: %v ink=%.4f", zimg.Bounds(), inkFraction(zimg))
	}
	// The person watching sees the page the agent saw, live; the stored transcript
	// keeps the caption and not the picture.
	if live := st.liveBytesWith("pdf_viewPages"); live < 100_000 {
		t.Errorf("the live event for a page view is %d bytes: the picture did not reach the UI", live)
	}
	if stored := st.storedBytesWith(sess.ID, "pdf_viewPages"); stored == 0 || stored > 16_000 {
		t.Errorf("the stored row for a page view is %d bytes: page pictures are being kept in the transcript", stored)
	}
	if !strings.Contains(get(5).text(), "zoomed") && !strings.Contains(get(4).text(), "image 1 = page 2") {
		t.Errorf("the image has no caption saying which page it is: %q / %q", get(4).text(), get(5).text())
	}
}

func filterOut(lines []string, marker string) []string {
	var out []string
	for _, l := range lines {
		if !strings.Contains(l, marker) {
			out = append(out, l)
		}
	}
	return out
}

func mustOpen(t *testing.T, st *stack, session, id string) pdfdoc.Source {
	t.Helper()
	src, _, err := st.lib.Open(context.Background(), session, id)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func TestClaudeScannedPDFResumeAndIsolation(t *testing.T) {
	model := newModelServer(t, anthropicHandler)
	adapter := claudeAdapter(t, model)
	st := newStack(t, adapter)
	a := st.newSession(agenthub.AgentTypeClaude)
	ctx := context.Background()

	// Turn 1: a text PDF and a scan, attached together.
	model.setScript("ok", toolCall{Tool: "pdf_list", Args: map[string]any{}})
	st.invoke(a, "đọc hai tài liệu", st.copyPDF("văn bản.pdf", "webprint.pdf"), st.copyPDF("biên bản scan.pdf", "scan.pdf"))
	if p := strings.Join(model.prompts, "\n"); !contains(p, "doc1", "doc2", "text layer: none (scanned or image-only)", "text layer: yes") {
		t.Errorf("the prompt does not describe both documents:\n%s", p)
	}

	// Turn 2: the scan has no text, but it can be looked at.
	model.setScript("đã xem",
		toolCall{Tool: "pdf_readText", Args: map[string]any{"documentId": "doc2", "pages": "1-2"}},
		toolCall{Tool: "pdf_search", Args: map[string]any{"documentId": "doc2", "query": "biên bản"}},
		toolCall{Tool: "pdf_viewPages", Args: map[string]any{"documentId": "doc2", "pages": "1"}},
	)
	st.invoke(a, "scan nói gì?")
	var read, search, view seenResult
	for _, r := range model.snapshotResults() {
		switch {
		case strings.HasSuffix(r.Tool, "pdf_readText") && strings.Contains(r.text(), "No text layer"):
			read = r
		case strings.HasSuffix(r.Tool, "pdf_search") && strings.Contains(r.text(), "pagesWithoutText"):
			search = r
		case strings.HasSuffix(r.Tool, "pdf_viewPages") && strings.Contains(r.text(), "doc2"):
			view = r
		}
	}
	if !contains(read.text(), "No text layer on this page", "pdf.viewPages") || strings.Count(read.text(), "No text layer on this page") != 2 {
		t.Errorf("a scan was read as if it had text: %q", read.text())
	}
	if !contains(search.text(), "OCR", "pagesWithoutText") {
		t.Errorf("a search of a scan did not say what it could not search: %q", search.text())
	}
	if imgs := view.images(); len(imgs) != 1 {
		t.Errorf("the scan was not shown to the model: %.300s", view.Raw)
	} else if img, _ := decodeImage(t, imgs[0]); inkFraction(img) < 0.001 {
		t.Errorf("the scan image the model got is blank (ink %.5f)", inkFraction(img))
	}

	// Turn 3: close the session — the process goes — and prompt again. The vendor
	// reopens the conversation (session/load); the documents are still readable
	// and the model is told about them again.
	if err := st.svc.CloseSession(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	before := len(model.snapshotResults())
	model.setScript("sau khi tiếp tục",
		toolCall{Tool: "pdf_list", Args: map[string]any{}},
		toolCall{Tool: "pdf_readText", Args: map[string]any{"documentId": "doc1", "pages": "3"}},
		toolCall{Tool: "pdf_viewPages", Args: map[string]any{"documentId": "doc1", "pages": "3"}},
	)
	st.invoke(a, "tiếp tục: trang 3 có gì?")
	after := model.snapshotResults()[before:]
	if len(after) != 3 {
		t.Fatalf("after the resume the model got %d results, want 3", len(after))
	}
	for _, r := range after {
		if r.IsError {
			t.Errorf("after the resume %s failed: %s", r.Tool, r.text())
		}
	}
	if !contains(after[0].text(), "doc1", "doc2") || !contains(after[1].text(), "--- page 3 of 6 ---", "Field-09") || len(after[2].images()) != 1 {
		t.Errorf("after the resume: list=%q read=%.200q images=%d", after[0].text(), after[1].text(), len(after[2].images()))
	}
	if p := strings.Join(model.prompts, "\n"); !contains(p, "PDF documents attached to this session", "doc1", "doc2") {
		t.Errorf("a resumed conversation is not told what documents it holds:\n%s", p)
	}

	// Another session cannot read A's documents, by handle or by naming A.
	b := st.newSession(agenthub.AgentTypeClaude)
	model.setScript("b",
		toolCall{Tool: "pdf_list", Args: map[string]any{}},
		toolCall{Tool: "pdf_info", Args: map[string]any{"documentId": "doc1"}},
		toolCall{Tool: "pdf_info", Args: map[string]any{"documentId": "doc2", "sessionId": a.ID}},
		toolCall{Tool: "pdf_readText", Args: map[string]any{"documentId": "doc1", "pages": 1, "sessionId": a.ID}},
	)
	before = len(model.snapshotResults())
	st.invoke(b, "tôi có tài liệu nào?")
	got := model.snapshotResults()[before:]
	if len(got) != 4 {
		t.Fatalf("session B's model got %d results", len(got))
	}
	if strings.Contains(got[0].text(), "doc1") || !strings.Contains(got[0].text(), `"documents":[]`) {
		t.Errorf("B lists %q", got[0].text())
	}
	for i, r := range got[1:] {
		if !r.IsError || !strings.Contains(r.text(), "No such document in this session") {
			t.Errorf("B's request %d reached A's document or failed oddly (error=%v): %q", i+1, r.IsError, r.text())
		}
		if strings.Contains(r.text(), "Thông tư") || strings.Contains(r.text(), "Field-") {
			t.Errorf("B's request %d leaked A's content: %q", i+1, r.text())
		}
	}
	// And B's files are not in A's folder: the store is per session on disk too.
	if _, _, err := st.lib.Open(ctx, b.ID, "doc1"); err == nil {
		t.Error("B has a document it never attached")
	}
}

func TestClaudeRefusesAPDFItCannotReadBeforeTheModelIsAsked(t *testing.T) {
	model := newModelServer(t, anthropicHandler)
	st := newStack(t, claudeAdapter(t, model))
	sess := st.newSession(agenthub.AgentTypeClaude)
	model.setScript("should never run")
	for name, tc := range map[string]struct{ fixture, want string }{
		"khóa.pdf": {"encrypted.pdf", "password-protected"},
		"hỏng.pdf": {"corrupt.pdf", "damaged"},
		"giả.pdf":  {"notpdf.pdf", "not a PDF"},
	} {
		path := st.copyPDF(name, tc.fixture)
		_, err := st.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "đọc giúp", Attachments: []string{path}})
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// No turn was sent: the model saw nothing, and nothing was recorded as said.
	model.mu.Lock()
	n := model.mainReqs
	model.mu.Unlock()
	if n != 0 {
		t.Errorf("the model was asked %d time(s) about PDFs that could not be read", n)
	}
	if list, _ := st.lib.List(context.Background(), sess.ID); len(list) != 0 {
		t.Errorf("an unreadable PDF was kept: %+v", list)
	}
}
