// Command mocksyncserver is a minimal, in-memory reference implementation of the
// DevKit sync contract (GET /v1/sync/session, POST /v1/sync/push,
// GET /v1/sync/pull). It exists to exercise the sync client end-to-end without a
// database, Keycloak, or Redis.
//
// It is NOT production code and deliberately zero-knowledge: it stores encrypted
// replication envelopes verbatim (opaque ciphertext) and never decrypts them.
// Auth is stubbed — any non-empty Bearer token is accepted and mapped to a
// stable account id derived from the token.
//
// Usage:
//
//	go run ./cmd/mocksyncserver [-addr 127.0.0.1:8787]
//
// Then point the app's sync base URL at http://127.0.0.1:8787 (the client allows
// plain HTTP for loopback hosts).
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	protocolVersion             = 1
	maxHTTPRequestBytes         = 4 << 20
	maxOperationCiphertextBytes = 1 << 20
	maxPullOperations           = 1000

	deviceHeader   = "X-DevKit-Device-ID"
	protocolHeader = "X-DevKit-Sync-Protocol"
)

// opView reads only the routing fields the server arbitrates on. The full
// operation JSON is stored and echoed back verbatim so client AEAD/AAD stays
// intact across devices.
type opView struct {
	IdempotencyKey string `json:"idempotency_key"`
	Operation      string `json:"operation"`
	Envelope       struct {
		RecordID        string `json:"record_id"`
		RecordType      string `json:"record_type"`
		AccountID       string `json:"account_id"`
		DeviceID        string `json:"device_id"`
		ProtocolVersion uint32 `json:"protocol_version"`
		OperationID     string `json:"operation_id"`
		EntityVersion   uint32 `json:"entity_version"`
		Ciphertext      []byte `json:"ciphertext"`
	} `json:"envelope"`
}

type head struct {
	version uint32
	idemKey string
}

type storedOp struct {
	seq int64
	raw json.RawMessage
}

type account struct {
	heads   map[string]head
	seen    map[string]struct{} // operation_id
	ops     []storedOp
	nextSeq int64
}

func newAccount() *account {
	return &account{heads: map[string]head{}, seen: map[string]struct{}{}}
}

type server struct {
	mu       sync.Mutex
	accounts map[string]*account
}

func newServer() *server { return &server{accounts: map[string]*account{}} }

func (s *server) account(id string) *account {
	a, ok := s.accounts[id]
	if !ok {
		a = newAccount()
		s.accounts[id] = a
	}
	return a
}

// session identifies the authenticated caller. Auth is stubbed for the mock.
type session struct {
	accountID string
	deviceID  string
}

func authenticate(r *http.Request) (session, bool) {
	token := bearer(r.Header.Get("Authorization"))
	device := r.Header.Get(deviceHeader)
	if token == "" || device == "" {
		return session{}, false
	}
	if v, err := strconv.Atoi(r.Header.Get(protocolHeader)); err != nil || v != protocolVersion {
		return session{}, false
	}
	return session{accountID: deriveAccount(token), deviceID: device}, true
}

