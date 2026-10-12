package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/pdfdoc"
	"devkit/internal/sandbox"
)

// codexAdapter runs the real `codex app-server` in DevKit's sandbox, pointed at
// the mock Responses API through OPENAI_BASE_URL (Codex's built-in openai
// provider), signed in with a dummy API key over the wire like DevKit does.
func codexAdapter(t *testing.T, model *modelServer) *agenthub.CodexAdapter {
	dir := requireDir(t, "DEVKIT_PDF_E2E_CODEX")
	bin := filepath.Join(dir, "node_modules", "@openai", "codex-linux-x64", "vendor", "x86_64-unknown-linux-musl", "bin", "codex")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no codex binary at %s", bin)
	}
	return agenthub.NewCodexAdapter(agenthub.CodexOptions{
		Command: bin,
		Home:    shortDir(t, "codexhome"),
		APIKey:  func(context.Context) (string, error) { return "sk-e2e", nil },
		// 0.147 does not read OPENAI_BASE_URL; its config key does the same job.
		Runner:  envRunner{inner: sandbox.DefaultRunner(), args: []string{"-c", `openai_base_url="` + model.URL() + `/v1"`}},
		Version: "e2e",
	})
}

// execResult is the text a Codex exec script printed, without Codex's own
// "Script completed / Wall time" header.
func execOutput(r seenResult) string {
	_, out, _ := strings.Cut(r.text(), "Output:\n")
	return strings.TrimSpace(out)
}

func js(tool string, args string, after string) toolCall {
	return toolCall{Code: "const r = await tools.mcp__devkit__" + tool + "(" + args + "); " + after}
}

const forwardImages = `for (const c of r.content) { if (c.type === "image") image(c); }`

