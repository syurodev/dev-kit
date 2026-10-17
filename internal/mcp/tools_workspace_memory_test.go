package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/apperrors"
	"devkit/internal/identity"
	"devkit/internal/workspacememory"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const memoryWorkspaceID = "000000000000000000000001"

type fakeMemory struct {
	ws, id string
	source workspacememory.Source
	calls  int
	err    error
}

func (f *fakeMemory) hit(ws, id string, source workspacememory.Source) (workspacememory.Entry, error) {
	f.calls++
	f.ws, f.id, f.source = ws, id, source
	return workspacememory.Entry{ID: id, WorkspaceID: ws, Revision: 1, Source: source}, f.err
}
func (f *fakeMemory) List(_ context.Context, ws, q string, limit, offset int) (workspacememory.Page, error) {
	_, e := f.hit(ws, "", workspacememory.Source{})
	return workspacememory.Page{Entries: []workspacememory.Summary{}}, e
}
func (f *fakeMemory) Get(_ context.Context, ws, id string) (workspacememory.Entry, error) {
	return f.hit(ws, id, workspacememory.Source{})
}
func (f *fakeMemory) Create(_ context.Context, ws string, in workspacememory.Input, source workspacememory.Source) (workspacememory.Entry, error) {
	return f.hit(ws, "new", source)
}
func (f *fakeMemory) Update(_ context.Context, ws, id string, rev int64, in workspacememory.Input, source workspacememory.Source) (workspacememory.Entry, error) {
	return f.hit(ws, id, source)
}
func (f *fakeMemory) Delete(_ context.Context, ws, id string, rev int64, source workspacememory.Source) (workspacememory.Entry, error) {
	return f.hit(ws, id, source)
}
func (f *fakeMemory) History(_ context.Context, ws, id string, limit, offset int) ([]workspacememory.Revision, error) {
	_, e := f.hit(ws, id, workspacememory.Source{})
	return []workspacememory.Revision{}, e
}
func (f *fakeMemory) Restore(_ context.Context, ws, id string, expected, target int64, source workspacememory.Source) (workspacememory.Entry, error) {
	return f.hit(ws, id, source)
}

func memoryTestServer(c *fakeCaller, f *fakeMemory, session agenthub.AgentSession) *Server {
	d := agentHubTestDeps(c, &fakeAgentHub{status: session})
	d.WorkspaceMemory = f
	return NewServer(d)
}
func assertMemoryError(t *testing.T, err error, code string) {
	t.Helper()
	var ae *apperrors.AppError
	if !errors.As(err, &ae) || ae.Code != code {
		t.Fatalf("error = %v, expected %s", err, code)
	}
}

type memoryOperation struct {
	name  string
	write bool
	run   func(*Server, context.Context, string) error
}

