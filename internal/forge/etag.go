package forge

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// etagCache remembers the last answer to a GET with the validator the host
// sent, so the next request can be conditional. A 304 is served from here.
//
// The key carries a digest of the token: a private repository's answer cached
// for one account must not be served to another. The token itself is never
// stored. Bounded and least-recently-used.
type etagCache struct {
	mu    sync.Mutex
	max   int
	order *list.List
	items map[string]*list.Element
}

type etagEntry struct {
	key  string
	etag string
	body []byte
	next string // the Link rel=next, for a paged read
}

func newETagCache(max int) *etagCache {
	return &etagCache{max: max, order: list.New(), items: map[string]*list.Element{}}
}

func etagKey(token, url string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8]) + " " + url
}

func (c *etagCache) get(key string) (etagEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return etagEntry{}, false
	}
	c.order.MoveToFront(el)
	return *el.Value.(*etagEntry), true
}

func (c *etagCache) put(entry etagEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[entry.key]; ok {
		*el.Value.(*etagEntry) = entry
		c.order.MoveToFront(el)
		return
	}
	c.items[entry.key] = c.order.PushFront(&entry)
	for c.order.Len() > c.max {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*etagEntry).key)
	}
}
