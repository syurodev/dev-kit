package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"devkit/internal/apperrors"
	"devkit/internal/capabilities"
	"devkit/internal/environments"
	"devkit/internal/identity"
	"devkit/internal/notes"
	"devkit/internal/ops"
	"devkit/internal/tools"
	"devkit/internal/vault"
	"encoding/json"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type callRecord struct {
	subject, capability, scope string
	metadata                   map[string]any
	caller                     string
}

type fakeCaller struct {
	last callRecord
	err  error
}

func (f *fakeCaller) Call(
	ctx context.Context,
	subject, capability, scope string,
	metadata map[string]any,
	handler capabilities.Handler,
) (any, error) {
	f.last = callRecord{
		subject:    subject,
		capability: capability,
		scope:      scope,
		metadata:   metadata,
		caller:     identity.Label(ctx),
	}
	if f.err != nil {
		return nil, f.err
	}
	return handler(ctx)
}

type fakeNotes struct {
	list []notes.Summary
	note notes.Note
}

func (f *fakeNotes) List() ([]notes.Summary, error)         { return f.list, nil }
func (f *fakeNotes) Search(string) ([]notes.Summary, error) { return f.list, nil }
func (f *fakeNotes) Get(string) (notes.Note, error)         { return f.note, nil }
func (f *fakeNotes) Create(string, string, string, []string) (notes.Summary, error) {
	return notes.Summary{ID: "created", Title: "t"}, nil
}
func (f *fakeNotes) Update(string, string, string) (notes.Note, error) { return f.note, nil }
func (f *fakeNotes) Delete(string) error                               { return nil }
func (f *fakeNotes) SetOrganization(string, string, []string, string) (notes.Summary, error) {
	return notes.Summary{ID: "org"}, nil
}
func (f *fakeNotes) ListFolders() ([]notes.Folder, error) { return nil, nil }
func (f *fakeNotes) CreateFolder(string, string, []string) (notes.Folder, error) {
	return notes.Folder{ID: "f", Name: "Work", Color: "blue"}, nil
}
func (f *fakeNotes) UpdateFolder(string, string, string, []string) (notes.Folder, error) {
	return notes.Folder{ID: "f", Name: "Ops", Color: "red"}, nil
}
func (f *fakeNotes) DeleteFolder(string) error { return nil }

type fakeVault struct {
	state vault.State
	err   error
}

func (f *fakeVault) Status() (vault.State, error) { return f.state, f.err }

type fakeSearch struct {
	hits []SearchHit
}

func (f *fakeSearch) Search(string) ([]SearchHit, error) { return f.hits, nil }

type fakeTools struct{}

func (fakeTools) Hash(req tools.HashRequest) (tools.HashResult, error) {
	return tools.HashResult{Algorithm: req.Algorithm, Output: "deadbeef"}, nil
}

type fakeEnvs struct {
	tags []environments.Tag
	req  map[string]bool
}

func (f *fakeEnvs) ListTags() ([]environments.Tag, error) { return f.tags, nil }

func (f *fakeEnvs) RequiresConfirmation(name string) bool {
	if f.req != nil {
		if v, ok := f.req[name]; ok {
			return v
		}
	}
	return true // fail closed
}

func testDeps(caller *fakeCaller, notesAPI NotesAPI) Deps {
	if notesAPI == nil {
		notesAPI = &fakeNotes{}
	}
	return Deps{
		Caller: caller,
		Notes:  notesAPI,
		Vault:  &fakeVault{state: vault.StateUnlocked},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{},
	}
}

func TestNotesListTool(t *testing.T) {
	caller := &fakeCaller{}
	want := []notes.Summary{{ID: "abc", Title: "hello", UpdatedAt: time.Unix(1, 0).UTC()}}
	srv := NewServer(testDeps(caller, &fakeNotes{list: want}))

	res, v, err := invokeNoteOp(srv, context.Background(), ops.NotesList, ops.NotesEmptyIn{})
	if err != nil {
		t.Fatalf("toolNotesList: %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil CallToolResult, got %#v", res)
	}
	summaries, ok := unwrap(v).([]notes.Summary)
	if !ok || len(summaries) != 1 || summaries[0].Title != "hello" {
		t.Fatalf("output = %#v, want fake summaries", v)
	}
	got, ok := caller.last.metadata["origin"].(string)
	if !ok || got != OriginMCP {
		t.Fatalf("origin = %v, want %q", caller.last.metadata["origin"], OriginMCP)
	}
	if caller.last.subject != "builtin.notes" || caller.last.capability != "notes:read" || caller.last.scope != "note:list" {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["action"] != "list" {
		t.Fatalf("action = %v, want list", caller.last.metadata["action"])
	}
}

func TestNotesDeleteDestructivePath(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, &fakeNotes{}))
	id := "0123456789abcdef01234567"

	_, _, err := invokeNoteOp(srv, context.Background(), ops.NotesDelete, ops.NotesIDIn{ID: id})
	if err != nil {
		t.Fatalf("toolNotesDelete: %v", err)
	}
	if caller.last.subject != "builtin.notes" || caller.last.capability != "notes:write" {
		t.Fatalf("Call subject/cap = %s/%s", caller.last.subject, caller.last.capability)
	}
	if caller.last.scope != "note:"+id {
		t.Fatalf("scope = %q, want note:%s", caller.last.scope, id)
	}
	if caller.last.metadata["action"] != "delete" {
		t.Fatalf("action = %v", caller.last.metadata["action"])
	}
	if caller.last.metadata["origin"] != OriginMCP {
		t.Fatalf("origin = %v", caller.last.metadata["origin"])
	}
	if caller.last.metadata["note_id"] != id {
		t.Fatalf("note_id = %v", caller.last.metadata["note_id"])
	}
}

