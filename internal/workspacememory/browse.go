package workspacememory

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type BrowseInput struct {
	Query   string `json:"query"`
	Kind    string `json:"kind"`
	Cursor  string `json:"cursor"`
	Deleted bool   `json:"deleted"`
	Limit   int    `json:"limit"`
}
type BrowsePage struct {
	Entries    []Summary `json:"entries"`
	HasMore    bool      `json:"hasMore"`
	NextCursor string    `json:"nextCursor"`
}
type RevisionSummary struct {
	Revision  int64  `json:"revision"`
	Operation string `json:"operation"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	UpdatedAt string `json:"updatedAt"`
	Deleted   bool   `json:"deleted"`
	Source    Source `json:"source"`
}
type HistoryPage struct {
	Entries    []RevisionSummary `json:"entries"`
	HasMore    bool              `json:"hasMore"`
	NextCursor string            `json:"nextCursor"`
}

func validKind(kind string) bool {
	switch kind {
	case "", "decision", "convention", "knowledge", "handoff":
		return true
	}
	return false
}
func (s *Service) Browse(ctx context.Context, ws string, in BrowseInput) (BrowsePage, error) {
	var err error
	in.Limit, err = pageBounds(in.Limit, 0)
	if err != nil || !textValid(in.Query, 256, false) || !validKind(in.Kind) {
		return BrowsePage{}, ErrInvalidInput
	}
	if err = s.workspace(ctx, ws); err != nil {
		return BrowsePage{}, err
	}
	return s.repo.Browse(ctx, ws, in)
}
func (s *Service) HistoryPage(ctx context.Context, ws, id, cursor string, limit int) (HistoryPage, error) {
	limit, err := pageBounds(limit, 0)
	if err != nil {
		return HistoryPage{}, err
	}
	// Verify only the workspace here; the repository checks the scoped entry ID
	// without fetching its full body before returning the small history read model.
	if err = s.workspace(ctx, ws); err != nil {
		return HistoryPage{}, err
	}
	return s.repo.HistoryPage(ctx, ws, id, cursor, limit)
}
func (s *Service) ReadRevision(ctx context.Context, ws, id string, revision int64) (Revision, error) {
	if revision <= 0 {
		return Revision{}, ErrInvalidInput
	}
	if err := s.workspace(ctx, ws); err != nil {
		return Revision{}, err
	}
	return s.repo.GetRevision(ctx, ws, id, revision)
}

// Search is literal word-prefix AND search. Punctuation only returns no hits;
// quotes, stars, operators and column names never become user-authored FTS syntax.
func searchExpression(ws, query string) (string, bool) {
	if strings.TrimSpace(query) == "" {
		return "", false
	}
	terms := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) })
	parts := make([]string, 0, len(terms))
	seen := map[string]bool{}
	for _, term := range terms {
		hasBase := false
		for _, r := range term {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				hasBase = true
				break
			}
		}
		if !hasBase {
			continue
		}
		term = strings.ToLower(term)
		if seen[term] {
			continue
		}
		seen[term] = true
		parts = append(parts, `"`+term+`"*`)
	}
	if len(parts) == 0 {
		return "", true
	}
	// Hex encodes the complete workspace ID into one tokenizer-safe indexed token.
	return `workspace_key:"w` + hex.EncodeToString([]byte(ws)) + `" AND {title summary body}: (` + strings.Join(parts, " AND ") + `)`, false
}

type pageCursor struct {
	Version   int    `json:"v"`
	Binding   string `json:"b"`
	UpdatedAt string `json:"t,omitempty"`
	ID        string `json:"i,omitempty"`
	Revision  int64  `json:"r,omitempty"`
}

func cursorBinding(parts ...string) string {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func browseBinding(ws string, in BrowseInput) string {
	deleted := "active"
	if in.Deleted {
		deleted = "deleted"
	}
	return cursorBinding("browse", ws, in.Query, in.Kind, deleted)
}
func encodeCursor(cursor pageCursor) string {
	cursor.Version = 1
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}
func decodeCursor(text, binding string, history bool) (pageCursor, error) {
	if text == "" {
		return pageCursor{}, nil
	}
	if len(text) > 2048 {
		return pageCursor{}, ErrInvalidInput
	}
	data, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil || !utf8.Valid(data) {
		return pageCursor{}, ErrInvalidInput
	}
	var c pageCursor
	if err = json.Unmarshal(data, &c); err != nil || c.Version != 1 || c.Binding != binding {
		return pageCursor{}, ErrInvalidInput
	}
	if history {
		if c.Revision <= 0 || c.ID != "" || c.UpdatedAt != "" {
			return pageCursor{}, ErrInvalidInput
		}
	} else {
		if c.Revision != 0 || !textValid(c.ID, 256, true) || !textValid(c.UpdatedAt, 64, true) {
			return pageCursor{}, ErrInvalidInput
		}
		parsed, err := time.Parse(time.RFC3339Nano, c.UpdatedAt)
		if err != nil || parsed.UTC().Format("2006-01-02T15:04:05.000000000Z") != c.UpdatedAt {
			return pageCursor{}, ErrInvalidInput
		}
	}
	return c, nil
}