func (s *server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, ok := authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{
		"account_id": sess.accountID,
		"device_id":  sess.deviceID,
		"expires_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	log.Printf("session account=%s device=%s", sess.accountID, sess.deviceID)
}

func (s *server) handlePush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, ok := authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Operations []json.RawMessage `json:"operations"`
	}
	if err := decodeBody(r, &req); err != nil || len(req.Operations) > maxPullOperations {
		http.Error(w, "invalid request", http.StatusUnprocessableEntity)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	acct := s.account(sess.accountID)

	sent := []string{}
	conflicts := []map[string]any{}
	for _, raw := range req.Operations {
		var op opView
		if err := json.Unmarshal(raw, &op); err != nil {
			http.Error(w, "invalid operation", http.StatusUnprocessableEntity)
			return
		}
		ev := op.Envelope
		if ev.AccountID != sess.accountID || ev.DeviceID != sess.deviceID ||
			ev.ProtocolVersion != protocolVersion || op.IdempotencyKey == "" ||
			ev.RecordID == "" || ev.OperationID == "" || ev.EntityVersion == 0 ||
			len(ev.Ciphertext) == 0 || len(ev.Ciphertext) > maxOperationCiphertextBytes {
			http.Error(w, "operation does not match session", http.StatusUnprocessableEntity)
			return
		}

		if _, replayed := acct.seen[ev.OperationID]; replayed {
			sent = append(sent, op.IdempotencyKey) // idempotent: already applied
			continue
		}

		current, exists := acct.heads[ev.RecordID]
		expected := uint32(1)
		if exists {
			expected = current.version + 1
		}
		switch {
		case ev.EntityVersion == expected:
			acct.nextSeq++
			acct.ops = append(acct.ops, storedOp{seq: acct.nextSeq, raw: append(json.RawMessage(nil), raw...)})
			acct.heads[ev.RecordID] = head{version: ev.EntityVersion, idemKey: op.IdempotencyKey}
			acct.seen[ev.OperationID] = struct{}{}
			sent = append(sent, op.IdempotencyKey)
		case exists && ev.EntityVersion <= current.version:
			remote := current.idemKey
			if remote == op.IdempotencyKey {
				remote = remote + ":head"
			}
			conflicts = append(conflicts, map[string]any{
				"id":                newID(),
				"record_id":         ev.RecordID,
				"record_type":       ev.RecordType,
				"local_change_key":  op.IdempotencyKey,
				"remote_change_key": remote,
				"detected_at":       time.Now().UTC().Format(time.RFC3339),
			})
		default:
			// Fresh entity with version != 1, or a version gap: malformed for a
			// correct client.
			http.Error(w, "version gap", http.StatusUnprocessableEntity)
			return
		}
	}

	writeJSON(w, map[string]any{"sent_idempotency_keys": sent, "conflicts": conflicts})
	log.Printf("push account=%s sent=%d conflicts=%d", sess.accountID, len(sent), len(conflicts))
}

func (s *server) handlePull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, ok := authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 || limit > maxPullOperations {
		http.Error(w, "invalid limit", http.StatusUnprocessableEntity)
		return
	}
	cursor := int64(0)
	if token := r.URL.Query().Get("cursor"); token != "" {
		if cursor, err = strconv.ParseInt(token, 10, 64); err != nil {
			http.Error(w, "invalid cursor", http.StatusUnprocessableEntity)
			return
		}
	}

	s.mu.Lock()
	acct := s.account(sess.accountID)
	ops := []json.RawMessage{}
	next := cursor
	for _, stored := range acct.ops {
		if stored.seq <= cursor {
			continue
		}
		if len(ops) >= limit {
			break
		}
		ops = append(ops, stored.raw)
		next = stored.seq
	}
	s.mu.Unlock()

	writeJSON(w, map[string]any{
		"account_id":       sess.accountID,
		"device_id":        sess.deviceID,
		"protocol_version": protocolVersion,
		"operations":       ops,
		"next_cursor":      strconv.FormatInt(next, 10),
	})
	log.Printf("pull account=%s cursor=%d -> %d ops", sess.accountID, cursor, len(ops))
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "listen address (loopback recommended)")
	flag.Parse()

	s := newServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sync/session", s.handleSession)
	mux.HandleFunc("/v1/sync/push", s.handlePush)
	mux.HandleFunc("/v1/sync/pull", s.handlePull)

	log.Printf("mock sync server (in-memory, zero-knowledge) listening on http://%s", *addr)
	server := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func bearer(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && header[:len(prefix)] == prefix {
		return header[len(prefix):]
	}
	return ""
}

// deriveAccount maps a token to a stable, valid account identifier so the same
// user (token) shares one account across devices.
func deriveAccount(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "acct-" + hex.EncodeToString(sum[:8])
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeBody(r *http.Request, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxHTTPRequestBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxHTTPRequestBytes {
		return errors.New("invalid body")
	}
	return json.Unmarshal(body, dst)
}
