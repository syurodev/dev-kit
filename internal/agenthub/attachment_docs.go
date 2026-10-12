package agenthub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/dslipak/pdf"
)

// Office and PDF files are binary containers, so they cannot ride as the text
// resource a source file does. DevKit reads the text out of them itself and
// sends that: the agent advertises embedded text, not a PDF or Word reader, and
// the person attaching "the spec" means what it says, not its bytes.
//
// Extraction is deliberately plain. Layout, images, comments, tracked changes,
// formulas (only their cached result), charts and fonts are left behind: this
// is "what a model needs to read the document", not a converter. Everything
// here is pure Go — no Office, no poppler, no cgo — so it behaves the same on
// every platform DevKit ships for.

// maxExtractedBytes bounds the text one attachment contributes to a prompt.
// The 8 MiB file limit bounds what is read; this bounds what a prompt carries
// (a 4 MiB spreadsheet is millions of cells), and a cut is said out loud.
const maxExtractedBytes = 1 << 20

// maxPartBytes bounds one decompressed part of an Office zip, so a small
// archive that inflates to gigabytes (a zip bomb) cannot take the process.
const maxPartBytes = 64 << 20

// legacyOffice names the old binary Word and Excel formats DevKit does not
// read: no maintained pure-Go reader exists for them, and a hand-rolled parser
// is the kind of thing that is wrong in ways nobody notices. Refused by name,
// with the way out, rather than as "not text".
func legacyOffice(ext string) string {
	switch strings.ToLower(ext) {
	case ".doc", ".dot":
		return "Word"
	case ".xls", ".xlt":
		return "Excel"
	}
	return ""
}

// documentKind names the extractor for a file extension, or "" when the file
// is not one of the formats read here.
func documentKind(ext string) string {
	switch strings.ToLower(ext) {
	case ".pdf":
		return "pdf"
	case ".docx", ".docm":
		return "docx"
	case ".xlsx", ".xlsm":
		return "xlsx"
	}
	return ""
}

// extractDocument returns the readable text of a document, capped at
// maxExtractedBytes. An extractor panicking on a malformed file (the PDF and
// BIFF readers are third-party and do) is a refused attachment, not a crash.
func extractDocument(kind string, body []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("could not read the file (%v)", r)
		}
	}()
	switch kind {
	case "pdf":
		text, err = extractPDF(body)
	case "docx":
		text, err = extractDOCX(body)
	case "xlsx":
		text, err = extractXLSX(body)
	default:
		return "", errors.New("unsupported document type")
	}
	if err != nil {
		return "", err
	}
	text = strings.ToValidUTF8(text, "�")
	if strings.TrimSpace(text) == "" {
		return "", errors.New("it has no text to read (a scan or an image-only file?)")
	}
	return capText(text), nil
}

// capText cuts at a rune boundary and says how much was left out.
func capText(text string) string {
	if len(text) <= maxExtractedBytes {
		return text
	}
	cut := maxExtractedBytes
	for cut > 0 && (text[cut]&0xC0) == 0x80 {
		cut--
	}
	return text[:cut] + fmt.Sprintf("\n\n[… DevKit cut this attachment here: %d more bytes of text were not sent]", len(text)-cut)
}

// ---- PDF ----

func extractPDF(body []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("could not read the PDF (%v)", err)
	}
	var out strings.Builder
	pages := reader.NumPage()
	for i := 1; i <= pages && out.Len() <= maxExtractedBytes; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		// A page that will not decode costs that page, not the document.
		text := pageText(page)
		if strings.TrimSpace(text) == "" {
			continue
		}
		if pages > 1 {
			fmt.Fprintf(&out, "--- page %d ---\n", i)
		}
		out.WriteString(strings.TrimSpace(text))
		out.WriteString("\n\n")
	}
	return out.String(), nil
}

