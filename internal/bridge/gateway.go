package bridge

import (
	"context"
	"strings"

	"changeme/internal/capabilities"
)

const invalidOpaqueID = "invalid"

func (s *AppService) call(
	ctx context.Context,
	subject string,
	capability string,
	scope string,
	metadata map[string]any,
	handler capabilities.Handler,
) (any, error) {
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
