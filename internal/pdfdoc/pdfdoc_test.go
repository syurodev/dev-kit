package pdfdoc

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The fixtures in testdata were written by reportlab (webprint, scan, mixed),
// with pypdf for the encrypted one: a six-page "web print" with a repeating
// header, footer and left sidebar and a table that runs over pages 2–4, an
// image-only scan, a text page followed by a scan, an AES-encrypted file, and
// damaged ones. DEVKIT_PDF_SAMPLE points at a real long document for the
// optional test at the bottom.

var (
	engine     *Engine
	engineOnce sync.Once
	cacheDir   string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pdfdoc-test")
	if err != nil {
		panic(err)
	}
	cacheDir = dir
	code := m.Run()
	if engine != nil {
		engine.Close()
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func eng() *Engine {
	engineOnce.Do(func() { engine = New(Options{CacheDir: cacheDir}) })
	return engine
}

func load(t *testing.T, name string) Source {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return Source{Data: data}
}

func TestInspectDescribesTheDocumentAndItsChrome(t *testing.T) {
	info, err := eng().Inspect(context.Background(), load(t, "webprint.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Pages != 6 || info.Title != "Thông tư mẫu về xác thực" || info.Author != "DevKit tests" {
		t.Fatalf("info = %+v", info)
	}
	if info.PageSize != [2]float64{612, 792} {
		t.Fatalf("page size = %v", info.PageSize)
	}
	if info.TextLayer.State != "full" || info.TextLayer.Count != 0 {
		t.Fatalf("text layer = %+v", info.TextLayer)
	}
	joined := strings.Join(info.Chrome, "; ")
	for _, want := range []string{"running header", "running footer", "left sidebar"} {
		if !strings.Contains(joined, want) {
			t.Errorf("chrome %q does not mention %q", joined, want)
		}
	}
	if info.OCR.Available || !strings.Contains(info.OCR.Note, "no OCR") {
		t.Fatalf("ocr = %+v", info.OCR)
	}
}

func TestReadTextLeavesOutPageChromeAndSaysSo(t *testing.T) {
	src := load(t, "webprint.pdf")
	pages, err := eng().ReadText(context.Background(), src, []int{2}, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p := pages[0]
	if !p.HasTextLayer || p.ChromeLines < 8 || len(p.ChromeSample) == 0 {
		t.Fatalf("page = %+v", p)
	}
	for _, want := range []string{"Điều 2. Quy định về xác thực khách hàng", "Tham số", "Field-01", "Field-08"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("body is missing %q:\n%s", want, p.Text)
		}
	}
	for _, chrome := range []string{"TƯ VẤN PHÁP LUẬT", "Hệ thống thử nghiệm", "9/15/26", "example.test", "2/6"} {
		if strings.Contains(p.Text, chrome) {
			t.Errorf("chrome %q leaked into the body:\n%s", chrome, p.Text)
		}
	}

	// The table row stays one line the agent can split on.
	if !strings.Contains(p.Text, "Field-01\tString\tM") {
		t.Errorf("table row is not tab-separated:\n%s", p.Text)
	}

	with, err := eng().ReadText(context.Background(), src, []int{2}, TextOptions{IncludeChrome: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, chrome := range []string{"TƯ VẤN PHÁP LUẬT", "9/15/26", "example.test"} {
		if !strings.Contains(with[0].Text, chrome) {
			t.Errorf("includeChrome lost %q", chrome)
		}
	}
	if with[0].ChromeLines != 0 {
		t.Errorf("includeChrome still counts omitted lines: %d", with[0].ChromeLines)
	}
}

func TestATableRunningOverPagesIsReadAcrossThem(t *testing.T) {
	pages, err := eng().ReadText(context.Background(), load(t, "webprint.pdf"), []int{2, 3, 4}, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 3 {
		t.Fatalf("pages = %d", len(pages))
	}
	seen := 0
	for i, p := range pages {
		if p.Page != i+2 {
			t.Fatalf("page %d reported as %d", i+2, p.Page)
		}
		// The header row repeats on every sheet; it is the document, not chrome.
		if !strings.Contains(p.Text, "Tham số\tLoại\tBắt buộc\tMô tả") {
			t.Errorf("page %d lost the table's header row:\n%s", p.Page, p.Text)
		}
		seen += strings.Count(p.Text, "Field-")
	}
	if seen != 24 {
		t.Fatalf("rows read = %d, want 24 across three pages", seen)
	}
	if !strings.Contains(pages[1].Text, "Field-09") || strings.Contains(pages[1].Text, "Field-08\t") {
		t.Errorf("page 3 should start at Field-09:\n%s", pages[1].Text)
	}
}

func TestLayoutModeKeepsTheColumns(t *testing.T) {
	pages, err := eng().ReadText(context.Background(), load(t, "webprint.pdf"), []int{2}, TextOptions{Layout: true})
	if err != nil {
		t.Fatal(err)
	}
	var cols []int
	for _, l := range strings.Split(pages[0].Text, "\n") {
		if strings.HasPrefix(l, "Field-") {
			cols = append(cols, strings.Index(l, "String"))
		}
	}
	if len(cols) < 8 {
		t.Fatalf("rows = %d", len(cols))
	}
	for _, c := range cols {
		if c != cols[0] || c < 0 {
			t.Fatalf("column drifted: %v", cols)
		}
	}
}

func TestSearchFindsByMeaningAndSkipsChromeUnlessAsked(t *testing.T) {
	src := load(t, "webprint.pdf")
	res, err := eng().Search(context.Background(), src, SearchOptions{Query: "XAC THUC khach hang"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Complete || res.Searched != 6 || len(res.Hits) < 6 {
		t.Fatalf("result = %+v", res)
	}
	for i, h := range res.Hits {
		if h.Match != "phrase" || !strings.Contains(strings.ToLower(h.Snippet), "xác thực khách hàng") {
			t.Fatalf("hit %d = %+v", i, h)
		}
	}
	if res.Hits[0].Page != 1 {
		t.Fatalf("hits are not in page order: %+v", res.Hits[0])
	}

	// The sidebar is chrome: searching for its words finds nothing by default…
	none, err := eng().Search(context.Background(), src, SearchOptions{Query: "tư vấn pháp luật"})
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Hits) != 0 {
		t.Fatalf("sidebar text was searched: %+v", none.Hits)
	}
	// …and is found when asked for.
	some, err := eng().Search(context.Background(), src, SearchOptions{Query: "tư vấn pháp luật", IncludeChrome: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(some.Hits) != 6 {
		t.Fatalf("hits with chrome = %d", len(some.Hits))
	}

	// Page range and the hit cap are explicit.
	one, err := eng().Search(context.Background(), src, SearchOptions{Query: "khách hàng", FromPage: 3, ToPage: 3})
	if err != nil || one.Searched != 1 || one.Hits[0].Page != 3 {
		t.Fatalf("range search = %+v, %v", one, err)
	}
	capped, err := eng().Search(context.Background(), src, SearchOptions{Query: "khách hàng", MaxHits: 2})
	if err != nil || len(capped.Hits) != 2 || !capped.Truncated {
		t.Fatalf("capped = %+v, %v", capped, err)
	}
}

func TestAScanHasNoTextAndSaysItIsNotSearchable(t *testing.T) {
	src := load(t, "scan.pdf")
	info, err := eng().Inspect(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if info.TextLayer.State != "none" || info.TextLayer.Count != 2 || len(info.TextLayer.PagesWithoutText) != 2 {
		t.Fatalf("text layer = %+v", info.TextLayer)
	}
	pages, err := eng().ReadText(context.Background(), src, []int{1}, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pages[0].HasTextLayer || pages[0].Text != "" {
		t.Fatalf("a scan produced text: %+v", pages[0])
	}
	res, err := eng().Search(context.Background(), src, SearchOptions{Query: "biên bản"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 || len(res.PagesWithoutText) != 2 {
		t.Fatalf("search of a scan = %+v", res)
	}
	// It can still be looked at.
	imgs, err := eng().Render(context.Background(), src, []int{1}, RenderOptions{})
	if err != nil || len(imgs) != 1 {
		t.Fatalf("render = %v, %v", imgs, err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(imgs[0].Data))
	if err != nil || cfg.Width != imgs[0].Width || cfg.Height != imgs[0].Height || cfg.Width < 500 {
		t.Fatalf("decoded %+v, %v", cfg, err)
	}
}

func TestAMixedDocumentIsPartial(t *testing.T) {
	info, err := eng().Inspect(context.Background(), load(t, "mixed.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if info.TextLayer.State != "partial" || len(info.TextLayer.PagesWithoutText) != 1 || info.TextLayer.PagesWithoutText[0] != 2 {
		t.Fatalf("text layer = %+v", info.TextLayer)
	}
}

func TestUnreadableDocumentsAreRefusedByName(t *testing.T) {
	ctx := context.Background()
	for name, want := range map[string]error{
		"corrupt.pdf":   ErrCorrupt,
		"notpdf.pdf":    ErrNotPDF,
		"encrypted.pdf": ErrPasswordRequired,
	} {
		_, err := eng().Inspect(ctx, load(t, name))
		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", name, err, want)
		}
		if _, err := eng().ReadText(ctx, load(t, name), []int{1}, TextOptions{}); err == nil {
			t.Errorf("%s: ReadText answered as if it had read the file", name)
		}
		if _, err := eng().Render(ctx, load(t, name), []int{1}, RenderOptions{}); err == nil {
			t.Errorf("%s: Render answered as if it had read the file", name)
		}
	}
	wrong := load(t, "encrypted.pdf")
	wrong.Password = "nope"
	if _, err := eng().Inspect(ctx, wrong); !errors.Is(err, ErrPasswordRequired) {
		t.Errorf("wrong password: %v", err)
	}
	right := load(t, "encrypted.pdf")
	right.Password = "secret"
	right.Key = "encrypted-with-password"
	info, err := eng().Inspect(ctx, right)
	if err != nil || info.Pages != 2 {
		t.Fatalf("with the password: %+v, %v", info, err)
	}
}

func TestLimitsAreReportedNotApplied(t *testing.T) {
	ctx := context.Background()
	big := append([]byte("%PDF-1.4\n"), make([]byte, MaxFileBytes)...)
	if _, err := eng().Inspect(ctx, Source{Data: big}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversized: %v", err)
	}
	src := load(t, "webprint.pdf")
	if _, err := eng().ReadText(ctx, src, []int{7}, TextOptions{}); !errors.Is(err, ErrPageRange) {
		t.Errorf("page 7 of 6: %v", err)
	}
	if _, err := eng().ReadText(ctx, src, []int{0}, TextOptions{}); !errors.Is(err, ErrPageRange) {
		t.Errorf("page 0: %v", err)
	}
	many := make([]int, MaxReadPages+1)
	for i := range many {
		many[i] = 1
	}
	if _, err := eng().ReadText(ctx, src, many, TextOptions{}); !errors.Is(err, ErrPageRange) {
		t.Errorf("too many pages: %v", err)
	}
	if _, err := eng().Render(ctx, src, []int{1, 2, 3, 4, 5}, RenderOptions{}); !errors.Is(err, ErrPageRange) {
		t.Errorf("too many images: %v", err)
	}
	for _, bad := range []RenderOptions{{DPI: 10}, {DPI: 900}, {MaxEdge: 50}, {MaxEdge: 9000}, {Format: "gif"}, {Region: &Region{X0: 0.5, X1: 0.4, Y0: 0, Y1: 1}}} {
		if _, err := eng().Render(ctx, src, []int{1}, bad); !errors.Is(err, ErrPageRange) {
			t.Errorf("%+v accepted: %v", bad, err)
		}
	}
	if _, err := eng().Search(ctx, src, SearchOptions{Query: "   "}); err == nil {
		t.Error("an empty query was accepted")
	}
}

func TestRenderRegionZoomsAndIsCached(t *testing.T) {
	src := load(t, "webprint.pdf")
	full, err := eng().Render(context.Background(), src, []int{2}, RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if full[0].Width != 935 || full[0].Height != 1210 || full[0].DPI < 109 || full[0].DPI > 111 {
		t.Fatalf("default render = %dx%d @%v", full[0].Width, full[0].Height, full[0].DPI)
	}
	top := &Region{X0: 0.2, Y0: 0.05, X1: 0.95, Y1: 0.45}
	zoom, err := eng().Render(context.Background(), src, []int{2}, RenderOptions{Region: top})
	if err != nil {
		t.Fatal(err)
	}
	if zoom[0].DPI <= full[0].DPI*1.5 || zoom[0].Width > DefaultMaxEdge || zoom[0].Height > DefaultMaxEdge {
		t.Fatalf("zoom = %dx%d @%v", zoom[0].Width, zoom[0].Height, zoom[0].DPI)
	}
	if zoom[0].Region == nil {
		t.Fatal("the region is not reported back")
	}
	before := eng().renders.size
	again, err := eng().Render(context.Background(), src, []int{2}, RenderOptions{Region: top})
	if err != nil || !bytes.Equal(again[0].Data, zoom[0].Data) || eng().renders.size != before {
		t.Fatalf("second render was not served from the cache: %v", err)
	}
	jpeg, err := eng().Render(context.Background(), src, []int{2}, RenderOptions{Format: "jpeg"})
	if err != nil || jpeg[0].MIME != "image/jpeg" {
		t.Fatalf("jpeg = %v, %v", jpeg, err)
	}
	// Bounded: a 4-page call at the ceiling still fits the result budget.
	four, err := eng().Render(context.Background(), src, []int{1, 2, 3, 4}, RenderOptions{DPI: MaxDPI, MaxEdge: MaxMaxEdge})
	if err != nil {
		t.Fatalf("four pages at the ceiling: %v", err)
	}
	total := 0
	for _, i := range four {
		total += len(i.Data)
	}
	if total > MaxRenderBytes {
		t.Fatalf("a call returned %d bytes, over the %d bound", total, MaxRenderBytes)
	}
}

func TestConcurrentUseIsSafe(t *testing.T) {
	src := load(t, "webprint.pdf")
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				_, err := eng().ReadText(context.Background(), src, []int{1 + i%6}, TextOptions{})
				errs <- err
			case 1:
				_, err := eng().Search(context.Background(), src, SearchOptions{Query: "khách hàng"})
				errs <- err
			default:
				_, err := eng().Render(context.Background(), src, []int{1 + i%6}, RenderOptions{})
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestACancelledCallStopsBeforeWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := eng().ReadText(ctx, load(t, "webprint.pdf"), []int{1}, TextOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// With a real long document (a website print, 45 pages) at DEVKIT_PDF_SAMPLE.
func TestRealSampleDocument(t *testing.T) {
	path := os.Getenv("DEVKIT_PDF_SAMPLE")
	if path == "" {
		t.Skip("set DEVKIT_PDF_SAMPLE to a PDF to run this")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := Source{Data: data}
	info, err := eng().Inspect(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d pages; text layer %s; chrome %v", info.Pages, info.TextLayer.State, info.Chrome)
	if info.Pages < 1 || info.TextLayer.State == "" {
		t.Fatalf("info = %+v", info)
	}
}

func TestPageSizesAreReadWithoutReadingText(t *testing.T) {
	src := load(t, "webprint.pdf")
	sizes, err := eng().PageSizes(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	info, err := eng().Inspect(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(sizes) != info.Pages || info.Pages < 2 {
		t.Fatalf("%d sizes for %d pages", len(sizes), info.Pages)
	}
	if sizes[0] != info.PageSize {
		t.Fatalf("first page = %v, Inspect says %v", sizes[0], info.PageSize)
	}
	if _, err := eng().PageSizes(context.Background(), Source{Data: []byte("not a pdf")}); !errors.Is(err, ErrNotPDF) {
		t.Fatalf("not a pdf: %v", err)
	}
}