// pageText lays a page's text runs out by position: lines top to bottom, runs
// left to right. The library's own plain-text reader follows the order the
// runs sit in the file, and a producer that writes a fallback font's glyphs
// (the dotted vowels of Vietnamese, say) after everything else puts them at
// the end of the page instead of in their words.
func pageText(page pdf.Page) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	runs := page.Content().Text
	sort.SliceStable(runs, func(a, b int) bool {
		if d := runs[a].Y - runs[b].Y; d > lineTolerance(runs[a], runs[b]) || d < -lineTolerance(runs[a], runs[b]) {
			return runs[a].Y > runs[b].Y
		}
		return runs[a].X < runs[b].X
	})
	var out strings.Builder
	var prev *pdf.Text
	for i := range runs {
		run := &runs[i]
		if prev != nil {
			if d := run.Y - prev.Y; d > lineTolerance(*prev, *run) || d < -lineTolerance(*prev, *run) {
				out.WriteByte('\n')
			} else if gap := run.X - (prev.X + prev.W); gap > 0.2*run.FontSize && !strings.HasSuffix(out.String(), " ") && !strings.HasPrefix(run.S, " ") {
				if gap > 4*run.FontSize {
					out.WriteByte('\t')
				} else {
					out.WriteByte(' ')
				}
			}
		}
		// U+FFFD is the reader saying a glyph has no Unicode mapping (a
		// ligature piece, a zero-width mark); there is nothing to show for it.
		out.WriteString(strings.ReplaceAll(run.S, "\uFFFD", ""))
		prev = run
	}
	return out.String()
}

// lineTolerance is how far apart two runs may sit vertically and still be one
// line: a fraction of the type size, so a superscript stays with its word.
func lineTolerance(a, b pdf.Text) float64 {
	size := a.FontSize
	if b.FontSize > size {
		size = b.FontSize
	}
	if size <= 0 {
		size = 10
	}
	return size * 0.5
}

// ---- Office Open XML (docx, xlsx) ----

func openZip(body []byte) (*zip.Reader, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("not a valid Office file (%v)", err)
	}
	return zr, nil
}

// zipPart reads one part by name, refusing one that inflates past maxPartBytes.
func zipPart(zr *zip.Reader, name string) ([]byte, error) {
	for _, file := range zr.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, maxPartBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxPartBytes {
			return nil, fmt.Errorf("%s is too large once unpacked", name)
		}
		return data, nil
	}
	return nil, fmt.Errorf("%s is missing", name)
}

// extractDOCX reads word/document.xml: paragraphs, line breaks, tabs and table
// cells. Text boxes and headers/footers are not read.
func extractDOCX(body []byte) (string, error) {
	zr, err := openZip(body)
	if err != nil {
		return "", err
	}
	data, err := zipPart(zr, "word/document.xml")
	if err != nil {
		return "", err
	}
	var out strings.Builder
	dec := xml.NewDecoder(bytes.NewReader(data))
	inText := false
	cells := 0 // table cells open: a paragraph inside one does not end the row's line
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("could not read the document (%v)", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tc":
				cells++
			case "tab":
				out.WriteByte('\t')
			case "br", "cr":
				out.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if cells == 0 {
					out.WriteByte('\n')
				} else if s := out.String(); s != "" && !strings.HasSuffix(s, "\t") && !strings.HasSuffix(s, "\n") && !strings.HasSuffix(s, " ") {
					out.WriteByte(' ')
				}
			case "tc":
				cells--
				trimmed := strings.TrimRight(out.String(), " ")
				out.Reset()
				out.WriteString(trimmed)
				out.WriteByte('\t')
			case "tr":
				trimmed := strings.TrimRight(out.String(), "\t")
				out.Reset()
				out.WriteString(trimmed)
				out.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				out.Write(t)
			}
		}
		if out.Len() > maxExtractedBytes*2 {
			break
		}
	}
	return out.String(), nil
}

type xlsxSheetRef struct {
	Name string `xml:"name,attr"`
	RID  string `xml:"id,attr"`
}