func TestVaultStatusLockedError(t *testing.T) {
	caller := &fakeCaller{
		err: apperrors.New("vault_locked", "Vault is locked or unavailable.", true),
	}
	srv := NewServer(testDeps(caller, nil))

	_, _, err := srv.toolVaultStatus(context.Background(), &sdk.CallToolRequest{}, struct{}{})
	if err == nil {
		t.Fatal("expected vault_locked error")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != "vault_locked" {
		t.Fatalf("err = %v, want vault_locked AppError", err)
	}
	if caller.last.subject != "builtin.vault" || caller.last.capability != "vault:manage" || caller.last.scope != "vault" {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["origin"] != OriginMCP {
		t.Fatalf("origin = %v", caller.last.metadata["origin"])
	}
}

func TestToolsHashAndEnvListTags(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{state: vault.StateLocked},
		Search: &fakeSearch{hits: []SearchHit{{ID: "1", Type: "note", Label: "n"}}},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{tags: []environments.Tag{{Name: "dev"}}},
	})

	_, _, err := srv.toolToolsHash(context.Background(), &sdk.CallToolRequest{}, toolsHashInput{
		Algorithm: "sha256",
		Input:     "hi",
	})
	if err != nil {
		t.Fatalf("tools.hash: %v", err)
	}
	if caller.last.subject != "builtin.tools" || caller.last.capability != "tools:hash" || caller.last.scope != "command:tools.hash" {
		t.Fatalf("hash Call = %+v", caller.last)
	}
	if caller.last.metadata["action"] != "execute" || caller.last.metadata["origin"] != OriginMCP {
		t.Fatalf("hash meta = %v", caller.last.metadata)
	}

	_, _, err = srv.toolEnvListTags(context.Background(), &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatalf("env.listTags: %v", err)
	}
	if caller.last.subject != "builtin.environments" || caller.last.capability != "env:read" || caller.last.scope != "tag:list" {
		t.Fatalf("env Call = %+v", caller.last)
	}
}

func TestNotesFolderCreateTool(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, &fakeNotes{}))
	_, v, err := invokeNoteOp(srv, context.Background(), ops.NotesFolderCreate, ops.NotesFolderCreateIn{
		Name: "Work", Color: "blue", Tags: []string{"work"},
	})
	if err != nil {
		t.Fatalf("toolNotesFolderCreate: %v", err)
	}
	folder, ok := unwrap(v).(notes.Folder)
	if !ok || folder.Name != "Work" {
		t.Fatalf("output = %#v", v)
	}
	if caller.last.capability != "notes:write" || caller.last.scope != "note-folder:new" {
		t.Fatalf("Call = %+v", caller.last)
	}
}

func TestNotesSetOrganizationTool(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, &fakeNotes{}))
	id := "0123456789abcdef01234567"
	_, _, err := invokeNoteOp(srv, context.Background(), ops.NotesSetOrganization, ops.NotesSetOrganizationIn{
		ID: id, FolderID: "", Tags: []string{"solo"}, Color: "red",
	})
	if err != nil {
		t.Fatalf("toolNotesSetOrganization: %v", err)
	}
	if caller.last.scope != "note:"+id || caller.last.metadata["action"] != "set_organization" {
		t.Fatalf("Call = %+v", caller.last)
	}
}

func TestMetaMergesExtra(t *testing.T) {
	m := meta("get", map[string]any{"note_id": "x"})
	if m["origin"] != OriginMCP || m["action"] != "get" || m["note_id"] != "x" {
		t.Fatalf("meta = %v", m)
	}
}

func (f *fakeCaller) CallOperation(ctx context.Context, r capabilities.CallRequest) (any, error) {
	return f.Call(ctx, r.SubjectID, r.Capability, r.Scope, r.Metadata, r.Handler)
}
func invokeNoteOp[I, O any](s *Server, ctx context.Context, op ops.Op[I, O], in I) (*sdk.CallToolResult, any, error) {
	ctx = identity.WithCaller(ctx, identity.UserTerminal())
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, nil, err
	}
	v, err := op.InvokeJSON(ctx, ops.Runtime{Deps: ops.Deps{Notes: s.deps.Notes}, Caller: s.deps.Caller, Origin: "mcp"}, raw)
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}
