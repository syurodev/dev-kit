package forge

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// ---- comments (GraphQL: REST has no resolved/outdated) ---------------

const commentsQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){
comments(first:100){nodes{databaseId author{login} body createdAt url}}
reviewThreads(first:100){nodes{id isResolved isOutdated path line originalLine
comments(first:50){nodes{databaseId author{login} body createdAt url replyTo{databaseId}}}}}}}}`

const threadsQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){
reviewThreads(first:100){nodes{isResolved}}}}}`

type gqlAuthor struct {
	Login string `json:"login"`
}

type gqlComment struct {
	DatabaseID int64      `json:"databaseId"`
	Author     *gqlAuthor `json:"author"`
	Body       string     `json:"body"`
	CreatedAt  time.Time  `json:"createdAt"`
	URL        string     `json:"url"`
	ReplyTo    *struct {
		DatabaseID int64 `json:"databaseId"`
	} `json:"replyTo"`
}

func (g *GitHub) graphql(ctx context.Context, c Credential, query string, vars map[string]any, out any) error {
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"errors"`
	}
	if err := g.send(ctx, c, "POST", "/graphql", map[string]any{"query": query, "variables": vars}, &envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		kind := ErrUnavailable
		switch envelope.Errors[0].Type {
		case "NOT_FOUND":
			kind = ErrNotFound
		case "FORBIDDEN", "INSUFFICIENT_SCOPES":
			kind = ErrForbidden
		case "RATE_LIMITED":
			kind = ErrRateLimited
		}
		return &Error{Kind: kind, Message: clip(envelope.Errors[0].Message, 300)}
	}
	return decode(envelope.Data, out)
}

func (g *GitHub) graphVars(repo Repo, number int) map[string]any {
	owner, name, _ := strings.Cut(repo.Path, "/")
	return map[string]any{"owner": owner, "name": name, "number": number}
}

func (g *GitHub) Comments(ctx context.Context, c Credential, repo Repo, number int) ([]Comment, error) {
	if err := validRepo(repo); err != nil {
		return nil, err
	}
	if _, err := numberPath(number); err != nil {
		return nil, err
	}
	var data struct {
		Repository struct {
			PullRequest *struct {
				Comments struct{ Nodes []gqlComment } `json:"comments"`
				Threads  struct {
					Nodes []struct {
						ID           string                       `json:"id"`
						IsResolved   bool                         `json:"isResolved"`
						IsOutdated   bool                         `json:"isOutdated"`
						Path         string                       `json:"path"`
						Line         int                          `json:"line"`
						OriginalLine int                          `json:"originalLine"`
						Comments     struct{ Nodes []gqlComment } `json:"comments"`
					}
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := g.graphql(ctx, c, commentsQuery, g.graphVars(repo, number), &data); err != nil {
		return nil, err
	}
	pr := data.Repository.PullRequest
	if pr == nil {
		return nil, &Error{Kind: ErrNotFound, Message: "no such pull request"}
	}
	var out []Comment
	for _, n := range pr.Comments.Nodes {
		out = append(out, Comment{
			ID: strconv.FormatInt(n.DatabaseID, 10), Kind: CommentConversation, Author: authorOf(n.Author),
			Body: n.Body, URL: n.URL, CreatedAt: n.CreatedAt,
		})
	}
	for _, thread := range pr.Threads.Nodes {
		line := thread.Line
		if line == 0 {
			line = thread.OriginalLine
		}
		for _, n := range thread.Comments.Nodes {
			cm := Comment{
				ID: strconv.FormatInt(n.DatabaseID, 10), Kind: CommentReview, Author: authorOf(n.Author),
				Body: n.Body, Path: thread.Path, Line: line, ThreadID: thread.ID,
				Resolved: thread.IsResolved, Outdated: thread.IsOutdated, URL: n.URL, CreatedAt: n.CreatedAt,
			}
			if n.ReplyTo != nil {
				cm.ReplyTo = strconv.FormatInt(n.ReplyTo.DatabaseID, 10)
			}
			out = append(out, cm)
		}
	}
	return out, nil
}

func authorOf(a *gqlAuthor) string {
	if a == nil {
		return "ghost"
	}
	return a.Login
}

func (g *GitHub) UnresolvedThreads(ctx context.Context, c Credential, repo Repo, number int) (int, error) {
	if err := validRepo(repo); err != nil {
		return 0, err
	}
	if _, err := numberPath(number); err != nil {
		return 0, err
	}
	var data struct {
		Repository struct {
			PullRequest *struct {
				Threads struct {
					Nodes []struct {
						IsResolved bool `json:"isResolved"`
					}
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := g.graphql(ctx, c, threadsQuery, g.graphVars(repo, number), &data); err != nil {
		return 0, err
	}
	if data.Repository.PullRequest == nil {
		return 0, &Error{Kind: ErrNotFound, Message: "no such pull request"}
	}
	open := 0
	for _, t := range data.Repository.PullRequest.Threads.Nodes {
		if !t.IsResolved {
			open++
		}
	}
	return open, nil
}

// ---- writes ------------------------------------------------------------

func (g *GitHub) Create(ctx context.Context, c Credential, repo Repo, in CreateInput) (ChangeRequest, error) {
	if err := validRepo(repo); err != nil {
		return ChangeRequest{}, err
	}
	if strings.TrimSpace(in.Title) == "" || in.Head == "" || in.Base == "" {
		return ChangeRequest{}, &Error{Kind: ErrInvalid, Message: "a pull request needs a title, a head branch and a base branch"}
	}
	var pull ghPull
	err := g.send(ctx, c, "POST", repoPath(repo)+"/pulls", map[string]any{
		"title": in.Title, "body": in.Body, "head": in.Head, "base": in.Base, "draft": in.Draft,
	}, &pull)
	if err != nil {
		return ChangeRequest{}, err
	}
	return g.changeRequest(repo, pull), nil
}

func (g *GitHub) Update(ctx context.Context, c Credential, repo Repo, number int, in UpdateInput) (ChangeRequest, error) {
	if err := validRepo(repo); err != nil {
		return ChangeRequest{}, err
	}
	n, err := numberPath(number)
	if err != nil {
		return ChangeRequest{}, err
	}
	payload := map[string]any{}
	if in.Title != nil {
		if strings.TrimSpace(*in.Title) == "" {
			return ChangeRequest{}, &Error{Kind: ErrInvalid, Message: "the title cannot be empty"}
		}
		payload["title"] = *in.Title
	}
	if in.Body != nil {
		payload["body"] = *in.Body
	}
	if len(payload) == 0 {
		return ChangeRequest{}, &Error{Kind: ErrInvalid, Message: "nothing to change"}
	}
	var pull ghPull
	if err := g.send(ctx, c, "PATCH", repoPath(repo)+"/pulls/"+n, payload, &pull); err != nil {
		return ChangeRequest{}, err
	}
	return g.changeRequest(repo, pull), nil
}

type ghComment struct {
	ID        int64                  `json:"id"`
	Body      string                 `json:"body"`
	HTMLURL   string                 `json:"html_url"`
	CreatedAt time.Time              `json:"created_at"`
	User      struct{ Login string } `json:"user"`
	Path      string                 `json:"path"`
	Line      int                    `json:"line"`
	InReplyTo int64                  `json:"in_reply_to_id"`
}

func (g *GitHub) Comment(ctx context.Context, c Credential, repo Repo, number int, body string) (Comment, error) {
	if err := validRepo(repo); err != nil {
		return Comment{}, err
	}
	n, err := numberPath(number)
	if err != nil {
		return Comment{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Comment{}, &Error{Kind: ErrInvalid, Message: "the comment is empty"}
	}
	var out ghComment
	if err := g.send(ctx, c, "POST", repoPath(repo)+"/issues/"+n+"/comments", map[string]string{"body": body}, &out); err != nil {
		return Comment{}, err
	}
	return Comment{ID: strconv.FormatInt(out.ID, 10), Kind: CommentConversation, Author: out.User.Login, Body: out.Body, URL: out.HTMLURL, CreatedAt: out.CreatedAt}, nil
}

func (g *GitHub) Reply(ctx context.Context, c Credential, repo Repo, number int, commentID, body string) (Comment, error) {
	if err := validRepo(repo); err != nil {
		return Comment{}, err
	}
	n, err := numberPath(number)
	if err != nil {
		return Comment{}, err
	}
	if _, err := strconv.ParseInt(commentID, 10, 64); err != nil {
		return Comment{}, &Error{Kind: ErrInvalid, Message: "not a review comment id: " + commentID}
	}
	if strings.TrimSpace(body) == "" {
		return Comment{}, &Error{Kind: ErrInvalid, Message: "the reply is empty"}
	}
	var out ghComment
	if err := g.send(ctx, c, "POST", repoPath(repo)+"/pulls/"+n+"/comments/"+commentID+"/replies", map[string]string{"body": body}, &out); err != nil {
		return Comment{}, err
	}
	cm := Comment{ID: strconv.FormatInt(out.ID, 10), Kind: CommentReview, Author: out.User.Login, Body: out.Body,
		Path: out.Path, Line: out.Line, URL: out.HTMLURL, CreatedAt: out.CreatedAt}
	if out.InReplyTo != 0 {
		cm.ReplyTo = strconv.FormatInt(out.InReplyTo, 10)
	}
	return cm, nil
}

func (g *GitHub) Merge(ctx context.Context, c Credential, repo Repo, number int, in MergeInput) (MergeResult, error) {
	if err := validRepo(repo); err != nil {
		return MergeResult{}, err
	}
	n, err := numberPath(number)
	if err != nil {
		return MergeResult{}, err
	}
	if !in.Method.Valid() {
		return MergeResult{}, &Error{Kind: ErrInvalid, Message: "merge method must be merge, squash or rebase"}
	}
	payload := map[string]any{"merge_method": string(in.Method)}
	if in.Title != "" {
		payload["commit_title"] = in.Title
	}
	if in.Body != "" {
		payload["commit_message"] = in.Body
	}
	if in.SHA != "" {
		payload["sha"] = in.SHA
	}
	var out struct {
		Merged  bool   `json:"merged"`
		SHA     string `json:"sha"`
		Message string `json:"message"`
	}
	if err := g.send(ctx, c, "PUT", repoPath(repo)+"/pulls/"+n+"/merge", payload, &out); err != nil {
		return MergeResult{}, err
	}
	return MergeResult{Merged: out.Merged, SHA: out.SHA, Message: out.Message}, nil
}

func (g *GitHub) Close(ctx context.Context, c Credential, repo Repo, number int) (ChangeRequest, error) {
	if err := validRepo(repo); err != nil {
		return ChangeRequest{}, err
	}
	n, err := numberPath(number)
	if err != nil {
		return ChangeRequest{}, err
	}
	var pull ghPull
	if err := g.send(ctx, c, "PATCH", repoPath(repo)+"/pulls/"+n, map[string]string{"state": "closed"}, &pull); err != nil {
		return ChangeRequest{}, err
	}
	return g.changeRequest(repo, pull), nil
}

func (g *GitHub) DeleteBranch(ctx context.Context, c Credential, repo Repo, name string) error {
	if err := validRepo(repo); err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return &Error{Kind: ErrInvalid, Message: "no branch named"}
	}
	return g.send(ctx, c, "DELETE", repoPath(repo)+"/git/refs/heads/"+escapeRef(name), nil, nil)
}

var _ Provider = (*GitHub)(nil)