func memoryOperations() []memoryOperation {
	return []memoryOperation{
		{"list", false, func(s *Server, c context.Context, w string) error {
			_, _, e := s.toolMemoryList(c, nil, memoryListInput{WorkspaceID: w})
			return e
		}},
		{"search", false, func(s *Server, c context.Context, w string) error {
			_, _, e := s.toolMemorySearch(c, nil, memoryListInput{WorkspaceID: w, Query: "sensitive query"})
			return e
		}},
		{"get", false, func(s *Server, c context.Context, w string) error {
			_, _, e := s.toolMemoryGet(c, nil, memoryGetInput{WorkspaceID: w, ID: "entry"})
			return e
		}},
		{"create", true, func(s *Server, c context.Context, w string) error {
			_, _, e := s.toolMemoryCreate(c, nil, memoryCreateInput{WorkspaceID: w, Kind: "knowledge", Title: "sensitive title", Summary: "sensitive summary", Body: "sensitive body"})
			return e
		}},
		{"update", true, func(s *Server, c context.Context, w string) error {
			_, _, e := s.toolMemoryUpdate(c, nil, memoryUpdateInput{WorkspaceID: w, ID: "entry", ExpectedRevision: 1, Kind: "knowledge", Title: "sensitive title", Summary: "sensitive summary", Body: "sensitive body"})
			return e
		}},
		{"delete", true, func(s *Server, c context.Context, w string) error {
			_, _, e := s.toolMemoryDelete(c, nil, memoryDeleteInput{WorkspaceID: w, ID: "entry", ExpectedRevision: 1})
			return e
		}},
		{"history", false, func(s *Server, c context.Context, w string) error {
			_, _, e := s.toolMemoryHistory(c, nil, memoryHistoryInput{WorkspaceID: w, ID: "entry"})
			return e
		}},
		{"restore", true, func(s *Server, c context.Context, w string) error {
			_, _, e := s.toolMemoryRestore(c, nil, memoryRestoreInput{WorkspaceID: w, ID: "entry", ExpectedRevision: 2, TargetRevision: 1})
			return e
		}},
	}
}
func TestMemoryAllOperationsScopeAndGateway(t *testing.T) {
	session := agenthub.AgentSession{ID: "session", WorkspaceID: memoryWorkspaceID, AgentType: agenthub.AgentTypeClaude, ProjectPath: "/shared/project"}
	ctx := identity.WithCaller(context.Background(), identity.Agent("session"))
	for _, op := range memoryOperations() {
		t.Run(op.name, func(t *testing.T) {
			c, f := &fakeCaller{}, &fakeMemory{}
			s := memoryTestServer(c, f, session)
			assertMemoryError(t, op.run(s, ctx, "other"), "memory.workspaceDenied")
			if f.calls != 0 || c.last.subject != "" {
				t.Fatal("mismatched workspace reached backend or gateway")
			}
			if e := op.run(s, ctx, ""); e != nil {
				t.Fatal(e)
			}
			if f.calls != 1 || f.ws != memoryWorkspaceID {
				t.Fatalf("memory calls=%d workspace=%q", f.calls, f.ws)
			}
			cap := "workspace-memory:read"
			if op.write {
				cap = "workspace-memory:write"
				if f.source != (workspacememory.Source{Caller: "agent:session", SessionID: "session", AgentType: "claude"}) {
					t.Fatalf("source=%+v", f.source)
				}
			}
			if c.last.subject != "builtin.workspace-memory" || c.last.capability != cap || c.last.scope != "workspace:"+memoryWorkspaceID {
				t.Fatalf("gateway=%+v", c.last)
			}
			for key, v := range c.last.metadata {
				if key == "query" || key == "title" || key == "summary" || key == "body" || strings.Contains(strings.TrimSpace(toString(v)), "sensitive") {
					t.Fatalf("sensitive audit metadata: %+v", c.last.metadata)
				}
			}
			c.err = apperrors.New("capability_denied", "Denied", true)
			assertMemoryError(t, op.run(s, ctx, ""), "capability_denied")
			if f.calls != 1 {
				t.Fatal("denied capability reached memory service")
			}
		})
	}
}
func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func TestMemoryIdentityFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		caller  identity.Caller
		session agenthub.AgentSession
	}{
		{"missing", identity.Caller{}, agenthub.AgentSession{}},
		{"malformed-agent", identity.Caller{Kind: identity.KindAgent}, agenthub.AgentSession{}},
		{"unknown-kind", identity.Caller{Kind: "bogus"}, agenthub.AgentSession{}},
		{"ui", identity.UI(), agenthub.AgentSession{}},
		{"unknown-session", identity.Agent("session"), agenthub.AgentSession{}},
		{"different-session", identity.Agent("session"), agenthub.AgentSession{ID: "other", WorkspaceID: "ws"}},
		{"unbound", identity.Agent("session"), agenthub.AgentSession{ID: "session", ProjectPath: "/shared/project"}},
		{"terminal-session", identity.Caller{Kind: identity.KindUserTerminal, SessionID: "session"}, agenthub.AgentSession{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{}
			s := memoryTestServer(&fakeCaller{}, f, tc.session)
			ctx := identity.WithCaller(context.Background(), tc.caller)
			_, _, e := s.toolMemoryGet(ctx, nil, memoryGetInput{WorkspaceID: "ws", ID: "id"})
			assertMemoryError(t, e, "memory.workspaceDenied")
			if f.calls != 0 {
				t.Fatal("invalid caller reached backend")
			}
		})
	}
}
func TestMemoryTerminalRequiresExplicitWorkspace(t *testing.T) {
	f := &fakeMemory{}
	s := memoryTestServer(&fakeCaller{}, f, agenthub.AgentSession{})
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())
	_, _, err := s.toolMemoryCreate(ctx, nil, memoryCreateInput{})
	assertMemoryError(t, err, "memory.workspaceDenied")
	_, _, err = s.toolMemoryCreate(ctx, nil, memoryCreateInput{WorkspaceID: "ws"})
	if err != nil {
		t.Fatal(err)
	}
	if f.ws != "ws" || f.source != (workspacememory.Source{Caller: "user-terminal"}) {
		t.Fatalf("terminal call=%+v", f)
	}
}
func TestMemorySanitizesDomainErrors(t *testing.T) {
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())
	for _, tc := range []struct {
		err  error
		code string
	}{{workspacememory.ErrNotFound, "memory.notFound"}, {workspacememory.ErrWorkspaceNotFound, "memory.notFound"}, {workspacememory.ErrConflict, "memory.revisionConflict"}, {workspacememory.ErrInvalidInput, "validation_failed"}, {errors.New("secret SQL body"), "internal_error"}} {
		f := &fakeMemory{err: tc.err}
		s := memoryTestServer(&fakeCaller{}, f, agenthub.AgentSession{})
		_, _, e := s.toolMemoryGet(ctx, nil, memoryGetInput{WorkspaceID: "ws", ID: "entry"})
		assertMemoryError(t, e, tc.code)
		if strings.Contains(e.Error(), "secret") {
			t.Fatal("raw backend error exposed")
		}
	}
	s := NewServer(Deps{})
	_, _, e := s.toolMemoryGet(ctx, nil, memoryGetInput{WorkspaceID: "ws", ID: "entry"})
	assertMemoryError(t, e, "internal_error")
}
func TestMemoryToolsProtocolAndAuthorship(t *testing.T) {
	ctx := context.Background()
	f := &fakeMemory{}
	s := memoryTestServer(&fakeCaller{}, f, agenthub.AgentSession{ID: "session", WorkspaceID: "ws", AgentType: agenthub.AgentTypeCodex})
	st, ct := sdk.NewInMemoryTransports()
	ss, e := s.sdkServerFor(identity.Agent("session")).Connect(ctx, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, e := client.Connect(ctx, ct, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	listed, e := cs.ListTools(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	for _, op := range memoryOperations() {
		if !names["workspace.memory."+op.name] {
			t.Fatalf("missing %s", op.name)
		}
	}
	result, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: "workspace.memory.create", Arguments: map[string]any{"kind": "knowledge", "title": "Title", "summary": "Summary", "body": "Body"}})
	if e != nil || result.IsError {
		t.Fatalf("create=%+v error=%v", result, e)
	}
	if f.source != (workspacememory.Source{Caller: "agent:session", SessionID: "session", AgentType: "codex"}) {
		t.Fatalf("source=%+v", f.source)
	}
	beforeSpoof := f.calls
	result, e = cs.CallTool(ctx, &sdk.CallToolParams{Name: "workspace.memory.create", Arguments: map[string]any{"kind": "knowledge", "title": "Title", "summary": "Summary", "body": "Body", "source": map[string]any{"caller": "user-terminal", "sessionId": "forged", "agentType": "claude"}}})
	if e == nil && !result.IsError {
		if f.source != (workspacememory.Source{Caller: "agent:session", SessionID: "session", AgentType: "codex"}) {
			t.Fatalf("source spoofed: %+v", f.source)
		}
	} else if f.calls != beforeSpoof {
		t.Fatal("rejected spoof reached backend")
	}
	before := f.calls
	result, e = cs.CallTool(ctx, &sdk.CallToolParams{Name: "workspace.memory.update", Arguments: map[string]any{"id": "entry", "kind": "knowledge", "title": "Title", "summary": "Summary", "body": "Body"}})
	if e == nil && !result.IsError {
		t.Fatal("missing expectedRevision accepted")
	}
	if f.calls != before {
		t.Fatal("invalid protocol reached backend")
	}
}

func TestMemoryAuditDoesNotContainRawIDs(t *testing.T) {
	c, f := &fakeCaller{}, &fakeMemory{}
	s := memoryTestServer(c, f, agenthub.AgentSession{})
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())
	_, _, err := s.toolMemoryGet(ctx, nil, memoryGetInput{WorkspaceID: "private workspace content", ID: "private entry content"})
	if err != nil {
		t.Fatal(err)
	}
	if c.last.scope != "workspace:invalid" || c.last.metadata["workspaceId"] != "invalid" || c.last.metadata["id"] != "invalid" {
		t.Fatalf("raw IDs leaked: %+v", c.last)
	}
	if f.ws != "private workspace content" || f.id != "private entry content" {
		t.Fatal("service did not receive original IDs")
	}
	for _, id := range []string{"000000000000000000000001", "11111111-2222-3333-4444-555555555555"} {
		if memoryRecordID(id) != id {
			t.Fatalf("opaque ID %q was discarded", id)
		}
	}
}
func TestMemoryResolvesSessionOnEveryCall(t *testing.T) {
	c, f := &fakeCaller{}, &fakeMemory{}
	hub := &fakeAgentHub{status: agenthub.AgentSession{ID: "session", WorkspaceID: memoryWorkspaceID, AgentType: agenthub.AgentTypeClaude}}
	d := agentHubTestDeps(c, hub)
	d.WorkspaceMemory = f
	s := NewServer(d)
	ctx := identity.WithCaller(context.Background(), identity.Agent("session"))
	if _, _, err := s.toolMemoryCreate(ctx, nil, memoryCreateInput{}); err != nil {
		t.Fatal(err)
	}
	hub.status.WorkspaceID = "000000000000000000000002"
	hub.status.AgentType = agenthub.AgentTypeCodex
	if _, _, err := s.toolMemoryCreate(ctx, nil, memoryCreateInput{}); err != nil {
		t.Fatal(err)
	}
	if f.ws != hub.status.WorkspaceID || f.source.AgentType != "codex" || hub.statusID != "session" {
		t.Fatalf("stale scope/source: %+v", f)
	}
	hub.status.WorkspaceID = ""
	_, _, err := s.toolMemoryCreate(ctx, nil, memoryCreateInput{})
	assertMemoryError(t, err, "memory.workspaceDenied")
	if f.calls != 2 {
		t.Fatal("unbound session still accessed memory")
	}
}
func TestMemoryMissingDependenciesFailGracefully(t *testing.T) {
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())
	for _, d := range []Deps{{Caller: &fakeCaller{}}, {WorkspaceMemory: &fakeMemory{}}} {
		s := NewServer(d)
		_, _, err := s.toolMemoryGet(ctx, nil, memoryGetInput{WorkspaceID: memoryWorkspaceID, ID: "id"})
		assertMemoryError(t, err, "internal_error")
	}
	s := NewServer(Deps{Caller: &fakeCaller{}, WorkspaceMemory: &fakeMemory{}})
	_, _, err := s.toolMemoryGet(identity.WithCaller(context.Background(), identity.Agent("session")), nil, memoryGetInput{ID: "id"})
	assertMemoryError(t, err, "memory.workspaceDenied")
}

