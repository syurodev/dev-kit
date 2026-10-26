package agenthub

import (
	"context"
	"devkit/internal/usagemetrics"
)

func (s *Service) ToolUsageReport(ctx context.Context, vendor AgentType, q usagemetrics.Query) (usagemetrics.Report, error) {
	if e := requireWindow(ctx); e != nil {
		return usagemetrics.Report{}, e
	}
	if _, ok := s.reg.Get(vendor); !ok {
		return usagemetrics.Report{}, ErrVendorUnknown
	}
	if s.ToolMetrics == nil {
		return usagemetrics.Report{}, usagemetrics.ErrUnavailable
	}
	return s.ToolMetrics.Report(ctx, string(vendor), q)
}
