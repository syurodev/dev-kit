package bridge

import (
	"context"
	"strings"

	"devkit/internal/capabilities"
	"devkit/internal/identity"
)

const invalidOpaqueID = "invalid"

// Call runs a capability-gated handler and maps errors through publicError.
// Bridge UI methods and MCP tool handlers must both use this entrypoint.
func (s *AppService) Call(
	ctx context.Context,
	subject string,
	capability string,
	scope string,
	metadata map[string]any,
	handler capabilities.Handler,
) (any, error) {
	return s.call(ctx, subject, capability, scope, metadata, handler)
}

func (s *AppService) call(
	ctx context.Context,
	subject string,
	capability string,
	scope string,
	metadata map[string]any,
	handler capabilities.Handler,
) (any, error) {
	ctx = callerContext(ctx)
	result, err := s.container.Capabilities.Call(ctx, capabilities.CallRequest{
		SubjectID:  subject,
		Capability: capability,
		Scope:      scope,
		Metadata:   metadata,
		Handler:    handler,
	})
	if err != nil {
		return nil, publicError(subject, err)
	}
	return result, nil
}

// callerContext defaults an unattributed call to the UI: bridge methods are
// invoked by the desktop frontend, while MCP tool handlers arrive with an
// agent / user-terminal caller already stamped by internal/mcp and keep it.
func callerContext(ctx context.Context) context.Context {
	if _, ok := identity.FromContext(ctx); ok {
		return ctx
	}
	return identity.WithCaller(ctx, identity.UI())
}

func recordID(value string) string {
	if len(value) != 24 {
		return invalidOpaqueID
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return invalidOpaqueID
		}
	}
	return value
}

func commandID(value string) string {
	if value == "" || len(value) > 128 {
		return invalidOpaqueID
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && !strings.ContainsRune("._-", char) {
			return invalidOpaqueID
		}
	}
	return value
}
