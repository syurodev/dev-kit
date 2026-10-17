package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
	gitservice "devkit/internal/git"
	"devkit/internal/tasks"
)

func jsonLen(t *testing.T, v any) int {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return len(raw)
}

// A work tree with changes in several places, as an agent's repository is
// halfway through a task.
func busyStatus() gitservice.Status {
	st := gitservice.Status{Branch: "claude/job-subagents", Files: map[string]string{}}
	for i := 0; i < 14; i++ {
		p := fmt.Sprintf("internal/agenthub/subagents_part%02d.go", i)
		st.Files[p] = "M"
		st.Unstaged = append(st.Unstaged, p)
	}
	for i := 0; i < 3; i++ {
		p := fmt.Sprintf("frontend/src/modules/agenthub/surfaces/Panel%d.tsx", i)
		st.Files[p] = "M"
		st.Staged = append(st.Staged, p)
		if i == 0 {
			st.Unstaged = append(st.Unstaged, p) // staged and then edited again
		}
	}
	for i := 0; i < 4; i++ {
		p := fmt.Sprintf("docs/superpowers/specs/2026-10-17-note-%d.md", i)
		st.Files[p] = "A"
		st.Untracked = append(st.Untracked, p)
	}
	return st
}

func TestGitStatusIsCompactAndKeepsWhatAnAgentReads(t *testing.T) {
	st := busyStatus()
	before := jsonLen(t, gitStatusResult2Old{Repo: "dev-kit-app", Status: st})
	out := compactGitStatus("dev-kit-app", st)
	after := jsonLen(t, out)
	t.Logf("git.status, 21 changed paths: %d -> %d bytes", before, after)
	if after*100 > before*65 {
		t.Errorf("compact status is %d of %d bytes, want at most 65%%", after, before)
	}
	if out.Branch != "claude/job-subagents" || out.Repo != "dev-kit-app" || out.Clean {
		t.Errorf("header lost: %+v", out)
	}
	if len(out.Unstaged) != 15 || len(out.Staged) != 3 || len(out.Untracked) != 4 {
		t.Errorf("lists = %d/%d/%d", len(out.Unstaged), len(out.Staged), len(out.Untracked))
	}
	if out.Unstaged[0] != "M internal/agenthub/subagents_part00.go" {
		t.Errorf("an entry is '<state> <path>', got %q", out.Unstaged[0])
	}
	raw, _ := json.Marshal(out)
	for _, empty := range []string{`"upstream"`, `"ahead"`, `"behind"`, `"detached"`, `"conflicted"`, `"files"`, `"omitted"`} {
		if strings.Contains(string(raw), empty) {
			t.Errorf("an empty field is spelled out: %s in %s", empty, raw)
		}
	}
}

// gitStatusResult2Old is git.status's answer before it was compacted.
type gitStatusResult2Old struct {
	Repo string `json:"repo,omitempty"`
	gitservice.Status
}

func TestGitStatusCleanAndCapped(t *testing.T) {
	clean := compactGitStatus("", gitservice.Status{Branch: "main", Upstream: "origin/main", Files: map[string]string{}})
	raw, _ := json.Marshal(clean)
	if !clean.Clean || string(raw) != `{"branch":"main","upstream":"origin/main","clean":true}` {
		t.Errorf("clean tree answered %s", raw)
	}
	messy := gitservice.Status{Branch: "main", Files: map[string]string{}}
	for i := 0; i < 450; i++ {
		p := fmt.Sprintf("dist/chunk-%03d.js", i)
		messy.Untracked = append(messy.Untracked, p)
		messy.Files[p] = "A"
	}
	out := compactGitStatus("", messy)
	if len(out.Untracked) != statusListCap || out.Omitted["untracked"] != 350 {
		t.Errorf("untracked = %d, omitted = %v", len(out.Untracked), out.Omitted)
	}
}

func TestTaskListsDropHistoryAndEmptyFields(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	var list []tasks.Task
	for i := 0; i < 20; i++ {
		task := tasks.Task{ID: fmt.Sprintf("task-%02d-aaaaaaaa", i), BoardID: "board-1", Key: fmt.Sprintf("API-%02d", i), Title: "Implement the thing", Status: "todo", CreatedAt: now, UpdatedAt: now, Description: strings.Repeat("d", 300)}
		for j := 0; j < 6; j++ {
			task.History = append(task.History, tasks.Transition{From: "todo", To: "active", By: "agent:session", At: now.Format(time.RFC3339)})
		}
		list = append(list, task)
	}
	got, err := compactTaskList(list, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, after := jsonLen(t, list), jsonLen(t, got)
	t.Logf("tasks.list, 20 tasks with 6 transitions each: %d -> %d bytes", before, after)
	if after*100 > before*35 {
		t.Errorf("compact list is %d of %d bytes", after, before)
	}
	raw, _ := json.Marshal(got)
	for _, gone := range []string{`"history"`, `"description"`, `"createdAt"`, `"priority"`, `"assignee"`, `"labels"`, `"branch"`} {
		if strings.Contains(string(raw), gone) {
			t.Errorf("%s survived: %s", gone, raw[:200])
		}
	}
	for _, kept := range []string{`"id"`, `"key"`, `"title"`, `"status"`, `"boardId"`, `"updatedAt"`} {
		if !strings.Contains(string(raw), kept) {
			t.Errorf("%s lost", kept)
		}
	}
	// The card for create/get/update/transition reads id, key, title, status — and
	// a transition keeps its task under "task" (frontend/src/modules/tasks/taskResult.ts).
	tr, _ := compactTransition(tasks.TransitionResult{Task: list[0], Verifying: true, RunID: "r"}, nil)
	raw, _ = json.Marshal(tr)
	if !strings.Contains(string(raw), `"task":{"id":"task-00-aaaaaaaa"`) || !strings.Contains(string(raw), `"verifying":true`) || !strings.Contains(string(raw), `"runId":"r"`) {
		t.Errorf("transition = %s", raw)
	}
	one, _ := compactTaskResult(list[0], nil)
	raw, _ = json.Marshal(one)
	if strings.Contains(string(raw), "history") || strings.Contains(string(raw), "description") {
		t.Errorf("a write echoes the history or the description: %s", raw)
	}
}

func TestPeerListMeasured(t *testing.T) {
	var peers []agenthub.PeerInfo
	parent := "11111111-2222-3333-4444-555555555555"
	for i := 0; i < 12; i++ {
		peers = append(peers, agenthub.PeerInfo{
			SessionID: fmt.Sprintf("aaaaaaaa-bbbb-cccc-dddd-%012d", i), Title: "Settings → Agents: split into six one-topic pages and add the models page",
			AgentType: agenthub.AgentTypeCodex, Status: agenthub.SessionStatusIdle, Relation: agenthub.PeerSameWorkspace, ParentID: parent,
		})
	}
	for i := 0; i < 8; i++ {
		peers = append(peers, agenthub.PeerInfo{SessionID: fmt.Sprintf("cccccccc-bbbb-cccc-dddd-%012d", i), Title: "old", AgentType: agenthub.AgentTypeClaude, Status: agenthub.SessionStatusClosed, Relation: agenthub.PeerSameWorkspace})
	}
	before, after := jsonLen(t, peers), jsonLen(t, compactPeers(peers))
	t.Logf("agenthub.listPeers, 12 live + 8 closed: %d -> %d bytes", before, after)
	if after*100 > before*65 {
		t.Errorf("compact peers is %d of %d bytes", after, before)
	}
}
