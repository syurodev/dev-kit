// Package codexsessions stores what DevKit observed of an external Codex CLI
// session, reported through the lifecycle hook contract in
// internal/codexhook.
//
// DevKit does not run Codex; Codex runs it own copy, in the user's own
// terminal, and calls back into DevKit's MCP server as its hooks fire. This
// package is the other end of that: turning one `codexhook.Call` at a time
// into a row a person can read later as a transcript.
//
// Everything here is local-only, like internal/apiclient's history and
// internal/database's query history: it says what THIS MACHINE observed, not
// what the project is, and it has no path to sync.
package codexsessions

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"devkit/internal/codexhook"
	"devkit/internal/crypto"
)

// DetailKeyID and DetailRecordType are the sealed payload's identity.
// Distinct from every other key id in the app, so an envelope written here
// cannot open as anything else.
const (
	DetailKeyID      = "codex-hook-detail"
	DetailRecordType = "codex-hook-event"
)

// EventLimit bounds how many events ONE session keeps. It exists so a very
// long-running Codex session cannot grow the table without bound; it is not a
// retention policy for old sessions, which is a coarser knob (Clear) left to
// the person, the same split internal/apiclient's history makes.
const EventLimit = 500

// Sealer is the vault's sealing surface, declared locally so this package
// stays testable without a real vault and does not import internal/vault.
type Sealer interface {
	SealV2(ad crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
	OpenWithAD(envelope crypto.Envelope, ad crypto.AssociatedData) ([]byte, error)
}

// Event is one hook call, as stored.
//
// Prompt, ToolInput, ToolResponse and LastMessage are the sealed half: a
// prompt can carry a secret exactly as easily as a URL query string can, and
// DevKit did not write either. They come back empty when the vault cannot
// open them — a locked vault, not a missing or corrupt event — which is why
// they are never used to decide whether an event exists.
type Event struct {
	ID             string          `json:"id"`
	SessionID      string          `json:"sessionId"`
	Event          codexhook.Event `json:"event"`
	CWD            string          `json:"cwd"`
	TurnID         string          `json:"turnId,omitempty"`
	Model          string          `json:"model,omitempty"`
	PermissionMode string          `json:"permissionMode,omitempty"`
	ToolName       string          `json:"toolName,omitempty"`
	// Summary is plaintext and safe to list without opening the vault — see
	// codexhook.Call.Summary.
	Summary      string          `json:"summary"`
	Prompt       string          `json:"prompt,omitempty"`
	ToolInput    json.RawMessage `json:"toolInput,omitempty"`
	ToolResponse json.RawMessage `json:"toolResponse,omitempty"`
	LastMessage  string          `json:"lastMessage,omitempty"`
	OccurredAt   time.Time       `json:"occurredAt"`
}

// Session is one Codex session, summarized from its last event — the answer
// to "what is this row, and is it still going".
type Session struct {
	ID          string          `json:"id"`
	CWD         string          `json:"cwd"`
	Model       string          `json:"model"`
	StartedAt   time.Time       `json:"startedAt"`
	LastEventAt time.Time       `json:"lastEventAt"`
	LastEvent   codexhook.Event `json:"lastEvent"`
	EventCount  int             `json:"eventCount"`
}

// sensitive is the JSON shape sealed into detail_envelope. A struct rather
// than four separate envelopes: one crypto operation per row, and the four
// fields are never meaningful apart from each other.
type sensitive struct {
	Prompt       string          `json:"prompt,omitempty"`
	ToolInput    json.RawMessage `json:"toolInput,omitempty"`
	ToolResponse json.RawMessage `json:"toolResponse,omitempty"`
	LastMessage  string          `json:"lastMessage,omitempty"`
}

func (s sensitive) empty() bool {
	return s.Prompt == "" && len(s.ToolInput) == 0 && len(s.ToolResponse) == 0 && s.LastMessage == ""
}

// Service owns the table. Nil-safe on every method: a container that never
// called SetSealer simply cannot record or read, and installing Codex support
// without a vault open must not panic the app.
type Service struct {
	db     *sql.DB
	table  string
	sealer Sealer
}

func NewService(db *sql.DB, table string, sealer Sealer) *Service {
	return &Service{db: db, table: table, sealer: sealer}
}

func eventID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}

