package usagemetrics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type Batch struct {
	RunID                  string
	Sequence               int64
	Rows                   []DailyRow
	Dropped, WriteFailures int64
	At                     time.Time
}
type Store interface {
	Start(context.Context, string, time.Time) error
	Commit(context.Context, Batch) error
	Close(context.Context, string, time.Time) error
}
type Reader interface {
	Report(context.Context, string, Query) (Report, error)
}

// Options provides deterministic time and ticks for collector tests. Nil fields use UTC wall time and a two-second ticker.
type Options struct {
	Now   func() time.Time
	Ticks <-chan time.Time
}
type Collector struct {
	ticks             <-chan time.Time
	closing           bool
	inFlight          int
	idle              chan struct{}
	mu                sync.Mutex
	flushMu           sync.Mutex
	store             Store
	id                string
	active            map[Key]Stats
	pending           *Batch
	sequence          int64
	dropped, failures int64
	closed            bool
	stop              chan struct{}
	done              chan struct{}
	now               func() time.Time
}

func New(ctx context.Context, store Store) (*Collector, error) {
	return NewWithOptions(ctx, store, Options{})
}
func NewWithOptions(ctx context.Context, store Store, options Options) (*Collector, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	c := &Collector{store: store, id: hex.EncodeToString(id[:]), active: map[Key]Stats{}, stop: make(chan struct{}), done: make(chan struct{}), now: time.Now}
	if options.Now != nil {
		c.now = options.Now
	}
	c.ticks = options.Ticks
	c.idle = make(chan struct{})
	close(c.idle)
	if err := store.Start(ctx, c.id, c.now()); err != nil {
		return nil, err
	}
	go c.run()
	return c, nil
}
func (c *Collector) Record(o Observation) {
	if c == nil {
		return
	}
	r, ok := rowOf(o)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if !ok {
		c.dropped++
		return
	}
	v, exists := c.active[r.Key]
	if !exists && len(c.active) >= MaxKeys {
		c.dropped++
		return
	}
	v.Add(r.Stats)
	c.active[r.Key] = v
}

// Missing attribution is a coverage gap, not an invented workspace/vendor.
func (c *Collector) Drop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.dropped++
	}
}
func (c *Collector) run() {
	defer close(c.done)
	ticks := c.ticks
	if ticks == nil {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		ticks = t.C
	}
	for {
		select {
		case <-c.stop:
			return
		case <-ticks:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = c.Flush(ctx)
			cancel()
		}
	}
}

// An ambiguous failed commit retains the immutable pending batch. The SQL
// watermark makes retry safe even when commit succeeded but its reply was lost.
func (c *Collector) Flush(ctx context.Context) error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	c.mu.Lock()
	if c.pending == nil {
		c.sequence++
		b := &Batch{RunID: c.id, Sequence: c.sequence, Dropped: c.dropped, WriteFailures: c.failures, At: c.now(), Rows: make([]DailyRow, 0, len(c.active))}
		for k, v := range c.active {
			b.Rows = append(b.Rows, DailyRow{k, v})
		}
		c.active = map[Key]Stats{}
		c.pending = b
	}
	b := *c.pending
	c.mu.Unlock()
	if err := c.store.Commit(ctx, b); err != nil {
		c.mu.Lock()
		c.failures++
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.pending = nil
	c.mu.Unlock()
	return nil
}

// Begin/End bound in-flight measurements to shutdown; Add cannot race a drain.
func (c *Collector) Begin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return false
	}
	if c.inFlight == 0 {
		c.idle = make(chan struct{})
	}
	c.inFlight++
	return true
}
func (c *Collector) End() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inFlight--
	if c.inFlight == 0 {
		close(c.idle)
	}
}
func (c *Collector) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	if !c.closing {
		c.closing = true
		close(c.stop)
	}
	idle := c.idle
	c.mu.Unlock()
	select {
	case <-c.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-idle:
	case <-ctx.Done():
		return ctx.Err()
	}
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	for i := 0; i < 2; i++ {
		if err := c.Flush(ctx); err != nil {
			return err
		}
	}
	return c.store.Close(ctx, c.id, c.now())
}