// extractXLSX reads every sheet as tab-separated rows under its name. Cells
// hold what Excel last calculated; a date is its serial number, because the
// number format that would show it as a date is not read.
func extractXLSX(body []byte) (string, error) {
	zr, err := openZip(body)
	if err != nil {
		return "", err
	}
	shared := readSharedStrings(zr)

	workbook, err := zipPart(zr, "xl/workbook.xml")
	if err != nil {
		return "", err
	}
	var wb struct {
		Sheets []xlsxSheetRef `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(workbook, &wb); err != nil {
		return "", fmt.Errorf("could not read the workbook (%v)", err)
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if data, err := zipPart(zr, "xl/_rels/workbook.xml.rels"); err == nil {
		_ = xml.Unmarshal(data, &rels)
	}
	targets := map[string]string{}
	for _, rel := range rels.Items {
		target := rel.Target
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(target, "/")
		} else {
			target = path.Join("xl", target)
		}
		targets[rel.ID] = target
	}

	var out strings.Builder
	for i, sheet := range wb.Sheets {
		target := targets[sheet.RID]
		if target == "" {
			target = fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		}
		data, err := zipPart(zr, target)
		if err != nil {
			continue
		}
		rows := readSheetRows(data, shared)
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&out, "## Sheet: %s\n", sheet.Name)
		for _, row := range rows {
			out.WriteString(row)
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
		if out.Len() > maxExtractedBytes*2 {
			break
		}
	}
	return out.String(), nil
}

func readSharedStrings(zr *zip.Reader) []string {
	data, err := zipPart(zr, "xl/sharedStrings.xml")
	if err != nil {
		return nil
	}
	var strs []string
	dec := xml.NewDecoder(bytes.NewReader(data))
	var cur strings.Builder
	inSI, inT, inPhonetic := false, false, false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "rPh":
				inPhonetic = true
			case "t":
				inT = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				strs = append(strs, cur.String())
				inSI = false
			case "rPh":
				inPhonetic = false
			case "t":
				inT = false
			}
		case xml.CharData:
			if inSI && inT && !inPhonetic {
				cur.Write(t)
			}
		}
	}
	return strs
}

// readSheetRows turns one worksheet part into tab-separated rows, keeping
// empty cells as tabs so columns stay in line.
func readSheetRows(data []byte, shared []string) []string {
	var rows []string
	dec := xml.NewDecoder(bytes.NewReader(data))
	var (
		cells                  []string
		col                    int
		cellType               string
		value                  strings.Builder
		inV, inInlineT, inCell bool
	)
	flushCell := func() {
		text := value.String()
		if cellType == "s" {
			if n, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && n >= 0 && n < len(shared) {
				text = shared[n]
			}
		}
		for len(cells) < col {
			cells = append(cells, "")
		}
		cells = append(cells, strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(text))
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				cells = cells[:0]
			case "c":
				inCell = true
				cellType = ""
				value.Reset()
				col = len(cells)
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "t":
						cellType = attr.Value
					case "r":
						if c := columnIndex(attr.Value); c >= 0 {
							col = c
						}
					}
				}
			case "v":
				inV = true
			case "t":
				inInlineT = inCell
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inInlineT = false
			case "c":
				flushCell()
				inCell = false
			case "row":
				line := strings.TrimRight(strings.Join(cells, "\t"), "\t")
				if strings.TrimSpace(line) != "" {
					rows = append(rows, line)
				}
			}
		case xml.CharData:
			if inV || inInlineT {
				value.Write(t)
			}
		}
	}
	return rows
}

// columnIndex reads the column of a reference like "AB12" as a zero-based index.
func columnIndex(ref string) int {
	n := 0
	seen := false
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			if r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			} else {
				break
			}
		}
		seen = true
		n = n*26 + int(r-'A') + 1
	}
	if !seen {
		return -1
	}
	return n - 1
}

// supportedDocumentTypes is what the refusal message offers.
func supportedDocumentTypes() string {
	kinds := []string{"PDF", "Word (.docx)", "Excel (.xlsx)", "text, Markdown, HTML", "images"}
	return strings.Join(kinds, ", ")
}
