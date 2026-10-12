package pdfdoc

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
)

// Render limits. A page image is read by a model, and a model downsizes what it
// is given — Claude's long edge ceiling is 1568 px — so more pixels than that
// cost bytes and buy nothing, except when only a part of the page is asked for
// (a table), where the part gets the whole budget.
const (
	DefaultDPI     = 110
	MinDPI         = 50
	MaxDPI         = 300
	DefaultMaxEdge = 1568
	MaxMaxEdge     = 2400
	// An image bigger than this as PNG is sent as JPEG instead.
	pngBudget = 1200 << 10
	jpegQ     = 85
	// What one call may return in total, so a result cannot grow without bound.
	MaxRenderBytes = 6 << 20
)

// Region is part of a page, as fractions of its width and height measured from
// the top-left corner: {0,0,1,0.5} is the top half.
type Region struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

func (r Region) valid() bool {
	return r.X0 >= 0 && r.Y0 >= 0 && r.X1 <= 1 && r.Y1 <= 1 && r.X1-r.X0 >= 0.02 && r.Y1-r.Y0 >= 0.02
}

// RenderOptions describe one page image.
type RenderOptions struct {
	// DPI of the render, MinDPI..MaxDPI; 0 = DefaultDPI. Capped by MaxEdge.
	DPI int
	// MaxEdge is the longest side in pixels, up to MaxMaxEdge; 0 = DefaultMaxEdge.
	MaxEdge int
	// Region renders only part of the page, at up to the full MaxEdge — the way
	// to read small print in a table.
	Region *Region
	// Format is "png", "jpeg" or "" (PNG unless it would be large).
	Format string
}

// Image is a rendered page.
type Image struct {
	Page   int     `json:"page"`
	Data   []byte  `json:"-"`
	MIME   string  `json:"mimeType"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	DPI    float64 `json:"dpi"`
	Region *Region `json:"region,omitempty"`
}

func (o RenderOptions) normalised() (RenderOptions, error) {
	if o.DPI == 0 {
		o.DPI = DefaultDPI
	}
	if o.DPI < MinDPI || o.DPI > MaxDPI {
		return o, fmt.Errorf("%w: dpi must be between %d and %d", ErrPageRange, MinDPI, MaxDPI)
	}
	if o.MaxEdge == 0 {
		o.MaxEdge = DefaultMaxEdge
	}
	if o.MaxEdge < 200 || o.MaxEdge > MaxMaxEdge {
		return o, fmt.Errorf("%w: maxEdge must be between 200 and %d", ErrPageRange, MaxMaxEdge)
	}
	switch o.Format {
	case "", "png", "jpeg":
	default:
		return o, fmt.Errorf("%w: format must be png or jpeg", ErrPageRange)
	}
	if o.Region != nil && !o.Region.valid() {
		return o, fmt.Errorf("%w: region must be fractions of the page, within 0..1, at least 2%% wide and tall", ErrPageRange)
	}
	return o, nil
}

// Render draws the given pages (1-based; at most MaxRenderPages). The total size
// of what comes back is bounded by MaxRenderBytes; a call that would exceed it
// fails rather than returning fewer pages than asked for.
func (e *Engine) Render(ctx context.Context, src Source, pages []int, o RenderOptions) ([]Image, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: no pages named", ErrPageRange)
	}
	if len(pages) > MaxRenderPages {
		return nil, fmt.Errorf("%w: %d pages in one call, limit %d", ErrPageRange, len(pages), MaxRenderPages)
	}
	o, err := o.normalised()
	if err != nil {
		return nil, err
	}
	var out []Image
	total := 0
	err = e.open(ctx, src, func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, count int) error {
		if err := checkPages(pages, count); err != nil {
			return err
		}
		entry := e.docs.get(src.key(), count)
		for _, n := range pages {
			if err := ctx.Err(); err != nil {
				return err
			}
			key := renderKey(src.key(), n, o)
			img, ok := e.renders.get(key)
			if !ok {
				pi, err := e.readPage(p, doc, entry, n-1)
				if err != nil {
					return err
				}
				img, err = renderPage(p, doc, n, pi, o)
				if err != nil {
					return err
				}
				e.renders.put(key, img)
			}
			total += len(img.Data)
			if total > MaxRenderBytes {
				return fmt.Errorf("%w: these %d pages come to over %d MiB as images; ask for fewer pages or a lower dpi", ErrPageRange, len(pages), MaxRenderBytes>>20)
			}
			out = append(out, img)
		}
		return nil
	})
	return out, err
}

func renderPage(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, n int, pi *pageInfo, o RenderOptions) (Image, error) {
	// The size to draw: the page (or the part of it) at the asked DPI, brought
	// within the long-edge ceiling.
	w, h := pi.w, pi.h
	req := requests.RenderPageInPixels{Page: pageRef(doc, n-1)}
	if o.Region != nil {
		r := *o.Region
		w, h = pi.w*(r.X1-r.X0), pi.h*(r.Y1-r.Y0)
		req.Crop = &requests.RenderPageCrop{X: pi.w * r.X0, Y: pi.h * r.Y0, Width: w, Height: h}
	}
	scale := float64(o.DPI) / 72
	if o.Region != nil {
		// A region is asked for to read small print: give it the whole budget.
		scale = math.Max(scale, float64(o.MaxEdge)/math.Max(w, h))
	}
	pxW, pxH := w*scale, h*scale
	if long := math.Max(pxW, pxH); long > float64(o.MaxEdge) {
		f := float64(o.MaxEdge) / long
		pxW, pxH = pxW*f, pxH*f
	}
	req.Width, req.Height = int(math.Max(1, math.Round(pxW))), int(math.Max(1, math.Round(pxH)))

	rendered, err := p.RenderPageInPixels(&req)
	if err != nil {
		return Image{}, fmt.Errorf("%w: page %d could not be drawn: %v", ErrCorrupt, n, err)
	}
	defer rendered.Cleanup()
	pic := rendered.Result.RenderedImage
	if pic == nil {
		return Image{}, fmt.Errorf("%w: page %d drew nothing", ErrCorrupt, n)
	}
	data, mime, err := encode(pic, o.Format)
	if err != nil {
		return Image{}, err
	}
	b := pic.Bounds()
	img := Image{Page: n, Data: data, MIME: mime, Width: b.Dx(), Height: b.Dy(), Region: o.Region}
	img.DPI = math.Round(float64(b.Dx()) / (w / 72))
	return img, nil
}

func encode(pic image.Image, format string) ([]byte, string, error) {
	// Draw onto white: a page with transparency would otherwise be black behind
	// its text in a viewer that ignores alpha.
	b := pic.Bounds()
	flat := image.NewRGBA(b)
	draw.Draw(flat, b, image.White, image.Point{}, draw.Src)
	draw.Draw(flat, b, pic, b.Min, draw.Over)

	if format != "jpeg" {
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestSpeed}
		if err := enc.Encode(&buf, flat); err != nil {
			return nil, "", fmt.Errorf("pdfdoc: encode png: %w", err)
		}
		if format == "png" || buf.Len() <= pngBudget {
			return buf.Bytes(), "image/png", nil
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: jpegQ}); err != nil {
		return nil, "", fmt.Errorf("pdfdoc: encode jpeg: %w", err)
	}
	return buf.Bytes(), "image/jpeg", nil
}
