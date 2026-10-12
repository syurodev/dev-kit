package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/agenthub"
)

// The real document these were built for: a 45-page website print
// (thuvienphapluat.vn) with a left sidebar and running header and footer on
// every sheet, and a table that runs from page 24 through 26 — page 25 opens
// with rows continuing from 24 and closes on a cell whose sentence ends on 26.
//
//	DEVKIT_PDF_E2E_SAMPLE=/path/to/Thông-tư-64.pdf
func requireSample(t *testing.T) string {
	path := os.Getenv("DEVKIT_PDF_E2E_SAMPLE")
	if path == "" {
		t.Skip("set DEVKIT_PDF_E2E_SAMPLE to the real PDF to run this")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("sample: %v", err)
	}
	return path
}

func (s *stack) copyFile(name, from string) string {
	s.t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		s.t.Fatal(err)
	}
	path := filepath.Join(s.work, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		s.t.Fatal(err)
	}
	return path
}

// assertSample checks what the model was sent, whichever runtime sent it. out
// gives the text a result carried; images the pictures.
func assertSample(t *testing.T, results []seenResult, out func(seenResult) string) {
	t.Helper()
	if len(results) != 5 {
		for _, r := range results {
			t.Logf("result %s error=%v: %.200s", r.Tool, r.IsError, r.text())
		}
		t.Fatalf("the model received %d results, want 5", len(results))
	}
	info, search, read := out(results[0]), out(results[1]), out(results[2])
	if !contains(info, `"pages":45`, `"state":"full"`, "left sidebar", "running header", "running footer") {
		t.Errorf("pdf_info: %.600s", info)
	}
	if !contains(search, "Authorization", `"page":25`) {
		t.Errorf("pdf_search: %.600s", search)
	}

	// Three pages of a table that crosses page boundaries, in one read.
	for _, want := range []string{"--- page 24 of 45 ---", "--- page 25 of 45 ---", "--- page 26 of 45 ---", "4.5. API Xác nhận thanh toán", "Endpoint\t/v1/payments/submit", "consentId\tString [36]"} {
		if !strings.Contains(read, want) {
			t.Errorf("pdf_readText 24-26 lacks %q", want)
		}
	}
	// The table continues over the boundary where the document says it does: the
	// last cell of 25 is "…Ngân hàng trong luồng" and 26 opens "Decoupled.".
	p25 := between(read, "--- page 25 of 45 ---", "--- page 26 of 45 ---")
	p26 := after(read, "--- page 26 of 45 ---")
	if !strings.Contains(p25, "công trên Ứng dụng của Ngân hàng trong luồng") {
		t.Errorf("page 25 does not end with the cell that continues on 26:\n%s", tail(p25, 400))
	}
	if first := strings.TrimSpace(firstLine(p26)); first != "Decoupled." {
		t.Errorf("page 26 should open with the rest of that cell, opens with %q", first)
	}
	// And the website around it is not the document.
	var body []string
	for _, l := range strings.Split(read, "\n") {
		if !strings.Contains(l, "page chrome left out") {
			body = append(body, l)
		}
	}
	for _, chrome := range []string{"TƯ VẤN PHÁP LUẬT", "Hệ thống thử nghiệm Open", "thuvienphapluat.vn", "9/15/26"} {
		if strings.Contains(strings.Join(body, "\n"), chrome) {
			t.Errorf("website chrome %q was read as part of the document", chrome)
		}
	}
	if strings.Count(read, "page chrome left out") != 3 {
		t.Errorf("each page should say it left chrome out")
	}

	for i, name := range []string{"page 25", "table region of page 25"} {
		imgs := results[3+i].images()
		if len(imgs) != 1 {
			t.Fatalf("%s: the model got %d images", name, len(imgs))
		}
		img, format := decodeImage(t, imgs[0])
		if format != "png" || inkFraction(img) < 0.01 {
			t.Errorf("%s: %s %v ink=%.4f", name, format, img.Bounds(), inkFraction(img))
		}
		if i == 0 && (img.Bounds().Dx() != 935 || img.Bounds().Dy() != 1210) {
			t.Errorf("page 25 is %v", img.Bounds())
		}
		if i == 1 && img.Bounds().Dx() < 1200 {
			t.Errorf("the table region is only %v: not a zoom", img.Bounds())
		}
	}
}