// Record stores one hook call. It is the only write this package does, and it
// is meant to be cheap and to never block a Codex turn on anything slower
// than a local insert — the hook that called it is waiting on the reply.
func (s *Service) Record(call codexhook.Call) error {
	if s == nil || s.db == nil {
		return nil
	}
	id := eventID()
	detail := sensitive{
		Prompt: call.Prompt, ToolInput: call.ToolInput,
		ToolResponse: call.ToolResponse, LastMessage: call.LastMessage,
	}
	envelope := ""
	if !detail.empty() && s.sealer != nil {
		plaintext, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		ad := crypto.AssociatedData{RecordType: DetailRecordType, RecordID: id, KeyID: DetailKeyID}
		sealed, err := s.sealer.SealV2(ad, plaintext)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(sealed)
		if err != nil {
			return err
		}
		envelope = string(raw)
	}

	now := time.Now().UTC()
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO `+s.table+` (id, session_id, event, cwd, turn_id, model,
			permission_mode, tool_name, summary, detail_envelope, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, call.SessionID, string(call.Event), call.CWD, call.TurnID, call.Model,
		call.PermissionMode, call.ToolName, call.Summary(), envelope,
		now.Format(time.RFC3339Nano),
	); err != nil {
		return err
	}

	// Trim here rather than on a schedule: the row that pushes one session
	// past the limit is the only moment the limit can be exceeded.
	_, err := s.db.ExecContext(context.Background(),
		`DELETE FROM `+s.table+` WHERE session_id = ? AND id NOT IN (
			SELECT id FROM `+s.table+` WHERE session_id = ? ORDER BY occurred_at DESC LIMIT ?
		)`, call.SessionID, call.SessionID, EventLimit)
	return err
}

// Sessions lists every session this machine has observed, newest activity
// first. It needs no unlocked vault: everything it reads is plaintext.
func (s *Service) Sessions() ([]Session, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT session_id, cwd, model, event, occurred_at, started_at, cnt
		FROM (
			SELECT session_id, cwd, model, event, occurred_at,
				MIN(occurred_at) OVER (PARTITION BY session_id) AS started_at,
				COUNT(*) OVER (PARTITION BY session_id) AS cnt,
				ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY occurred_at DESC, id DESC) AS rn
			FROM `+s.table+`
		)
		WHERE rn = 1
		ORDER BY occurred_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var session Session
		var event, lastAt, startedAt string
		if err := rows.Scan(&session.ID, &session.CWD, &session.Model, &event,
			&lastAt, &startedAt, &session.EventCount); err != nil {
			return nil, err
		}
		session.LastEvent = codexhook.Event(event)
		session.LastEventAt, _ = time.Parse(time.RFC3339Nano, lastAt)
		session.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
		out = append(out, session)
	}
	return out, rows.Err()
}

// Events returns one session's transcript, oldest first — the order a reader
// follows it in. limit <= 0 means EventLimit, which is also the most any
// session can hold.
func (s *Service) Events(sessionID string, limit int) ([]Event, error) {
	if s == nil || s.db == nil || sessionID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > EventLimit {
		limit = EventLimit
	}
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT id, session_id, event, cwd, turn_id, model, permission_mode,
			tool_name, summary, detail_envelope, occurred_at
		FROM `+s.table+`
		WHERE session_id = ?
		ORDER BY occurred_at ASC
		LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var event Event
		var eventName, envelope, occurredAt string
		if err := rows.Scan(&event.ID, &event.SessionID, &eventName, &event.CWD,
			&event.TurnID, &event.Model, &event.PermissionMode, &event.ToolName,
			&event.Summary, &envelope, &occurredAt); err != nil {
			return nil, err
		}
		event.Event = codexhook.Event(eventName)
		event.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurredAt)
		if envelope != "" && s.sealer != nil {
			var sealed crypto.Envelope
			if json.Unmarshal([]byte(envelope), &sealed) == nil {
				ad := crypto.AssociatedData{RecordType: DetailRecordType, RecordID: event.ID, KeyID: DetailKeyID}
				// A detail that will not open is a locked vault, not a
				// broken row: the rest of the event is still worth showing.
				if plaintext, openErr := s.sealer.OpenWithAD(sealed, ad); openErr == nil {
					var detail sensitive
					if json.Unmarshal(plaintext, &detail) == nil {
						event.Prompt, event.ToolInput = detail.Prompt, detail.ToolInput
						event.ToolResponse, event.LastMessage = detail.ToolResponse, detail.LastMessage
					}
				}
			}
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

// ClearSession forgets one session's transcript.
func (s *Service) ClearSession(sessionID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(context.Background(),
		`DELETE FROM `+s.table+` WHERE session_id = ?`, sessionID)
	return err
}

// Clear forgets every session this machine has observed.
func (s *Service) Clear() error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM `+s.table)
	return err
}