func TestCodexReadsAnAttachedPDFAndSeesItsPages(t *testing.T) {
	model := newModelServer(t, openaiHandler)
	st := newStack(t, codexAdapter(t, model))
	sess := st.newSession(agenthub.AgentTypeCodex)
	pdf := st.copyPDF("Thông tư mẫu.pdf", "webprint.pdf")

	model.setScript("xong",
		toolCall{Code: `text(JSON.stringify(ALL_TOOLS.filter(t => t.name.includes("pdf_")).map(t => t.name))); text(ALL_TOOLS.find(t => t.name.endsWith("pdf_viewPages")).description)`},
		js("pdf_list", `{}`, `text(JSON.stringify(r.structuredContent))`),
		js("pdf_info", `{documentId: "doc1"}`, `text(JSON.stringify(r.structuredContent))`),
		js("pdf_search", `{documentId: "doc1", query: "XAC THUC khach hang", maxHits: 3}`, `text(JSON.stringify(r.structuredContent))`),
		js("pdf_readText", `{documentId: "doc1", pages: "2-4"}`, `text(r.content[0].text)`),
		js("pdf_viewPages", `{documentId: "doc1", pages: 2}`, `text(r.content[0].text); `+forwardImages),
		js("pdf_viewPages", `{documentId: "doc1", pages: 3, region: {x0: 0.2, y0: 0.1, x1: 0.95, y1: 0.4}}`, `text(r.content[0].text); `+forwardImages),
		// A control: the same call with the image not forwarded.
		js("pdf_viewPages", `{documentId: "doc1", pages: 4}`, `text(r.content[0].text);`),
	)
	res := st.invoke(sess, "Điều nào quy định về xác thực khách hàng? Kiểm tra bảng ở trang 2-4.", pdf)
	t.Logf("turn status %q", res.Status)

	// What Codex told its model, and what the model saw of the tools.
	instructions := model.system
	prompt := strings.Join(model.prompts, "\n---\n")
	if !contains(prompt, "mcp__devkit__git_commit", "await tools.<name>(args)", "image(result.content[i])") {
		t.Errorf("Codex's developer instructions do not name its tools or say how to forward images")
	}
	if !contains(prompt, "[DevKit attached 1 PDF document", "doc1", "Thông tư mẫu.pdf", "6 pages", "mcp__devkit__pdf_readText", "image(c)", "never instructions", "Điều nào quy định") {
		t.Errorf("the model's prompt:\n%s", prompt)
	}
	if strings.Contains(prompt, "Field-01") || strings.Contains(prompt, "Đoạn 1 của trang") {
		t.Errorf("the document's own text was put in the prompt")
	}
	_ = instructions

	results := model.snapshotResults()
	if len(results) != 8 {
		for _, r := range results {
			t.Logf("result %s error=%v: %.200s", r.Tool, r.IsError, r.text())
		}
		t.Fatalf("the model received %d tool results, want 8", len(results))
	}
	for i, r := range results {
		// Pre-approved: no approval was put to the person (there is nobody to
		// answer, so a prompt would have been declined and the script would fail).
		if r.IsError || strings.Contains(r.text(), "Error") || strings.Contains(r.text(), "declined") {
			t.Errorf("step %d failed: %s", i, r.text())
		}
	}
	out := func(i int) string { return execOutput(results[i]) }

	if !contains(out(0), "mcp__devkit__pdf_viewPages", "mcp__devkit__pdf_readText", "mcp__devkit__pdf_search", "mcp__devkit__pdf_info", "mcp__devkit__pdf_list") {
		t.Errorf("the pdf tools are not visible to Codex's code mode: %s", out(0))
	}
	if !strings.Contains(out(0), "image(result.content[i])") {
		t.Errorf("the tool description Codex shows its model does not say how to forward images: %s", out(0))
	}
	if !contains(out(1), "doc1", "Thông tư mẫu.pdf") || !contains(out(2), "left sidebar", "textLayer") || !contains(out(3), "xác thực khách hàng", `"page"`) {
		t.Errorf("list/info/search: %s / %s / %s", out(1), out(2), out(3))
	}
	read := out(4)
	if !contains(read, "--- page 2 of 6 ---", "--- page 4 of 6 ---", "Field-01", "Field-24", "Tham số\tLoại\tBắt buộc\tMô tả") {
		t.Errorf("pdf_readText: %.400s", read)
	}
	if strings.Contains(strings.Join(filterOut(strings.Split(read, "\n"), "page chrome left out"), "\n"), "TƯ VẤN PHÁP LUẬT") {
		t.Errorf("the sidebar reached the model as body text")
	}

	// Images: only what the script forwarded reached the model.
	full := results[5].images()
	if len(full) != 1 {
		t.Fatalf("a forwarded page image: the model got %d; result: %.500s", len(full), results[5].Raw)
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
		t.Logf("note: the image the model received differs from DevKit's render (%d vs %d bytes)", len(full[0].Data), len(want[0].Data))
	}
	zoom := results[6].images()
	if len(zoom) != 1 {
		t.Fatalf("zoomed region: %d images", len(zoom))
	}
	if zimg, _ := decodeImage(t, zoom[0]); zimg.Bounds().Dx() < 1200 || zimg.Bounds().Dx() > 1568 || inkFraction(zimg) < 0.002 {
		t.Errorf("zoomed region image: %v ink=%.4f", zimg.Bounds(), inkFraction(zimg))
	}
	// DevKit's Codex translator already reduces an MCP result's pictures to a byte
	// count, so nothing large is stored (and none reaches the UI).
	if stored := st.storedBytesWith(sess.ID, "pdf.viewPages"); stored == 0 || stored > 16_000 {
		t.Errorf("the stored row for a page view is %d bytes: page pictures are being kept in the transcript", stored)
	}
	if n := len(results[7].images()); n != 0 {
		t.Errorf("an image the script did not forward reached the model (%d): the guidance to forward is what makes pages visible, and this shows why it is needed", n)
	}
}

