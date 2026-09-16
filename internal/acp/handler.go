package acp

import (
	"context"
	"encoding/json"
)

// ClientHandler handles agent→client JSON-RPC requests and notifications.
type ClientHandler interface {
	RequestPermission(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
	SessionUpdate(ctx context.Context, params json.RawMessage) error
	// UnknownMethod handles unsupported agent→client methods (e.g. fs/*).
	// On error, Conn writes a JSON-RPC error response; on success, result is written.
	UnknownMethod(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
}

// ElicitationHandler is implemented by a ClientHandler that can put a question
// in front of a person (elicitation/create).
//
// Optional, like SessionLoader on the agenthub side: whether a client can ask
// is a fact about that client, not about the protocol. A handler without it
// gets elicitation/create through UnknownMethod, which refuses — the honest
// answer from a client that never declared elicitation in its capabilities.
type ElicitationHandler interface {
	// Elicit may block for as long as the person takes; there is no deadline on
	// a question. Conn runs it off the read loop, and cancels ctx when the agent
	// sends $/cancel_request for it or the connection goes away. On
	// cancellation it should still return a result — {"action":"cancel"} — which
	// Conn writes: the agent keeps a request it cancelled pending until a
	// response arrives.
	Elicit(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
}
