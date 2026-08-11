package mcp

import (
	"encoding/json"
	"time"

	"devkit/internal/vaultitem"
)

// ItemSearcher is the narrow vault-item search surface used to adapt
// vaultitem.Store (or equivalent) to SearchAPI without importing bridge.
type ItemSearcher interface {
	Search(query string, types ...string) ([]vaultitem.Record, error)
}

type itemsSearchAdapter struct {
	items ItemSearcher
}

// NewSearchAPI adapts a vault-item searcher to SearchAPI (metadata hits only).
func NewSearchAPI(items ItemSearcher) SearchAPI {
	if items == nil {
		return nil
	}
	return itemsSearchAdapter{items: items}
}

func (a itemsSearchAdapter) Search(query string) ([]SearchHit, error) {
	records, err := a.items.Search(query)
	if err != nil {
		return nil, err
	}
	out := make([]SearchHit, 0, len(records))
	for _, record := range records {
		out = append(out, SearchHit{
			ID:        record.ID,
			Type:      record.Type,
			Label:     labelFromMetadata(record.Metadata),
			UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

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

// NewServerFromContainer builds an MCP server from already-resolved Deps.
// main (package main) constructs Deps with AppService as Caller and domain
// services from the app container — mcp must not import app/bridge.
func NewServerFromContainer(d Deps) *Server {
	return NewServer(d)
}
