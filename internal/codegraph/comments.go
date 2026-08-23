// internal/codegraph/comments.go
package codegraph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func (s *sqliteStore) AddComment(ctx context.Context, c PlanComment) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO plan_node_comments (plan_node_id, comment_id, author, body, created_at, session_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		c.PlanNodeID, c.CommentID, c.Author, c.Body, c.CreatedAt, c.SessionID)
	if err != nil {
		return fmt.Errorf("codegraph: AddComment: %w", err)
	}
	return nil
}

// ListComments returns one node's thread, oldest first. The comment id breaks
// ties, so two comments written in the same second keep a stable order.
func (s *sqliteStore) ListComments(ctx context.Context, planNodeID string) ([]PlanComment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT plan_node_id, comment_id, author, body, created_at, session_id
		   FROM plan_node_comments WHERE plan_node_id = ? ORDER BY created_at, comment_id`,
		planNodeID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListComments: %w", err)
	}
	defer rows.Close()

	var out []PlanComment
	for rows.Next() {
		var c PlanComment
		if err := rows.Scan(&c.PlanNodeID, &c.CommentID, &c.Author, &c.Body, &c.CreatedAt, &c.SessionID); err != nil {
			return nil, fmt.Errorf("codegraph: scan comment: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountComments returns thread length per plan node, for the canvas badge.
// One query rather than one per node — the canvas draws every node at once.
func (s *sqliteStore) CountComments(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT plan_node_id, COUNT(*) FROM plan_node_comments GROUP BY plan_node_id`)
	if err != nil {
		return nil, fmt.Errorf("codegraph: CountComments: %w", err)
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("codegraph: scan comment count: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}

// CommentPlanNode records a human comment on a plan node.
//
// The comment is agent-readable by design: it comes back with the plan on the
// next getPlannedFlow, so it steers implementation rather than being a private
// note (codegraph.mdx § "Comment agent đọc được").
//
// TODO(agent session delivery): this only stores.
//
// codegraph.mdx § "Gửi vào đúng phiên đang chạy, không phải hàng đợi" requires
// that when an AgentSession is implementing this plan, the comment goes into
// THAT session as a scoped session/prompt — the same conversation as the chat
// panel, not a side channel — and that the UI distinguishes four states,
// because awaiting_permission is the case where the user presses send and
// nothing reaches the agent:
//
//	idle, running       -> delivered immediately as a prompt
//	awaiting_permission -> queued behind the permission request; the user has
//	                       to resolve it in Agent Hub first
//	error               -> not delivered; waits for the next getPlannedFlow
//	done, or no session -> stored; handed over on the next getPlannedFlow
//
// Doing this needs a link from a plan to the AgentSession implementing it, and
// nothing records that today. That link is the real first step, not the prompt
// call — which is why the UI currently states the queued case unconditionally
// rather than showing a status it cannot honour.
func (s *Service) CommentPlanNode(ctx context.Context, planNodeID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("codegraph: comment body is empty")
	}
	store, err := s.activeStore()
	if err != nil {
		return err
	}
	return store.AddComment(ctx, PlanComment{
		PlanNodeID: planNodeID,
		CommentID:  newCommentID(),
		Author:     "user",
		Body:       body,
		CreatedAt:  time.Now().Unix(),
	})
}

// ReplyPlanNode records an agent reply. This is an MCP write, and it stays
// inside the plan-metadata boundary: text only, no code touched, no effect
// until a person reads it.
func (s *Service) ReplyPlanNode(ctx context.Context, planNodeID, body, sessionID string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("codegraph: reply body is empty")
	}
	store, err := s.activeStore()
	if err != nil {
		return err
	}
	return store.AddComment(ctx, PlanComment{
		PlanNodeID: planNodeID,
		CommentID:  newCommentID(),
		Author:     "agent",
		Body:       body,
		CreatedAt:  time.Now().Unix(),
		SessionID:  sessionID,
	})
}

func (s *Service) ListPlanNodeComments(ctx context.Context, planNodeID string) ([]PlanComment, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	return store.ListComments(ctx, planNodeID)
}

func (s *Service) CountPlanNodeComments(ctx context.Context) (map[string]int, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	return store.CountComments(ctx)
}

func newCommentID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is unrecoverable, and a fallback scheme would
		// risk two comments sharing an id, which corrupts a thread's identity.
		panic(fmt.Sprintf("codegraph: rand: %v", err))
	}
	return hex.EncodeToString(b[:])
}
