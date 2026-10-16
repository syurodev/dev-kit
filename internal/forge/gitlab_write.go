package forge

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---- discussions -------------------------------------------------------

type glNote struct {
	ID         int64     `json:"id"`
	Body       string    `json:"body"`
	System     bool      `json:"system"`
	Type       string    `json:"type"`
	Resolvable bool      `json:"resolvable"`
	Resolved   bool      `json:"resolved"`
	CreatedAt  time.Time `json:"created_at"`
	Author     struct {
		Username string `json:"username"`
	} `json:"author"`
	Position *struct {
		NewPath string `json:"new_path"`
		OldPath string `json:"old_path"`
		NewLine int    `json:"new_line"`
		OldLine int    `json:"old_line"`
	} `json:"position"`
}

type glDiscussion struct {
	ID    string   `json:"id"`
	Notes []glNote `json:"notes"`
}

func (g *GitLab) discussions(ctx context.Context, c Credential, repo Repo, n string) ([]glDiscussion, error) {
	var all []glDiscussion
	next := ""
	for page := 0; page < maxPages; page++ {
		var batch []glDiscussion
		var err error
		if page == 0 {
			next, err = g.get(ctx, c, projectPath(repo)+"/merge_requests/"+n+"/discussions", url.Values{"per_page": {"100"}}, &batch)
		} else {
			next, err = g.getURL(ctx, c, next, &batch)
		}
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if next == "" {
			break
		}
	}
	return all, nil
}

// A comment's id on GitLab is "<discussion>:<note>": a reply goes to the
// discussion, and the note alone does not say which it is in. Opaque to a
// caller, who passes back what pr.comments gave.
func glCommentID(discussion string, note int64) string {
	return discussion + ":" + strconv.FormatInt(note, 10)
}

func (g *GitLab) Comments(ctx context.Context, c Credential, repo Repo, number int) ([]Comment, error) {
	if err := validProject(repo); err != nil {
		return nil, err
	}
	n, err := numberPath(number)
	if err != nil {
		return nil, err
	}
	discussions, err := g.discussions(ctx, c, repo, n)
	if err != nil {
		return nil, err
	}
	var out []Comment
	for _, d := range discussions {
		var first string
		for _, note := range d.Notes {
			if note.System {
				continue
			}
			cm := Comment{
				ID: glCommentID(d.ID, note.ID), Kind: CommentConversation, Author: note.Author.Username,
				Body: note.Body, CreatedAt: note.CreatedAt,
			}
			if note.Position != nil || note.Resolvable {
				cm.Kind, cm.ThreadID, cm.Resolved = CommentReview, d.ID, note.Resolved
				if p := note.Position; p != nil {
					cm.Path, cm.Line = p.NewPath, p.NewLine
					if cm.Path == "" {
						cm.Path = p.OldPath
					}
					if cm.Line == 0 {
						cm.Line = p.OldLine
					}
				}
			}
			if first == "" {
				first = cm.ID
			} else {
				cm.ReplyTo = first
			}
			out = append(out, cm)
		}
	}
	return out, nil
}

func (g *GitLab) UnresolvedThreads(ctx context.Context, c Credential, repo Repo, number int) (int, error) {
	if err := validProject(repo); err != nil {
		return 0, err
	}
	n, err := numberPath(number)
	if err != nil {
		return 0, err
	}
	discussions, err := g.discussions(ctx, c, repo, n)
	if err != nil {
		return 0, err
	}
	open := 0
	for _, d := range discussions {
		for _, note := range d.Notes {
			if note.Resolvable && !note.Resolved {
				open++
				break
			}
		}
	}
	return open, nil
}

// ---- writes ------------------------------------------------------------

func (g *GitLab) Create(ctx context.Context, c Credential, repo Repo, in CreateInput) (ChangeRequest, error) {
	if err := validProject(repo); err != nil {
		return ChangeRequest{}, err
	}
	if strings.TrimSpace(in.Title) == "" || in.Head == "" || in.Base == "" {
		return ChangeRequest{}, &Error{Kind: ErrInvalid, Message: "a merge request needs a title, a source branch and a target branch"}
	}
	if strings.Contains(in.Head, ":") {
		return ChangeRequest{}, &Error{Kind: ErrInvalid, Message: "opening a merge request from a fork is not supported yet"}
	}
	title := in.Title
	if in.Draft && !strings.HasPrefix(strings.ToLower(title), "draft:") {
		title = "Draft: " + title
	}
	var m glMR
	err := g.send(ctx, c, "POST", projectPath(repo)+"/merge_requests", map[string]any{
		"title": title, "description": in.Body, "source_branch": in.Head, "target_branch": in.Base,
	}, &m)
	if err != nil {
		return ChangeRequest{}, err
	}
	return g.changeRequest(repo, m), nil
}

func (g *GitLab) Update(ctx context.Context, c Credential, repo Repo, number int, in UpdateInput) (ChangeRequest, error) {
	if err := validProject(repo); err != nil {
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
		payload["description"] = *in.Body
	}
	if len(payload) == 0 {
		return ChangeRequest{}, &Error{Kind: ErrInvalid, Message: "nothing to change"}
	}
	var m glMR
	if err := g.send(ctx, c, "PUT", projectPath(repo)+"/merge_requests/"+n, payload, &m); err != nil {
		return ChangeRequest{}, err
	}
	return g.changeRequest(repo, m), nil
}

