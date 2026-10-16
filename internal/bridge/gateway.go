package bridge

import (
	"context"
	"errors"
	"strings"

	"devkit/internal/apperrors"
	"devkit/internal/capabilities"
	"devkit/internal/identity"
)

const invalidOpaqueID = "invalid"

// call runs a capability-gated handler and maps errors through publicError.
// Bridge UI methods reach it directly; MCP tool handlers reach it through
// NewCaller, so both go through the same gate.
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
		return nil, publicErrorFor(ctx, subject, err)
	}
	return result, nil
}

// publicErrorFor is publicError, except that a caller that is not the window
// is told what the gateway found missing. The window's contract stays "Permission
// denied."; an agent that is only told that cannot tell a person saying no from
// a grant DevKit forgot to install, and reports the first when it is the second.
func publicErrorFor(ctx context.Context, subject string, err error) *apperrors.AppError {
	mapped := publicError(subject, err)
	if caller, _ := identity.FromContext(ctx); caller.Kind == identity.KindUI || !errors.Is(err, capabilities.ErrPermissionDenied) {
		return mapped
	}
	_, detail, ok := strings.Cut(err.Error(), capabilities.ErrPermissionDenied.Error()+": ")
	if detail = strings.TrimSpace(detail); !ok || detail == "" {
		return mapped
	}
	return apperrors.New(codePermissionDenied, "Permission denied: "+detail+".", false)
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