func between(s, from, to string) string {
	_, rest, _ := strings.Cut(s, from)
	mid, _, _ := strings.Cut(rest, to)
	return mid
}
func after(s, marker string) string { _, rest, _ := strings.Cut(s, marker); return rest }
func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

var tableRegion = map[string]any{"x0": 0.2, "y0": 0.55, "x1": 0.95, "y1": 0.8}

func TestSampleDocumentThroughClaude(t *testing.T) {
	sample := requireSample(t)
	model := newModelServer(t, anthropicHandler)
	st := newStack(t, claudeAdapter(t, model))
	sess := st.newSession(agenthub.AgentTypeClaude)
	model.setScript("xong",
		toolCall{Tool: "pdf_info", Args: map[string]any{"documentId": "doc1"}},
		toolCall{Tool: "pdf_search", Args: map[string]any{"documentId": "doc1", "query": "Authorization định dạng"}},
		toolCall{Tool: "pdf_readText", Args: map[string]any{"documentId": "doc1", "pages": "24-26"}},
		toolCall{Tool: "pdf_viewPages", Args: map[string]any{"documentId": "doc1", "pages": 25}},
		toolCall{Tool: "pdf_viewPages", Args: map[string]any{"documentId": "doc1", "pages": 25, "region": tableRegion}},
	)
	st.invoke(sess, "Bảng ở trang 25 có những tham số nào? Đối chiếu với ảnh trang.", st.copyFile("Thông tư 64_2024_TT-NHNN.pdf", sample))
	if p := strings.Join(model.prompts, "\n"); !contains(p, "doc1", "Thông tư 64_2024_TT-NHNN.pdf", "45 pages", "text layer: yes", "left sidebar") {
		t.Errorf("the prompt does not describe the document:\n%s", p)
	}
	assertSample(t, model.snapshotResults(), func(r seenResult) string { return r.text() })
}

func TestSampleDocumentThroughCodex(t *testing.T) {
	sample := requireSample(t)
	model := newModelServer(t, openaiHandler)
	st := newStack(t, codexAdapter(t, model))
	sess := st.newSession(agenthub.AgentTypeCodex)
	model.setScript("xong",
		js("pdf_info", `{documentId: "doc1"}`, `text(JSON.stringify(r.structuredContent))`),
		js("pdf_search", `{documentId: "doc1", query: "Authorization định dạng"}`, `text(JSON.stringify(r.structuredContent))`),
		js("pdf_readText", `{documentId: "doc1", pages: "24-26"}`, `text(r.content[0].text)`),
		js("pdf_viewPages", `{documentId: "doc1", pages: 25}`, `text(r.content[0].text); `+forwardImages),
		js("pdf_viewPages", `{documentId: "doc1", pages: 25, region: {x0: 0.2, y0: 0.55, x1: 0.95, y1: 0.8}}`, `text(r.content[0].text); `+forwardImages),
	)
	st.invoke(sess, "Bảng ở trang 25 có những tham số nào? Đối chiếu với ảnh trang.", st.copyFile("Thông tư 64_2024_TT-NHNN.pdf", sample))
	if p := strings.Join(model.prompts, "\n"); !contains(p, "doc1", "Thông tư 64_2024_TT-NHNN.pdf", "45 pages", "text layer: yes", "left sidebar", "image(c)") {
		t.Errorf("the prompt does not describe the document:\n%s", p)
	}
	assertSample(t, model.snapshotResults(), execOutput)
}
