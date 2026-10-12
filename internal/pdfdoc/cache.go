package pdfdoc

import (
	"container/list"
	"fmt"
	"sync"
)

// pageInfo is what is known about one page once it has been read.
type pageInfo struct {
	w, h   float64
	lines  []line
	chars  int // non-space characters of text
	images int // image objects, counted up to a cap
}

// hasText reports whether the page has a text layer worth reading. A handful of
// stray characters (a page number stamped on a scan) is not one.
func (p *pageInfo) hasText() bool { return p.chars >= 8 }

// docEntry is the analysis kept for one document: pages as they are read, and
// the chrome model once there are enough pages to build it.
type docEntry struct {
	key    string
	pages  int
	mu     sync.Mutex
	loaded map[int]*pageInfo
	chrome *chromeModel
}

func (d *docEntry) page(i int) *pageInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loaded[i]
}

func (d *docEntry) put(i int, p *pageInfo) {
	d.mu.Lock()
	d.loaded[i] = p
	d.mu.Unlock()
}

// docCache keeps the analysis of the last few documents, so a search after a
// read, or a second read of the same pages, costs nothing.
type docCache struct {
	mu    sync.Mutex
	max   int
	order *list.List // front = most recent
	byKey map[string]*list.Element
}

func newDocCache(max int) *docCache {
	return &docCache{max: max, order: list.New(), byKey: map[string]*list.Element{}}
}

func (c *docCache) get(key string, pages int) *docEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byKey[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*docEntry)
	}
	entry := &docEntry{key: key, pages: pages, loaded: map[int]*pageInfo{}}
	c.byKey[key] = c.order.PushFront(entry)
	for c.order.Len() > c.max {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.byKey, last.Value.(*docEntry).key)
	}
	return entry
}

// renderCache keeps rendered page images, bounded by their size: a page is a
// few hundred KB and an agent often asks for the same one twice while it
// compares it with the text.
type renderCache struct {
	mu    sync.Mutex
	max   int
	size  int
	order *list.List
	byKey map[string]*list.Element
}

type renderEntry struct {
	key string
	img Image
}

func newRenderCache(maxBytes int) *renderCache {
	return &renderCache{max: maxBytes, order: list.New(), byKey: map[string]*list.Element{}}
}

func renderKey(doc string, page int, o RenderOptions) string {
	r := ""
	if o.Region != nil {
		r = fmt.Sprintf("%.4f,%.4f,%.4f,%.4f", o.Region.X0, o.Region.Y0, o.Region.X1, o.Region.Y1)
	}
	return fmt.Sprintf("%s|%d|%d|%d|%s|%s", doc, page, o.DPI, o.MaxEdge, r, o.Format)
}

func (c *renderCache) get(key string) (Image, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byKey[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*renderEntry).img, true
	}
	return Image{}, false
}

func (c *renderCache) put(key string, img Image) {
	if len(img.Data) > c.max/2 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byKey[key]; ok {
		c.order.MoveToFront(el)
		return
	}
	c.byKey[key] = c.order.PushFront(&renderEntry{key: key, img: img})
	c.size += len(img.Data)
	for c.size > c.max && c.order.Len() > 1 {
		last := c.order.Back()
		e := last.Value.(*renderEntry)
		c.order.Remove(last)
		delete(c.byKey, e.key)
		c.size -= len(e.img.Data)
	}
}
