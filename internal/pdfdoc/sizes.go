package pdfdoc

import (
	"context"
	"fmt"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
)

// PageSizes is the size of every page in points (1/72 in), rotation applied —
// what a viewer needs to lay out a column of pages before any of them is drawn.
// It reads page boxes only, never text, so it is cheap on a long document.
func (e *Engine) PageSizes(ctx context.Context, src Source) ([][2]float64, error) {
	var sizes [][2]float64
	err := e.open(ctx, src, func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, pages int) error {
		sizes = make([][2]float64, 0, pages)
		for i := 0; i < pages; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			r, err := p.FPDF_GetPageSizeByIndexF(&requests.FPDF_GetPageSizeByIndexF{Document: doc, Index: i})
			if err != nil {
				return fmt.Errorf("%w: page %d: %v", ErrCorrupt, i+1, err)
			}
			sizes = append(sizes, [2]float64{float64(r.Size.Width), float64(r.Size.Height)})
		}
		return nil
	})
	return sizes, err
}