func (g *GitLab) noteComment(n glNote, kind CommentKind, thread string) Comment {
	return Comment{ID: strconv.FormatInt(n.ID, 10), Kind: kind, Author: n.Author.Username, Body: n.Body, ThreadID: thread, CreatedAt: n.CreatedAt}
}

func (g *GitLab) Comment(ctx context.Context, c Credential, repo Repo, number int, body string) (Comment, error) {
	if err := validProject(repo); err != nil {
		return Comment{}, err
	}
	n, err := numberPath(number)
	if err != nil {
		return Comment{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Comment{}, &Error{Kind: ErrInvalid, Message: "the comment is empty"}
	}
	var note glNote
	if err := g.send(ctx, c, "POST", projectPath(repo)+"/merge_requests/"+n+"/notes", map[string]string{"body": body}, &note); err != nil {
		return Comment{}, err
	}
	return g.noteComment(note, CommentConversation, ""), nil
}

var hexID = regexp.MustCompile(`^[0-9a-f]{8,64}$`)

func (g *GitLab) Reply(ctx context.Context, c Credential, repo Repo, number int, commentID, body string) (Comment, error) {
	if err := validProject(repo); err != nil {
		return Comment{}, err
	}
	n, err := numberPath(number)
	if err != nil {
		return Comment{}, err
	}
	discussion, _, _ := strings.Cut(commentID, ":")
	if !hexID.MatchString(discussion) {
		return Comment{}, &Error{Kind: ErrInvalid, Message: "not a comment id from pr.comments: " + commentID}
	}
	if strings.TrimSpace(body) == "" {
		return Comment{}, &Error{Kind: ErrInvalid, Message: "the reply is empty"}
	}
	var note glNote
	if err := g.send(ctx, c, "POST", projectPath(repo)+"/merge_requests/"+n+"/discussions/"+discussion+"/notes", map[string]string{"body": body}, &note); err != nil {
		return Comment{}, err
	}
	cm := g.noteComment(note, CommentReview, discussion)
	cm.ID = glCommentID(discussion, note.ID)
	cm.ReplyTo = commentID
	return cm, nil
}

// Merge maps the three methods onto what GitLab can do in one call: a merge
// commit, or a squash. A rebase is a separate, asynchronous operation and the
// project's own merge method decides whether a plain merge may fast-forward,
// so it is refused rather than approximated.
func (g *GitLab) Merge(ctx context.Context, c Credential, repo Repo, number int, in MergeInput) (MergeResult, error) {
	if err := validProject(repo); err != nil {
		return MergeResult{}, err
	}
	n, err := numberPath(number)
	if err != nil {
		return MergeResult{}, err
	}
	switch in.Method {
	case MergeMethodMerge, MergeMethodSquash:
	case MergeMethodRebase:
		return MergeResult{}, &Error{Kind: ErrInvalid, Message: "GitLab merges with a merge commit or a squash here; choose merge or squash (the project's own settings decide fast-forward)"}
	default:
		return MergeResult{}, &Error{Kind: ErrInvalid, Message: "merge method must be merge, squash or rebase"}
	}
	payload := map[string]any{}
	if in.Method == MergeMethodSquash {
		payload["squash"] = true
	}
	if in.Title != "" || in.Body != "" {
		message := strings.TrimSpace(in.Title + "\n\n" + in.Body)
		if in.Method == MergeMethodSquash {
			payload["squash_commit_message"] = message
		} else {
			payload["merge_commit_message"] = message
		}
	}
	if in.SHA != "" {
		payload["sha"] = in.SHA
	}
	var m glMR
	if err := g.send(ctx, c, "PUT", projectPath(repo)+"/merge_requests/"+n+"/merge", payload, &m); err != nil {
		return MergeResult{}, err
	}
	sha := m.MergeCommitSHA
	if sha == "" {
		sha = m.SquashCommitSHA
	}
	return MergeResult{Merged: m.State == "merged", SHA: sha, Message: "state: " + m.State}, nil
}

func (g *GitLab) Close(ctx context.Context, c Credential, repo Repo, number int) (ChangeRequest, error) {
	if err := validProject(repo); err != nil {
		return ChangeRequest{}, err
	}
	n, err := numberPath(number)
	if err != nil {
		return ChangeRequest{}, err
	}
	var m glMR
	if err := g.send(ctx, c, "PUT", projectPath(repo)+"/merge_requests/"+n, map[string]string{"state_event": "close"}, &m); err != nil {
		return ChangeRequest{}, err
	}
	return g.changeRequest(repo, m), nil
}

func (g *GitLab) DeleteBranch(ctx context.Context, c Credential, repo Repo, name string) error {
	if err := validProject(repo); err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return &Error{Kind: ErrInvalid, Message: "no branch named"}
	}
	return g.send(ctx, c, "DELETE", projectPath(repo)+"/repository/branches/"+url.PathEscape(name), nil, nil)
}

var _ Provider = (*GitLab)(nil)
