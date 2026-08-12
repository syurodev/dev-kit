package acp

import (
	"context"
	"encoding/json"
)

// ClientHandler handles agent→client JSON-RPC methods.
type ClientHandler interface {
	RequestPermission(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
	SessionUpdate(ctx context.Context, params json.RawMessage) error
	// UnknownMethod handles unsupported agent→client methods (e.g. fs/*).
	// On error, Conn writes a JSON-RPC error response; on success, result is written.
	UnknownMethod(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
}
