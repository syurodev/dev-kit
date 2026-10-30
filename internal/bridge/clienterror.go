package bridge

import "context"

// ReportClientError is the window's one-way channel to DevKit's log: the root
// error screen and the window's own error events send what crashed, because the
// production webview has no inspector and the error is gone after Reload.
//
// It is fire and forget. It returns nothing, so a failing report can neither
// break the window nor tell it anything, and it records the label, message and
// stack the window chose to send, all bounded and rate limited in
// internal/clienterror. Window only: an agent or a terminal on the shared socket
// is dropped without a line. The audit record carries the action, never the
// text; no MCP tool reaches this method.
func (s *AppService) ReportClientError(label string, message string, stack string, componentStack string) {
	s.reportClientError(context.Background(), label, message, stack, componentStack)
}

func (s *AppService) reportClientError(ctx context.Context, label, message, stack, componentStack string) {
	_, _ = s.call(ctx, "builtin.diagnostics", "diagnostics:write", "client_error", map[string]any{
		"action": "report_client_error",
	}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		s.container.ClientErrors.Report(label, message, stack, componentStack)
		return nil, nil
	})
}