type fakeMemoryBrowser struct {
	fakeMemory
	browseCalls     int
	browseWorkspace string
	input           workspacememory.BrowseInput
	page            workspacememory.BrowsePage
}

func (f *fakeMemoryBrowser) Browse(_ context.Context, ws string, in workspacememory.BrowseInput) (workspacememory.BrowsePage, error) {
	f.browseCalls++
	f.browseWorkspace = ws
	f.input = in
	return f.page, f.err
}
func TestMemoryCursorPagination(t *testing.T) {
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())
	for _, search := range []bool{false, true} {
		t.Run(map[bool]string{false: "list", true: "search"}[search], func(t *testing.T) {
			caller := &fakeCaller{}
			f := &fakeMemoryBrowser{page: workspacememory.BrowsePage{Entries: []workspacememory.Summary{{ID: "entry"}}, HasMore: true, NextCursor: "next cursor"}}
			d := agentHubTestDeps(caller, &fakeAgentHub{})
			d.WorkspaceMemory = f
			s := NewServer(d)
			in := memoryListInput{WorkspaceID: memoryWorkspaceID, Query: "private query", Cursor: "private cursor", Limit: 25}
			var v any
			var err error
			if search {
				_, v, err = s.toolMemorySearch(ctx, nil, in)
			} else {
				_, v, err = s.toolMemoryList(ctx, nil, in)
			}
			if err != nil {
				t.Fatal(err)
			}
			page, ok := unwrap(v).(memoryPageOut)
			if !ok || !page.HasMore || page.NextCursor != "next cursor" || len(page.Entries) != 1 {
				t.Fatalf("page=%#v", unwrap(v))
			}
			if f.browseCalls != 1 || f.calls != 0 || f.browseWorkspace != memoryWorkspaceID || f.input != (workspacememory.BrowseInput{Query: in.Query, Cursor: in.Cursor, Limit: 25}) {
				t.Fatalf("browse=%+v", f)
			}
			for _, value := range caller.last.metadata {
				if strings.Contains(toString(value), "private") {
					t.Fatalf("query/cursor entered audit: %+v", caller.last.metadata)
				}
			}
		})
	}
}
func TestMemoryCursorValidationAndLegacyFallback(t *testing.T) {
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())
	browser := &fakeMemoryBrowser{}
	d := agentHubTestDeps(&fakeCaller{}, &fakeAgentHub{})
	d.WorkspaceMemory = browser
	s := NewServer(d)
	for _, in := range []memoryListInput{
		{Cursor: "cursor", Offset: 1}, {Cursor: "cursor", Offset: -1}, {Cursor: strings.Repeat("x", 2049)}, {Cursor: string([]byte{255})}, {Limit: 101}, {Limit: -1},
	} {
		in.WorkspaceID = memoryWorkspaceID
		_, _, err := s.toolMemoryList(ctx, nil, in)
		assertMemoryError(t, err, "validation_failed")
	}
	if browser.browseCalls != 0 || browser.calls != 0 {
		t.Fatal("invalid input reached backend")
	}
	_, v, err := s.toolMemoryList(ctx, nil, memoryListInput{WorkspaceID: memoryWorkspaceID, Offset: 20, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := unwrap(v).(memoryPageOut); !ok {
		t.Fatalf("legacy page=%#v", v)
	}
	if browser.calls != 1 || browser.browseCalls != 0 {
		t.Fatal("offset call did not retain legacy API")
	}
	legacy := &fakeMemory{}
	d.WorkspaceMemory = legacy
	s = NewServer(d)
	_, _, err = s.toolMemoryList(ctx, nil, memoryListInput{WorkspaceID: memoryWorkspaceID, Cursor: "cursor"})
	assertMemoryError(t, err, "validation_failed")
	if legacy.calls != 0 {
		t.Fatal("unsupported cursor reached legacy backend")
	}
	_, _, err = s.toolMemoryList(ctx, nil, memoryListInput{WorkspaceID: memoryWorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.calls != 1 {
		t.Fatal("legacy API no longer works")
	}
}

func TestMemoryWriteAnswersWithoutTheBodyUnlessAsked(t *testing.T) {
	body := strings.Repeat("a long note ", 500)
	entry := workspacememory.Entry{ID: "e1", WorkspaceID: memoryWorkspaceID, Kind: "decision", Title: "T", Summary: "S", Body: body, Revision: 4, UpdatedAt: "2026-10-17T00:00:00Z", Source: workspacememory.Source{Caller: "agent", SessionID: "s"}}
	whole, _ := json.Marshal(entry)

	compact, _ := json.Marshal(compactMemoryEntry(entry, false))
	if strings.Contains(string(compact), "a long note") || strings.Contains(string(compact), "workspaceId") || strings.Contains(string(compact), "source") {
		t.Fatalf("compact write carries the body, workspace or source: %s", compact)
	}
	for _, field := range []string{`"id":"e1"`, `"revision":4`, `"title":"T"`, `"updatedAt"`, `"kind":"decision"`, `"summary":"S"`} {
		if !strings.Contains(string(compact), field) {
			t.Errorf("compact write lost %s: %s", field, compact)
		}
	}
	if len(compact)*20 > len(whole) {
		t.Errorf("compact answer is %d bytes of %d; expected under 5%% of a 6 KB note", len(compact), len(whole))
	}
	t.Logf("memory write answer: %d -> %d bytes", len(whole), len(compact))

	withBody, _ := json.Marshal(compactMemoryEntry(entry, true))
	if !strings.Contains(string(withBody), "a long note") {
		t.Fatal("includeBody did not return the body")
	}
}

func TestMemoryWriteToolsHonourIncludeBody(t *testing.T) {
	session := agenthub.AgentSession{ID: "session", WorkspaceID: memoryWorkspaceID, AgentType: agenthub.AgentTypeClaude}
	ctx := identity.WithCaller(context.Background(), identity.Agent("session"))
	s := memoryTestServer(&fakeCaller{}, &fakeMemory{}, session)
	for _, include := range []bool{false, true} {
		_, v, err := s.toolMemoryCreate(ctx, nil, memoryCreateInput{Kind: "knowledge", Title: "t", Summary: "s", Body: "b", IncludeBody: include})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := unwrap(v).(memoryEntryOut); !ok {
			t.Fatalf("create answered %T, want the compact entry", unwrap(v))
		}
		_, v, err = s.toolMemoryUpdate(ctx, nil, memoryUpdateInput{ID: "e", ExpectedRevision: 1, Kind: "knowledge", Title: "t", Summary: "s", Body: "b", IncludeBody: include})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := unwrap(v).(memoryEntryOut); !ok {
			t.Fatalf("update answered %T", unwrap(v))
		}
	}
}

func TestMemoryHistoryAndListAreCompact(t *testing.T) {
	rows := []workspacememory.Revision{
		{Operation: "update", Entry: workspacememory.Entry{Revision: 3, Title: "T", Summary: "S", Body: strings.Repeat("x", 6000), UpdatedAt: "u", WorkspaceID: "w"}},
		{Operation: "create", Entry: workspacememory.Entry{Revision: 2, Title: "T", Summary: "S", Body: strings.Repeat("y", 6000), UpdatedAt: "u"}},
	}
	short, _ := json.Marshal(compactMemoryHistory(rows, false))
	if len(short) > 400 || strings.Contains(string(short), "xxx") {
		t.Errorf("history without includeBody is %d bytes: %s", len(short), short)
	}
	full, _ := json.Marshal(compactMemoryHistory(rows, true))
	if !strings.Contains(string(full), "yyy") {
		t.Error("history with includeBody lost the bodies")
	}
	t.Logf("history of 2 revisions of a 6 KB note: %d -> %d bytes", len(full), len(short))

	page := compactMemoryPage(workspacememory.BrowsePage{HasMore: true, NextCursor: "c", Entries: []workspacememory.Summary{{ID: "a", WorkspaceID: "w", Kind: "k", Title: "t", Summary: "s", Revision: 1, UpdatedAt: "u", CreatedAt: "c0", Source: workspacememory.Source{Caller: "x"}}}})
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "workspaceId") || strings.Contains(string(raw), "createdAt") || strings.Contains(string(raw), "source") {
		t.Errorf("list row carries fields the caller knows: %s", raw)
	}
	if !strings.Contains(string(raw), `"nextCursor":"c"`) || !strings.Contains(string(raw), `"hasMore":true`) {
		t.Errorf("list lost its paging: %s", raw)
	}
}
