package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sync"
	"time"
)

const (
	sqlSinkQueue = 1024
	sqlSinkBatch = 256
	// DefaultRetention is how long audit rows are kept before Prune removes them.
	DefaultRetention = 30 * 24 * time.Hour
)

// SQLSink persists events to a SQLite table. Record never blocks on SQLite
// while the queue has room; when the queue is full it writes synchronously
// (backpressure) rather than dropping — an audit trail that loses events is
// not an audit trail. A failed SQLite write is a different case: those
// events go to the fallback sink (normally the in-memory one) and are logged.
type SQLSink struct {
	db        *sql.DB
	table     string
	fallback  Sink
	retention time.Duration
	queue     chan Event
	done      chan struct{}
	closeOnce sync.Once
}

// NewSQLSink starts the writer goroutine. table is the fully-qualified table
// name (storage.TableAuditEvents); fallback may be nil.
func NewSQLSink(db *sql.DB, table string, fallback Sink) *SQLSink {
	s := &SQLSink{
		db:        db,
		table:     table,
		fallback:  fallback,
		retention: DefaultRetention,
		queue:     make(chan Event, sqlSinkQueue),
		done:      make(chan struct{}),
	}
	go s.run()
	return s
}

// Record implements Sink.
func (s *SQLSink) Record(event Event) {
	event.Metadata = RedactMap(event.Metadata)
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	select {
	case s.queue <- event:
	default:
		s.write([]Event{event}) // queue full: backpressure, never drop
	}
}

func (s *SQLSink) run() {
	defer close(s.done)
	for event, ok := <-s.queue; ok; event, ok = <-s.queue {
		batch := []Event{event}
	drain:
		for len(batch) < sqlSinkBatch {
			select {
			case next, more := <-s.queue:
				if !more {
					break drain
				}
				batch = append(batch, next)
			default:
				break drain
			}
		}
		s.write(batch)
	}
}

func (s *SQLSink) write(batch []Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.fail(batch, err)
		return
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO `+s.table+
		` (at, caller, subject, capability, scope, outcome, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		s.fail(batch, err)
		return
	}
	defer stmt.Close()
	for _, e := range batch {
		meta, _ := json.Marshal(e.Metadata) // already redacted; unmarshalable values become null
		if _, err := stmt.ExecContext(ctx,
			e.At.UTC().Format(time.RFC3339Nano), e.Caller, e.SubjectID, e.Capability, e.Scope, string(e.Outcome), string(meta),
		); err != nil {
			_ = tx.Rollback()
			s.fail(batch, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.fail(batch, err)
	}
}

func (s *SQLSink) fail(batch []Event, err error) {
	log.Printf("audit: sqlite write failed, keeping %d event(s) in memory: %v", len(batch), err)
	if s.fallback == nil {
		return
	}
	for _, e := range batch {
		s.fallback.Record(e)
	}
}

// Prune deletes rows older than the retention window.
func (s *SQLSink) Prune(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-s.retention).Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `DELETE FROM `+s.table+` WHERE at < ?`, cutoff)
	return err
}

// PruneLoop runs Prune every interval until ctx is done.
func (s *SQLSink) PruneLoop(ctx context.Context, every time.Duration) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Prune(ctx); err != nil {
					log.Printf("audit: prune: %v", err)
				}
			}
		}
	}()
}

// Close flushes the queue and stops the writer. Record must not be called after Close.
func (s *SQLSink) Close() {
	s.closeOnce.Do(func() { close(s.queue) })
	<-s.done
}
