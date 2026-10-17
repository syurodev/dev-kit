package forge

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- comments (REST discussions) -----------------------------------------

type glNote struct {
	ID         int       `json:"id"`
	Type       string    `json:"type"`
	Body       string    `json:"body"`
	System     bool      `json:"system"`
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
	ID             string   `json:"id"`
	IndividualNote bool     `json:"individual_note"`
	Notes          []glNote `json:"notes"`
}

// discussions lists a merge request's discussions, bounded to maxPages of 100.
func (g *GitLab) discussions(ctx context.Context, c Credential, repo Repo, iid string) ([]glDiscussion, error) {
	var all []glDiscussion
	next := ""
	for page := 0; page < maxPages; page++ {
		var batch []glDiscussion
		var err error
		if page == 0 {
			next, err = g.get(ctx, c, repo, mrPath(repo, iid)+"/discussions", url.Values{"per_page": {"100"}}, &batch)
		} else {
			next, err = g.getURL(ctx, c, repo, next, &batch)
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

// threadOpen: a discussion is open while any of its resolvable notes is.
func threadOpen(d glDiscussion) bool {
	for _, n := range d.Notes {
		if !n.System && n.Resolvable && !n.Resolved {
			return true
		}
	}
	return false
}

// threadResolvable: the discussion is one that can be resolved at all.
func threadResolvable(d glDiscussion) bool {
	for _, n := range d.Notes {
		if !n.System && n.Resolvable {
			return true
		}
	}
	return false
}

func (g *GitLab) Comments(ctx context.Context, c Credential, repo Repo, number int) ([]Comment, error) {
	if err := g.validRepo(repo); err != nil {
		return nil, err
	}
	iid, err := glIID(number)
	if err != nil {
		return nil, err
	}
	ds, err := g.discussions(ctx, c, repo, iid)
	if err != nil {
		return nil, err
	}
	base := "https://" + normalizeHost(repo.Host) + "/" + repo.Path + "/-/merge_requests/" + iid + "#note_"
	var out []Comment
	for _, d := range ds {
		resolvable := threadResolvable(d)
		resolved := resolvable && !threadOpen(d)
		var first string
		for _, n := range d.Notes {
			if n.System {
				continue
			}
			cm := Comment{
				ID: strconv.Itoa(n.ID), Author: n.Author.Username, Body: n.Body,
				URL: base + strconv.Itoa(n.ID), CreatedAt: n.CreatedAt,
			}
			if cm.Author == "" {
				cm.Author = "ghost"
			}
			if d.IndividualNote || !resolvable && n.Position == nil {
				cm.Kind = CommentConversation
			} else {
				// A thread: on a line of the diff, or a resolvable discussion
				// on the request. Either is something to resolve.
				cm.Kind, cm.ThreadID, cm.Resolved = CommentReview, d.ID, resolved
				if n.Position != nil {
					cm.Path, cm.Line = n.Position.NewPath, n.Position.NewLine
					if cm.Path == "" {
						cm.Path, cm.Line = n.Position.OldPath, n.Position.OldLine
					}
				}
				if first == "" {
					first = cm.ID
				} else {
					cm.ReplyTo = first
				}
			}
			out = append(out, cm)
		}
	}
	return out, nil
}

func (g *GitLab) UnresolvedThreads(ctx context.Context, c Credential, repo Repo, number int) (int, error) {
	if err := g.validRepo(repo); err != nil {
		return 0, err
	}
	iid, err := glIID(number)
	if err != nil {
		return 0, err
	}
	ds, err := g.discussions(ctx, c, repo, iid)
	if err != nil {
		return 0, err
	}
	open := 0
	for _, d := range ds {
		if threadOpen(d) {
			open++
		}
	}
	return open, nil
}

// ---- writes ------------------------------------------------------------

var glDraftPrefix = regexp.MustCompile(`(?i)^\s*(\[draft\]|\(draft\)|draft:)\s*`)

func (g *GitLab) Create(ctx context.Context, c Credential, repo Repo, in CreateInput) (ChangeRequest, error) {
	if err := g.validRepo(repo); err != nil {
		return ChangeRequest{}, err
	}
	if strings.TrimSpace(in.Title) == "" || in.Head == "" || in.Base == "" {
		return ChangeRequest{}, &Error{Kind: ErrInvalid, Message: "a merge request needs a title, a source branch and a target branch"}
	}
	if strings.Contains(in.Head, ":") {
		return ChangeRequest{}, &Error{Kind: ErrInvalid, Message: "DevKit does not open a GitLab merge request from a fork yet; open it on GitLab"}
	}
	title := in.Title
	if in.Draft && !glDraftPrefix.MatchString(title) {
		// GitLab has no draft flag on create: a draft is a title that starts with "Draft:".
		title = "Draft: " + title
	}
	var mr glMR
	err := g.send(ctx, c, repo, http.MethodPost, projectPath(repo)+"/merge_requests", map[string]any{
		"source_branch": in.Head, "target_branch": in.Base, "title": title, "description": in.Body,
	}, &mr)
	if err != nil {
		return ChangeRequest{}, err
	}
	return g.changeRequest(repo, mr), nil
}

func (g *GitLab) Update(ctx context.Context, c Credential, repo Repo, number int, in UpdateInput) (ChangeRequest, error) {
	if err := g.validRepo(repo); err != nil {
		return ChangeRequest{}, err
	}
	iid, err := glIID(number)
	if err != nil {
		return ChangeRequest{}, err
	}
	payload := map[string]any{}
	if in.Title != nil {
		if strings.TrimSpace(*in.Title) == "" {
			return ChangeRequest{}, &Error{Kind: ErrInvalid, Message: "the title cannot be empty"}
		}
		title := *in.Title
		// A draft is a prefix of the title: replacing the title without it would
		// quietly mark the request ready for review.
		current, err := g.getMR(ctx, c, repo, iid)
		if err != nil {
			return ChangeRequest{}, err
		}
		if (current.Draft || current.WorkInProgress) && !glDraftPrefix.MatchString(title) {
			title = "Draft: " + title
		}
		payload["title"] = title
	}
	if in.Body != nil {
		payload["description"] = *in.Body
	}
	if len(payload) == 0 {
		return ChangeRequest{}, &Error{Kind: ErrInvalid, Message: "nothing to change"}
	}
	var mr glMR
	if err := g.send(ctx, c, repo, http.MethodPut, mrPath(repo, iid), payload, &mr); err != nil {
		return ChangeRequest{}, err
	}
	return g.changeRequest(repo, mr), nil
}

func (g *GitLab) noteComment(n glNote, kind CommentKind, repo Repo, iid string) Comment {
	cm := Comment{
		ID: strconv.Itoa(n.ID), Kind: kind, Author: n.Author.Username, Body: n.Body, CreatedAt: n.CreatedAt,
		URL: "https://" + normalizeHost(repo.Host) + "/" + repo.Path + "/-/merge_requests/" + iid + "#note_" + strconv.Itoa(n.ID),
	}
	if n.Position != nil {
		cm.Path, cm.Line = n.Position.NewPath, n.Position.NewLine
	}
	return cm
}

func (g *GitLab) Comment(ctx context.Context, c Credential, repo Repo, number int, body string) (Comment, error) {
	if err := g.validRepo(repo); err != nil {
		return Comment{}, err
	}
	iid, err := glIID(number)
	if err != nil {
		return Comment{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Comment{}, &Error{Kind: ErrInvalid, Message: "the comment is empty"}
	}
	var note glNote
	if err := g.send(ctx, c, repo, http.MethodPost, mrPath(repo, iid)+"/notes", map[string]string{"body": body}, &note); err != nil {
		return Comment{}, err
	}
	return g.noteComment(note, CommentConversation, repo, iid), nil
}

var glDiscussionID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Reply answers a thread. commentID is a note's id from Comments (or a
// discussion id); a conversation comment is not a thread and is refused —
// pr.comment is how one adds to the conversation.
func (g *GitLab) Reply(ctx context.Context, c Credential, repo Repo, number int, commentID, body string) (Comment, error) {
	if err := g.validRepo(repo); err != nil {
		return Comment{}, err
	}
	iid, err := glIID(number)
	if err != nil {
		return Comment{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Comment{}, &Error{Kind: ErrInvalid, Message: "the reply is empty"}
	}
	discussion := ""
	switch {
	case glDiscussionID.MatchString(commentID):
		discussion = commentID
	default:
		noteID, err := strconv.Atoi(commentID)
		if err != nil || noteID <= 0 {
			return Comment{}, &Error{Kind: ErrInvalid, Message: "not a review comment id: " + commentID}
		}
		ds, err := g.discussions(ctx, c, repo, iid)
		if err != nil {
			return Comment{}, err
		}
	find:
		for _, d := range ds {
			for _, n := range d.Notes {
				if n.ID != noteID {
					continue
				}
				if d.IndividualNote || !threadResolvable(d) && n.Position == nil {
					return Comment{}, &Error{Kind: ErrInvalid, Message: "comment " + commentID + " is a conversation comment, not a thread; add to the conversation instead"}
				}
				discussion = d.ID
				break find
			}
		}
		if discussion == "" {
			return Comment{}, &Error{Kind: ErrNotFound, Message: "no review comment " + commentID + " on this merge request"}
		}
	}
	var note glNote
	if err := g.send(ctx, c, repo, http.MethodPost, mrPath(repo, iid)+"/discussions/"+discussion+"/notes", map[string]string{"body": body}, &note); err != nil {
		return Comment{}, err
	}
	cm := g.noteComment(note, CommentReview, repo, iid)
	cm.ThreadID = discussion
	return cm, nil
}

// CheckMergeMethod: GitLab merges the way the project is configured (merge
// commit, semi-linear or fast-forward); a request can only add "squash". A
// rebase is asked of the project, not of one merge.
func (g *GitLab) CheckMergeMethod(m MergeMethod) error {
	if m == MergeMethodRebase {
		return &Error{Kind: ErrInvalid, Message: "GitLab merges with the project's own method (merge commit, semi-linear or fast-forward), so a merge request cannot ask for a rebase; use merge or squash"}
	}
	return nil
}

func (g *GitLab) Merge(ctx context.Context, c Credential, repo Repo, number int, in MergeInput) (MergeResult, error) {
	if err := g.validRepo(repo); err != nil {
		return MergeResult{}, err
	}
	iid, err := glIID(number)
	if err != nil {
		return MergeResult{}, err
	}
	if !in.Method.Valid() {
		return MergeResult{}, &Error{Kind: ErrInvalid, Message: "merge method must be merge or squash"}
	}
	if err := g.CheckMergeMethod(in.Method); err != nil {
		return MergeResult{}, err
	}
	payload := map[string]any{}
	message := strings.TrimSpace(in.Title)
	if b := strings.TrimSpace(in.Body); b != "" {
		if message != "" {
			message += "\n\n"
		}
		message += b
	}
	if in.Method == MergeMethodSquash {
		payload["squash"] = true
		if message != "" {
			payload["squash_commit_message"] = message
		}
	} else if message != "" {
		payload["merge_commit_message"] = message
	}
	// The commit the person saw is the commit that lands: GitLab answers 409
	// when the source branch has moved. should_remove_source_branch is never
	// sent — deleting the branch is pr.deleteBranch, a separate question.
	if in.SHA != "" {
		payload["sha"] = in.SHA
	}
	var mr glMR
	if err := g.send(ctx, c, repo, http.MethodPut, mrPath(repo, iid)+"/merge", payload, &mr); err != nil {
		return MergeResult{}, err
	}
	res := MergeResult{Merged: mr.State == "merged"}
	for _, sha := range []string{mr.MergeCommitSHA, mr.SquashCommitSHA, mr.SHA} {
		if sha != "" {
			res.SHA = sha
			break
		}
	}
	if !res.Merged {
		res.Message = "GitLab accepted the request but the merge request is " + mr.State + ", not merged"
	}
	return res, nil
}

func (g *GitLab) Close(ctx context.Context, c Credential, repo Repo, number int) (ChangeRequest, error) {
	if err := g.validRepo(repo); err != nil {
		return ChangeRequest{}, err
	}
	iid, err := glIID(number)
	if err != nil {
		return ChangeRequest{}, err
	}
	var mr glMR
	if err := g.send(ctx, c, repo, http.MethodPut, mrPath(repo, iid), map[string]string{"state_event": "close"}, &mr); err != nil {
		return ChangeRequest{}, err
	}
	return g.changeRequest(repo, mr), nil
}

func (g *GitLab) DeleteBranch(ctx context.Context, c Credential, repo Repo, name string) error {
	if err := g.validRepo(repo); err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return &Error{Kind: ErrInvalid, Message: "no branch named"}
	}
	return g.send(ctx, c, repo, http.MethodDelete, projectPath(repo)+"/repository/branches/"+url.PathEscape(name), nil, nil)
}

// sortChecks puts failures first, then what is still running: the order a
// person reads in.
func sortChecks(checks []Check) {
	rank := map[CheckStatus]int{CheckFailed: 0, CheckRunning: 1, CheckPassed: 2, CheckSkipped: 3}
	sort.SliceStable(checks, func(i, j int) bool { return rank[checks[i].Status] < rank[checks[j].Status] })
}

var _ Provider = (*GitLab)(nil)