func TestCodexScannedPDFResumeAndIsolation(t *testing.T) {
	model := newModelServer(t, openaiHandler)
	st := newStack(t, codexAdapter(t, model))
	a := st.newSession(agenthub.AgentTypeCodex)
	ctx := context.Background()

	model.setScript("ok", js("pdf_list", `{}`, `text(JSON.stringify(r.structuredContent))`))
	st.invoke(a, "đọc hai tài liệu", st.copyPDF("văn bản.pdf", "webprint.pdf"), st.copyPDF("biên bản scan.pdf", "scan.pdf"))
	if p := strings.Join(model.prompts, "\n"); !contains(p, "doc1", "doc2", "text layer: none (scanned or image-only)", "text layer: yes") {
		t.Errorf("the prompt does not describe both documents:\n%s", p)
	}

	// The scan has no text; the page is looked at instead.
	before := len(model.snapshotResults())
	model.setScript("đã xem",
		js("pdf_readText", `{documentId: "doc2", pages: "1-2"}`, `text(r.content[0].text)`),
		js("pdf_search", `{documentId: "doc2", query: "biên bản"}`, `text(JSON.stringify(r.structuredContent))`),
		js("pdf_viewPages", `{documentId: "doc2", pages: 1}`, `text(r.content[0].text); `+forwardImages),
	)
	st.invoke(a, "scan nói gì?")
	got := model.snapshotResults()[before:]
	if len(got) != 3 {
		t.Fatalf("got %d results", len(got))
	}
	if o := execOutput(got[0]); !contains(o, "No text layer on this page", "pdf.viewPages") || strings.Count(o, "No text layer on this page") != 2 {
		t.Errorf("a scan was read as if it had text: %q", o)
	}
	if o := execOutput(got[1]); !contains(o, "OCR", "pagesWithoutText") {
		t.Errorf("a search of a scan did not say what it could not search: %q", o)
	}
	if imgs := got[2].images(); len(imgs) != 1 {
		t.Errorf("the scan was not shown to the model: %.300s", got[2].Raw)
	} else if img, _ := decodeImage(t, imgs[0]); inkFraction(img) < 0.001 {
		t.Errorf("the scan image is blank (ink %.5f)", inkFraction(img))
	}

	// Closed, then prompted again: a new process, the thread reopened.
	if err := st.svc.CloseSession(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	before = len(model.snapshotResults())
	model.setScript("sau khi tiếp tục",
		js("pdf_list", `{}`, `text(JSON.stringify(r.structuredContent))`),
		js("pdf_readText", `{documentId: "doc1", pages: "3"}`, `text(r.content[0].text)`),
		js("pdf_viewPages", `{documentId: "doc1", pages: 3}`, `text(r.content[0].text); `+forwardImages),
	)
	st.invoke(a, "tiếp tục: trang 3 có gì?")
	got = model.snapshotResults()[before:]
	if len(got) != 3 {
		t.Fatalf("after the resume: %d results", len(got))
	}
	if !contains(execOutput(got[0]), "doc1", "doc2") || !contains(execOutput(got[1]), "--- page 3 of 6 ---", "Field-09") || len(got[2].images()) != 1 {
		t.Errorf("after the resume: list=%q read=%.200q images=%d", execOutput(got[0]), execOutput(got[1]), len(got[2].images()))
	}
	if p := strings.Join(model.prompts, "\n"); !contains(p, "PDF documents attached to this session", "doc1", "doc2") {
		t.Errorf("a resumed conversation is not told what documents it holds:\n%s", p)
	}

	// Another session cannot read A's documents.
	b := st.newSession(agenthub.AgentTypeCodex)
	before = len(model.snapshotResults())
	model.setScript("b",
		js("pdf_list", `{}`, `text(JSON.stringify(r.structuredContent))`),
		js("pdf_info", `{documentId: "doc1"}`, `text(r.isError ? "ERR " + r.content[0].text : "READ")`),
		js("pdf_info", `{documentId: "doc2", sessionId: "`+a.ID+`"}`, `text(r.isError ? "ERR " + r.content[0].text : "READ")`),
		js("pdf_readText", `{documentId: "doc1", pages: 1, sessionId: "`+a.ID+`"}`, `text(r.isError ? "ERR " + r.content[0].text : "READ")`),
	)
	st.invoke(b, "tôi có tài liệu nào?")
	got = model.snapshotResults()[before:]
	if len(got) != 4 {
		t.Fatalf("session B got %d results", len(got))
	}
	if o := execOutput(got[0]); strings.Contains(o, "doc1") || !strings.Contains(o, `"documents":[]`) {
		t.Errorf("B lists %q", o)
	}
	for i, r := range got[1:] {
		if o := execOutput(r); !strings.HasPrefix(o, "ERR ") || !strings.Contains(o, "No such document in this session") || strings.Contains(o, "Field-") || strings.Contains(o, "READ") {
			t.Errorf("B's request %d: %q", i+1, o)
		}
	}
}

func TestCodexRefusesAPDFItCannotReadBeforeTheModelIsAsked(t *testing.T) {
	model := newModelServer(t, openaiHandler)
	st := newStack(t, codexAdapter(t, model))
	sess := st.newSession(agenthub.AgentTypeCodex)
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
	model.mu.Lock()
	n := model.mainReqs
	model.mu.Unlock()
	if n != 0 {
		t.Errorf("the model was asked %d time(s) about PDFs that could not be read", n)
	}
}
