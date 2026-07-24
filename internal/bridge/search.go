package bridge

import (
	"context"
	"encoding/json"
	"time"
)

// VaultSearch returns cross-type items whose plaintext metadata matches query.
// It runs on metadata only, so it works while the vault is locked, and never
// returns secret payloads.
func (s *AppService) VaultSearch(query string) ([]VaultSearchResult, error) {
	result, err := s.call(context.Background(), "builtin.search", "vault:search", "item:search", map[string]any{
		"action": "search",
	}, func(context.Context) (any, error) {
		records, err := s.container.Items.Search(query)
		if err != nil {
			return nil, err
		}
		out := make([]VaultSearchResult, 0, len(records))
		for _, record := range records {
			out = append(out, VaultSearchResult{
				ID:        record.ID,
				Type:      record.Type,
				Label:     labelFromMetadata(record.Metadata),
				UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
			})
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]VaultSearchResult), nil
}

// labelFromMetadata pulls a display label from generic metadata: "title" for
// notes, "name" for profiles. Empty when neither is present.
func labelFromMetadata(raw json.RawMessage) string {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	for _, key := range []string{"title", "name"} {
		if v, ok := fields[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
