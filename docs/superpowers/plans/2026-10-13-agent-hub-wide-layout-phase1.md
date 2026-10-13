# Agent Hub Wide Layout — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship §1–§4 of `docs/superpowers/specs/2026-10-13-agent-hub-wide-layout-design.md`:
- the redesigned sidebar;
- a persisted session preview;
- a wider conversation with a floating session summary card;
- clickable links and file paths.

**Architecture:**
- **Backend:** Go adds `agent_sessions.preview` (migration 41). It is written when a turn ends, kept out of MCP results, and reaches the UI on the existing session DTO. A new bridge method lists a session's PDF documents.
- **Sidebar:** the frontend rewrites the session row (`AgentSessionCard`) and the group chrome, and adds a cross-workspace "Needs you" group.
- **Main view:** the main view gets a container-query layout. At the Tailwind `@6xl` size (72rem ≈ 1152px) of the `agent-conversation` container, the summary card docks and the column shifts. Below it, a corner button opens the card as a popover. No JS measurement.
- **Links and paths:** they open through a small bridge wrapper over `Browser.OpenURL` and the existing `onOpenFile` chain, threaded with a React context.

**Tech Stack:** Go 1.25 + SQLite (`modernc`), Wails v3 bindings, React 19 + TypeScript, Tailwind v4 (container queries, `text-(--token)`), bun test (`renderToString`, no DOM), `@formkit/auto-animate`, framer-motion `AnimatedCollapse`, Radix Popover/Dialog.

**Spec deviations decided while planning** (record them in the spec in the last task):
- Docking threshold is Tailwind's `@6xl` (72rem). Arbitrary `[1200px]` values are banned by `shadcn/no-arbitrary-values` and `designSystemConformance.test.ts`.
- Group and stack expand/collapse keep the existing `AnimatedCollapse` (framer). The CSS `.tree-branch` grid pattern was removed on purpose in `1a6a872c`.
- "Changes" sums `added`/`removed` over all of the session's checkpoints. A row opens that path's diff from the newest checkpoint that touched it. There is no header `↗` (YAGNI).
- Sources lists PDFs, URLs and DevKit data tools by module (Database / Kafka / SSH, with a call count). `db.query` runs on the active target and has no profile in its input.
- MCP results blank `Preview` rather than tagging it `json:"-"`, which would also hide it from the Wails binding.
- No sidebar virtualization yet (§1.5). The existing partition draws at most 5 recent rows plus exceptions per group, so rows are bounded until someone opens a stack of more than 200. Revisit if a fixture shows otherwise.

---

## Conventions for every task

- Paths are relative to the repo root `/home/ptvu3/Downloads/dev-kit`. Frontend commands run in `frontend/`.
- Branch: `feat/mono-chrome-color-palettes`.
- Never stage `.serena/`. Never `git stash`. Never `--no-verify`. Do not push.
- Commit trailer: the `Co-Authored-By` line for the model doing the work.
- Frontend tests use `renderToString` (no DOM). New tests follow the same style.
- Before every frontend commit, run:
  - `bun test src/modules/agenthub src/designSystemConformance.test.ts src/smoothnessRatchet.test.ts src/storeSubscriptionRatchet.test.ts`
  - `bunx tsc --noEmit -p .`
  - `bunx eslint <changed files>` (no new errors).
- Before every Go commit, run:
  - `gofmt -l internal`
  - `go test ./internal/agenthub/ ./internal/storage/ ./internal/mcp/ ./internal/bridge/ ./internal/arch/ -count=1`

## File map

| File | Change |
|---|---|
| `internal/storage/sqlite.go`, `internal/storage/migrations_test.go` | migration 41 `agent_sessions.preview` |
| `internal/agenthub/types.go`, `store.go` | `AgentSession.Preview`, scan, `UpdateSessionPreview` |
| `internal/agenthub/preview.go` (new) + `preview_test.go` | `PreviewFromMessage`, `PreviewFromError` |
| `internal/agenthub/service.go` | capture the final message per turn and write the preview |
| `internal/mcp/tools_agenthub.go` + test | blank `Preview` in MCP results |
| `internal/bridge/agenthub.go` + test | `AgentHubListDocuments` |
| `frontend/bindings/**` | regenerated |
| `frontend/src/bridge/agenthub.ts` | `preview`, `agentHubListDocuments` |
| `frontend/src/workbench/AgentSessionsSubscriber.tsx` | refetch a session on a foreign `turnFinished` |
| `frontend/src/bridge/browser.ts` (new) + test | `openExternal(url)` |
| `frontend/src/modules/agenthub/transcript/fileRef.ts` (new) + test | path detection |
| `frontend/src/modules/agenthub/transcript/transcriptActions.tsx` (new) | context for link/file handlers |
| `frontend/src/modules/agenthub/transcript/AgentMessageRich.tsx` | links → buttons, paths → buttons |
| `frontend/src/modules/agenthub/surfaces/TranscriptStream.tsx` | `columnClassName`, provide actions context |
| `frontend/src/modules/agenthub/sidebar/model.ts` + test | `needsYou`, `secondLine`, `groupCollapsed` |
| `frontend/src/stores/agentPins.ts` | `expanded` keys |
| `frontend/src/modules/agenthub/sidebar/SessionAvatar.tsx` (new) | avatar + status |
| `frontend/src/modules/agenthub/sidebar/AgentSessionCard.tsx` | row redesign |
| `frontend/src/modules/agenthub/sidebar/AgentSessionGroup.tsx` | label + stack restyle, auto-animate |
| `frontend/src/modules/agenthub/sidebar/AgentSessionSidebar.tsx` | Needs-you group, default-collapsed other workspaces |
| `frontend/src/modules/agenthub/AgentHubSidebar.tsx` | `attentionText`, current workspace id, expanded keys |
| `frontend/src/modules/agenthub/summary/summaryModel.ts` (new) + test | plan / changes / sources derivations |
| `frontend/src/modules/agenthub/summary/SessionSummary.tsx` (new) + test | the card + collapsed button |
| `frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx` | column, docking, card |
| `frontend/src/modules/agenthub/surfaces/AgentTurn.tsx` | attachment cards, fold hairline |
| `frontend/src/modules/agenthub/surfaces/composer/ComposerContextBar.tsx` | hide subagents chip when docked |
| `frontend/src/index.css` | avatar keyframes |

---

### Task 1: Migration 41 — `agent_sessions.preview`

**Files:** Modify `internal/storage/sqlite.go` (after m40, ~line 1337; the runner list ~line 1339), `internal/storage/migrations_test.go:184-186`.

- [ ] **Step 1: Failing test.** In `migrations_test.go` change the pinned version to 41:
  ```go
  if version != 41 {
  	t.Fatalf("migration version = %d, want 41", version)
  }
  ```
  Then add a test at the end of the file:
  ```go
  func TestMigration41AddsSessionPreview(t *testing.T) {
  	db := openTestDB(t) // use the helper the other tests in this file use to open + migrate; if it has another name, use that one
  	rows, err := db.Query(`SELECT name, dflt_value, "notnull" FROM pragma_table_info('agent_sessions') WHERE name = 'preview'`)
  	if err != nil { t.Fatal(err) }
  	defer rows.Close()
  	if !rows.Next() { t.Fatal("agent_sessions.preview missing") }
  	var name string; var dflt sql.NullString; var notNull int
  	if err := rows.Scan(&name, &dflt, &notNull); err != nil { t.Fatal(err) }
  	if notNull != 1 || dflt.String != "''" { t.Fatalf("preview: notnull=%d default=%q", notNull, dflt.String) }
  }
  ```
  Read the top of `migrations_test.go` first and use its real open/migrate helper and imports.
- [ ] **Step 2:** `go test ./internal/storage/ -run 'Migration' -count=1`. Expected: FAIL (version 40, column missing).
- [ ] **Step 3: Implement.** After `m40` in `sqlite.go`:
  ```go
  	// m41: the line the sidebar shows under a session's title — where it got
  	// to, written when a turn ends (agenthub.PreviewFromMessage/FromError).
  	m41 := Migration{
  		Version: 41,
  		Name:    "agent_sessions preview",
  		Up: func(ctx context.Context) error {
  			return addColumnIfNotExists(ctx, db, TableAgentSessions, "preview", "TEXT NOT NULL DEFAULT ''")
  		},
  	}
  ```
  Append `m41` to the `NewMigrationRunner` slice after `m40`.
- [ ] **Step 4:** `go test ./internal/storage/ -count=1`. Expected: PASS.
- [ ] **Step 5:** Commit `storage: agent_sessions.preview column (migration 41)`.

### Task 2: Session struct, store read and write

**Files:** Modify `internal/agenthub/types.go:29-50`, `internal/agenthub/store.go` (`sessionColumns` :80, `scanSession` :82-109, the `Store` interface :22-42, writers :167-214). Test: `internal/agenthub/store_preview_test.go` (new).

- [ ] **Step 1: Failing test** `store_preview_test.go`:
  ```go
  package agenthub

  import (
  	"context"
  	"testing"
  	"time"
  )

  func TestUpdateSessionPreviewRoundTrips(t *testing.T) {
  	st := newTestSQLStore(t) // use the helper existing store tests use (grep "func newTest" in *_test.go); create one session through it
  	ctx := context.Background()
  	s := createTestSession(t, st) // likewise: reuse the existing helper that inserts a session
  	at := time.Now().UTC()
  	if err := st.UpdateSessionPreview(ctx, s.ID, "Fixed the TTL.", at); err != nil {
  		t.Fatal(err)
  	}
  	got, err := st.GetSession(ctx, s.ID)
  	if err != nil { t.Fatal(err) }
  	if got.Preview != "Fixed the TTL." { t.Fatalf("preview = %q", got.Preview) }
  	if err := st.UpdateSessionPreview(ctx, "missing", "x", at); err != ErrSessionNotFound {
  		t.Fatalf("missing session err = %v", err)
  	}
  }
  ```
  First find the real helper names used by the existing store tests. If no "create session" helper exists, insert via `st.CreateSession(ctx, AgentSession{...})` with the minimal fields other tests use.
- [ ] **Step 2:** `go test ./internal/agenthub/ -run UpdateSessionPreview -count=1`. Expected: compile error (`Preview`, `UpdateSessionPreview` undefined).
- [ ] **Step 3: Implement.**
  - `types.go`, inside `AgentSession` after `Title`:
    ```go
    	// Preview is where the session got to, one line: the first sentence of
    	// the agent's last message, or why the turn failed. Written when a turn
    	// ends. Agent output, stored like the transcript; MCP results blank it
    	// (internal/mcp/tools_agenthub.go), the UI shows it under the title.
    	Preview string `json:"Preview,omitempty"`
    ```
  - `store.go`: `sessionColumns` gains `, preview` before `, created_at`. `scanSession` scans into `&s.Preview` at the same position (between `&extraRW` and `&createdAt`).
  - Add to the `Store` interface:
    ```go
    	UpdateSessionPreview(ctx context.Context, id, preview string, at time.Time) error
    ```
    and to `SQLStore`, next to `UpdateSessionStatus`:
    ```go
    func (st *SQLStore) UpdateSessionPreview(ctx context.Context, id, preview string, at time.Time) error {
    	return st.updateSessionColumn(ctx, `preview = ?`, id, preview, at)
    }
    ```
  - If any test fake implements `Store` without embedding it, add a no-op method. Run `go vet ./internal/agenthub/` to find out.
- [ ] **Step 4:** `go test ./internal/agenthub/ -count=1`. Expected: PASS.
- [ ] **Step 5:** Commit `agenthub: persist a session preview`.

### Task 3: Preview text rules

**Files:** Create `internal/agenthub/preview.go`, `internal/agenthub/preview_test.go`.

- [ ] **Step 1: Failing test:**
  ```go
  package agenthub

  import (
  	"errors"
  	"strings"
  	"testing"
  	"unicode/utf8"
  )

  func TestPreviewFromMessage(t *testing.T) {
  	cases := map[string]string{
  		"":                                    "",
  		"   \n ":                              "",
  		"Done. I also ran the tests.":         "Done.",
  		"Fixed it!\nNext I will refactor.":    "Fixed it!",
  		"No full stop here":                   "No full stop here",
  		"Line one\n\n  line two":              "Line one line two",
  		"Đã sửa TTL. Tiếp theo là test.":      "Đã sửa TTL.",
  		"See v1.2.3 for details. Then more.":  "See v1.2.3 for details.",
  	}
  	for in, want := range cases {
  		if got := PreviewFromMessage(in); got != want {
  			t.Errorf("PreviewFromMessage(%q) = %q, want %q", in, got, want)
  		}
  	}
  }

  func TestPreviewClipsToRunes(t *testing.T) {
  	long := strings.Repeat("ữ", 300)
  	got := PreviewFromMessage(long)
  	if utf8.RuneCountInString(got) != previewMaxRunes {
  		t.Fatalf("runes = %d", utf8.RuneCountInString(got))
  	}
  	if !strings.HasSuffix(got, "…") { t.Fatalf("no ellipsis: %q", got) }
  }

  func TestPreviewFromError(t *testing.T) {
  	vendor := &VendorError{Agent: "codex", Err: ErrAgentUsageLimit, Message: "You hit your usage limit. Try again at 14:00."}
  	if got := PreviewFromError(vendor); got != "You hit your usage limit." {
  		t.Fatalf("vendor: %q", got)
  	}
  	if got := PreviewFromError(errors.New("boom")); got != "boom" {
  		t.Fatalf("plain: %q", got)
  	}
  	if got := PreviewFromError(nil); got != "" { t.Fatalf("nil: %q", got) }
  }
  ```
  Check `VendorError`'s real field types in `errors.go:84-91` (`Agent AgentType`) and adjust the literal.
- [ ] **Step 2:** `go test ./internal/agenthub/ -run Preview -count=1`. Expected: undefined.
- [ ] **Step 3: Implement** `preview.go`:
  ```go
  package agenthub

  import (
  	"strings"
  	"unicode"
  	"unicode/utf8"
  )

  // previewMaxRunes bounds the sidebar's second line. Runes, not bytes: a
  // Vietnamese sentence is two or three bytes a letter.
  const previewMaxRunes = 140

  // PreviewFromMessage is the first sentence of an agent message, on one line.
  // A sentence ends at . ! ? or … followed by whitespace or the end, so
  // "v1.2.3" and "e.g." inside a word do not cut it.
  func PreviewFromMessage(text string) string {
  	folded := strings.Join(strings.Fields(text), " ")
  	if folded == "" {
  		return ""
  	}
  	runes := []rune(folded)
  	for i, r := range runes {
  		if r != '.' && r != '!' && r != '?' && r != '…' {
  			continue
  		}
  		if i == len(runes)-1 || unicode.IsSpace(runes[i+1]) {
  			return clipPreview(string(runes[:i+1]))
  		}
  	}
  	return clipPreview(folded)
  }

  // PreviewFromError is why a turn failed, in the vendor's own words when it
  // gave any (the only place that says when a limit lifts), else the error.
  func PreviewFromError(err error) string {
  	if err == nil {
  		return ""
  	}
  	if msg := ErrorVendorMessage(err); msg != "" {
  		return PreviewFromMessage(msg)
  	}
  	return PreviewFromMessage(err.Error())
  }

  func clipPreview(s string) string {
  	if utf8.RuneCountInString(s) <= previewMaxRunes {
  		return s
  	}
  	r := []rune(s)
  	return strings.TrimRightFunc(string(r[:previewMaxRunes-1]), unicode.IsSpace) + "…"
  }
  ```
  `clipPreview` must return exactly `previewMaxRunes` runes in the long case. The test string has no spaces, so trimming does not shorten it. Keep the test as written.
- [ ] **Step 4:** `go test ./internal/agenthub/ -run Preview -count=1`. Expected: PASS.
- [ ] **Step 5:** Commit `agenthub: preview text from a message or a failure`.

### Task 4: Write the preview when a turn ends

**Files:** Modify `internal/agenthub/service.go`:
- `Invoke` ~1382–1590: the per-turn `OnUpdate` ~1420–1449, the failure path ~1517–1538, success ~1587–1590.

Test: `internal/agenthub/preview_service_test.go` (new).

- [ ] **Step 1: Failing test.** Use the mock adapter path that existing `Service.Invoke` tests use (grep `func TestInvoke` / `newTestService` in `internal/agenthub/*_test.go`, and reuse that harness):
  ```go
  func TestInvokeWritesPreviewOnSuccess(t *testing.T) {
  	svc, st := newServiceForPreviewTest(t) // wrap the existing harness: a Service with the mock adapter + SQL store
  	sess := mustCreateMockSession(t, svc)
  	if _, err := svc.Invoke(context.Background(), InvokeInput{SessionID: sess.ID, Prompt: "hi"}); err != nil {
  		t.Fatal(err)
  	}
  	got, _ := st.GetSession(context.Background(), sess.ID)
  	if got.Preview == "" {
  		t.Fatal("preview not written")
  	}
  }
  ```
  The mock adapter replies with a fixed agent message (`adapter_mock.go`). Assert the preview equals `PreviewFromMessage(<that text>)`. Add a second test where the mock prompt fails (use the existing failure switch in the mock, if there is one). It asserts `Preview == PreviewFromError(err)`. If the mock cannot fail, skip that test and note it in the report.
- [ ] **Step 2:** Run it. Expected: FAIL (preview empty).
- [ ] **Step 3: Implement** inside `Invoke`.
  - Next to the existing per-turn locals (`lastUpdate`, `touched`):
    ```go
    	// The agent's last message this turn, for the session preview. Only the
    	// top-level agent's text: subagent chunks carry an Origin.
    	var lastMsg strings.Builder
    	lastMsgID := ""
    ```
  - In the turn's `OnUpdate`, after `parsed := s.handleUpdate(recordCtx, in.SessionID, update)`:
    ```go
    		if parsed.Kind == UpdateAgentMessageChunk && parsed.Origin == "" {
    			if parsed.MessageID != lastMsgID {
    				lastMsg.Reset()
    				lastMsgID = parsed.MessageID
    			}
    			lastMsg.WriteString(parsed.Text())
    		}
    ```
    The handler may run on another goroutine. If the existing locals in that closure are guarded by a mutex, guard these with the same one. If they are not, add a `sync.Mutex` local and lock it around both the write and the read below.
  - Before **every** `setSessionStatus(recordCtx, in.SessionID, SessionStatusError|Idle)` in the failure branch (~1517–1538) and the later error exits (~1546–1583):
    ```go
    		_ = s.store.UpdateSessionPreview(recordCtx, in.SessionID, PreviewFromError(promptErr /* or the local error at that exit */), time.Now().UTC())
    ```
    Skip it when the error is `ErrCancelled`, so a cancelled turn keeps the previous preview.
  - Before the success `setSessionStatus(... SessionStatusIdle)` (~1587):
    ```go
    	if p := PreviewFromMessage(lastMsg.String()); p != "" {
    		_ = s.store.UpdateSessionPreview(recordCtx, in.SessionID, p, time.Now().UTC())
    	}
    ```
  - Preview errors are ignored on purpose. They must never fail a turn.
- [ ] **Step 4:** `go test ./internal/agenthub/ -count=1`. Expected: PASS (all).
- [ ] **Step 5:** Commit `agenthub: write the session preview when a turn ends`.

### Task 5: Keep the preview out of MCP

**Files:** Modify `internal/mcp/tools_agenthub.go` (`toolAgentHubListAgents` :70-81, `toolAgentHubCreateAgentSession` :91-113, `toolAgentHubGetSessionStatus` :141-157). Test: `internal/mcp/tools_agenthub_test.go`.

- [ ] **Step 1: Failing test.** Find the existing test that calls `agenthub.listAgents` with a fake `AgentHub` dependency. Make the fake return a session with `Preview: "secret-ish"`, then assert the marshalled result does not contain `secret-ish`:
  ```go
  	raw, _ := json.Marshal(result.StructuredContent) // use the field the existing assertions read
  	if strings.Contains(string(raw), "secret-ish") {
  		t.Fatal("preview leaked into MCP result")
  	}
  ```
- [ ] **Step 2:** Run `go test ./internal/mcp/ -run AgentHub -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement** in `tools_agenthub.go`:
  ```go
  // forAgent is a session as an MCP caller sees it. Preview is the agent's own
  // words for the person's sidebar; agents read the transcript, not this.
  func forAgent(s agenthub.AgentSession) agenthub.AgentSession {
  	s.Preview = ""
  	return s
  }

  func forAgents(list []agenthub.AgentSession) []agenthub.AgentSession {
  	out := make([]agenthub.AgentSession, len(list))
  	for i, s := range list {
  		out[i] = forAgent(s)
  	}
  	return out
  }
  ```
  Wrap the values passed to `structured(...)` in the three tools.
- [ ] **Step 4:** `go test ./internal/mcp/ -count=1`. Expected: PASS.
- [ ] **Step 5:** Commit `mcp: agent session results do not carry the preview`.

### Task 6: Bridge — documents list, bindings, frontend session type

**Files:**
- Modify `internal/bridge/agenthub.go` (add the method near `AgentHubListCheckpoints` ~461) and its test file (`internal/bridge/agenthub_test.go`, or wherever `AgentHubListCheckpoints` is tested).
- Regenerate `frontend/bindings/`.
- Modify `frontend/src/bridge/agenthub.ts` (type :48-65, `toSession` :222-235, new function).
- Modify `frontend/src/workbench/AgentSessionsSubscriber.tsx:84-113`.

- [ ] **Step 1: Failing Go test.** Mirror the existing `AgentHubListCheckpoints` bridge test. It calls `AgentHubListDocuments(sessionID)` on a service whose `Documents` library holds one stored document, and asserts the name and pages. If wiring a real `DocumentLibrary` in the bridge test is heavy, assert instead that a nil library returns an empty slice and nil error.
- [ ] **Step 2:** `go test ./internal/bridge/ -run Documents -count=1`. Expected: undefined method.
- [ ] **Step 3: Implement.**
  ```go
  // AgentHubListDocuments lists the PDFs attached to a session, oldest first,
  // for the session summary card. Metadata only — no bytes, no text.
  func (s *AppService) AgentHubListDocuments(id string) ([]agenthub.Document, error) {
  	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "session:"+id, map[string]any{
  		"action": "list_documents", "session_id": id,
  	}, func(ctx context.Context) (any, error) {
  		if s.container.AgentHub == nil || s.container.AgentHub.Documents == nil {
  			return []agenthub.Document{}, nil
  		}
  		return s.container.AgentHub.Documents.List(ctx, id)
  	})
  	if err != nil {
  		return nil, err
  	}
  	return result.([]agenthub.Document), nil
  }
  ```
  - Copy the exact `s.call(...)` shape, subject string and container field name from `AgentHubListCheckpoints`. They may differ from the sketch above.
  - If a grant test enumerates bridge methods (grep `TestEvery` in `internal/bridge`), add the method there with the same grant as `AgentHubListCheckpoints`.
- [ ] **Step 4:** `go test ./internal/bridge/ ./internal/arch/ -count=1`. Expected: PASS.
- [ ] **Step 5: Regenerate bindings.**
  - Run `task generate:bindings`.
  - If `wails3` is missing, install it in a throwaway module (AGENTS.md "Regenerating bindings") and run `wails3 generate bindings -clean=true -ts -i` from the repo root.
  - Check `git diff --stat frontend/bindings`. Expect `AgentSession` to gain `"Preview"?: string`, a new `AgentHubListDocuments`, and a `Document` model. Nothing unrelated should churn; if it does, report it and do not commit that churn.
- [ ] **Step 6: Frontend bridge.** In `frontend/src/bridge/agenthub.ts`:
  - Add `preview?: string;` to `AgentSession`, with a comment ("Where the session got to — written when a turn ends").
  - In `toSession`, add `preview: raw.Preview ?? "",`.
  - Add:
    ```ts
    export type AgentDocument = { id: string; name: string; pages: number; size: number };

    export function agentHubListDocuments(sessionId: string): Promise<AgentDocument[]> {
      return callBridge(async () => {
        const raw = (await AppService.AgentHubListDocuments(sessionId)) ?? [];
        return raw.map((d) => ({ id: d.id, name: d.name, pages: d.pages ?? 0, size: d.size ?? 0 }));
      });
    }
    ```
    Match the import name of `AppService` used in this file and the binding's real field names. `Document` has lowercase json tags.
- [ ] **Step 7: Foreign turn ends.** In `AgentSessionsSubscriber.tsx` `handleAgentTurnFinished`, in the `!ours` branch, after the status patch:
  ```ts
      // The preview is written with the status; a turn from elsewhere (MCP,
      // another agent) only told us the status, so read the row once.
      void agentHubGetStatus(sessionId).then((fresh) => {
        if (fresh) useAgentSessionsStore.getState().upsertSession(fresh);
      }).catch(() => {});
  ```
  Use the store accessor this file already uses.
- [ ] **Step 8:** Run `bunx tsc --noEmit -p . && bun test src/workbench src/modules/agenthub`. Expected: PASS.
- [ ] **Step 9:** Commit `bridge: session preview and documents reach the UI`. Include the bindings.

### Task 7: `openExternal` and the path matcher

**Files:** Create `frontend/src/bridge/browser.ts`, `frontend/src/bridge/browser.test.ts`, `frontend/src/modules/agenthub/transcript/fileRef.ts`, `frontend/src/modules/agenthub/transcript/fileRef.test.ts`.

- [ ] **Step 1: Failing tests.**
  `browser.test.ts`:
  ```ts
  import { describe, expect, test } from "bun:test";
  import { openExternalWith } from "./browser";

  describe("openExternal", () => {
    test("opens http(s) and mailto", async () => {
      const opened: string[] = [];
      const open = openExternalWith({ open: async (u) => void opened.push(u), copy: async () => {}, notify: () => {} });
      await open("https://grafana.local/d/auth");
      await open("mailto:a@b.c");
      expect(opened).toEqual(["https://grafana.local/d/auth", "mailto:a@b.c"]);
    });
    test("refuses other schemes", async () => {
      const opened: string[] = [];
      const open = openExternalWith({ open: async (u) => void opened.push(u), copy: async () => {}, notify: () => {} });
      await open("javascript:alert(1)");
      await open("file:///etc/passwd");
      expect(opened).toEqual([]);
    });
    test("copies and tells when the runtime cannot open", async () => {
      const copied: string[] = [];
      const notes: string[] = [];
      const open = openExternalWith({ open: async () => { throw new Error("no runtime"); }, copy: async (u) => void copied.push(u), notify: (m) => void notes.push(m) });
      await open("https://x.test");
      expect(copied).toEqual(["https://x.test"]);
      expect(notes).toEqual(["Link copied"]);
    });
  });
  ```
  `fileRef.test.ts`:
  ```ts
  import { describe, expect, test } from "bun:test";
  import { fileRef } from "./fileRef";

  describe("fileRef", () => {
    test("paths with a source extension", () => {
      expect(fileRef("auth/session.go:88")).toEqual({ path: "auth/session.go", line: 88 });
      expect(fileRef("src/App.tsx:12:4")).toEqual({ path: "src/App.tsx", line: 12 });
      expect(fileRef("internal/agenthub/service.go")).toEqual({ path: "internal/agenthub/service.go" });
      expect(fileRef("/abs/path/main.go")).toEqual({ path: "/abs/path/main.go" });
      expect(fileRef("README.md")).toEqual({ path: "README.md" });
    });
    test("not paths", () => {
      for (const s of ["https://x.test/a.go", "SessionTTL", "a b/c.go", "v1.2.3", "x.y", "npm run build", "1.5", "../"]) {
        expect(fileRef(s)).toBeNull();
      }
    });
  });
  ```
- [ ] **Step 2:** `bun test src/bridge/browser.test.ts src/modules/agenthub/transcript/fileRef.test.ts`. Expected: module not found.
- [ ] **Step 3: Implement.**
  `browser.ts`:
  ```ts
  import { Browser } from "@wailsio/runtime";
  import { toast } from "sonner";
  import { copyText } from "./clipboard";

  type Deps = { open: (url: string) => Promise<void>; copy: (text: string) => Promise<void>; notify: (message: string) => void };

  const ALLOWED = /^(https?:|mailto:)/i;

  /** The testable core: open what is allowed, copy it when the runtime cannot. */
  export function openExternalWith(deps: Deps) {
    return async (url: string): Promise<void> => {
      const trimmed = url.trim();
      if (!ALLOWED.test(trimmed)) return;
      try {
        await deps.open(trimmed);
      } catch {
        await deps.copy(trimmed);
        deps.notify("Link copied");
      }
    };
  }

  /**
   * Opens a link in the system browser. An <a target="_blank"> does nothing in
   * the Wails WebView, which is why agent links never opened. Server mode has
   * no native browser to hand it to, so the link is copied instead.
   */
  export const openExternal = openExternalWith({
    open: (url) => Browser.OpenURL(url),
    copy: (text) => copyText(text),
    notify: (message) => void toast(message),
  });
  ```
  Check that `copyText` in `frontend/src/bridge/clipboard.ts` has the signature `(text: string) => Promise<void>`. Adapt if not.

  `fileRef.ts`:
  ```ts
  export type FileRef = { path: string; line?: number };

  const SOURCE_EXT = /\.(go|ts|tsx|js|jsx|mjs|cjs|java|kt|lua|py|rb|rs|c|h|cpp|hpp|cs|swift|sql|json|ya?ml|toml|md|mdx|css|scss|html|sh|proto|gradle|xml|properties|env)$/i;
  const SHAPE = /^(\/?[\w.@-]+(?:\/[\w.@-]+)*)(?::(\d+))?(?::\d+)?$/;

  /**
   * A file reference written as inline code — `auth/session.go:88` — or null.
   * Conservative on purpose: a wrong link is worse than plain text, so it must
   * look like a path AND end in a source extension. URLs, versions and
   * identifiers stay code.
   */
  export function fileRef(text: string): FileRef | null {
    if (text.includes("://") || /\s/.test(text)) return null;
    const match = SHAPE.exec(text);
    if (!match) return null;
    const path = match[1];
    if (!SOURCE_EXT.test(path)) return null;
    const line = match[2] ? Number(match[2]) : undefined;
    return line ? { path, line } : { path };
  }
  ```
- [ ] **Step 4:** Run the two tests. Expected: PASS. Note that `"v1.2.3"` and `"x.y"` fail on the extension, as intended.
- [ ] **Step 5:** Commit `agenthub: open links in the system browser; recognise file refs`.

### Task 8: Clickable links and paths in agent messages

**Files:**
- Create `frontend/src/modules/agenthub/transcript/transcriptActions.tsx`.
- Modify `transcript/AgentMessageRich.tsx` (inline code :86-105, link :108-120), `surfaces/TranscriptStream.tsx` (provide the context near the root :562).
- Test: `transcript/transcriptComponents.test.tsx` (extend).

- [ ] **Step 1: Failing test.** In `transcriptComponents.test.tsx` add:
  ```tsx
  import { TranscriptActionsProvider } from "./transcriptActions";

  test("links render as buttons that name their URL", () => {
    const html = renderToString(<AgentProse text="See [dash](https://grafana.local/d/auth) now" />);
    expect(html).toContain('data-testid="agent-link"');
    expect(html).toContain('title="https://grafana.local/d/auth"');
    expect(html).not.toContain('target="_blank"');
  });

  test("a file ref in inline code is a button when files can open", () => {
    const html = renderToString(
      <TranscriptActionsProvider value={{ onOpenFile: () => {} }}>
        <AgentProse text="Look at `auth/session.go:88` and `SessionTTL`" />
      </TranscriptActionsProvider>,
    );
    expect(html.match(/data-testid="agent-file-ref"/g)?.length).toBe(1);
    expect(html).toContain(">auth/session.go:88<");
  });

  test("without a file handler a ref stays plain code", () => {
    const html = renderToString(<AgentProse text="`auth/session.go:88`" />);
    expect(html).not.toContain("agent-file-ref");
  });
  ```
  Import `AgentProse` the way the file already does.
- [ ] **Step 2:** `bun test src/modules/agenthub/transcript/transcriptComponents.test.tsx`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  `transcriptActions.tsx`:
  ```tsx
  import * as React from "react";

  export type TranscriptActions = { onOpenFile?: (path: string, line?: number) => void };

  const Ctx = React.createContext<TranscriptActions>({});

  /** What prose deep inside a turn may do, without threading props through every block. */
  export function TranscriptActionsProvider({ value, children }: { value: TranscriptActions; children: React.ReactNode }) {
    return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
  }

  export function useTranscriptActions(): TranscriptActions {
    return React.useContext(Ctx);
  }
  ```
  In `AgentMessageRich.tsx`:
  - Inside `InlineNodes`, read `const { onOpenFile } = useTranscriptActions();` once at the top.
  - `code` case: compute `const ref = onOpenFile ? fileRef(token.value) : null;`. When `ref` is set, render
    ```tsx
    <button key={idx} type="button" data-testid="agent-file-ref"
      onClick={() => onOpenFile!(ref.path, ref.line)}
      className="cursor-pointer whitespace-pre-wrap wrap-anywhere rounded-md bg-muted px-(--space-inline-code-x) py-(--space-inline-code-y) text-left align-baseline text-code-12 text-text-primary underline decoration-text-tertiary underline-offset-2 hover:decoration-text-primary">
      {token.value}
    </button>
    ```
    Otherwise render the existing `<code>` unchanged.
  - `link` case: replace the `<a>` with:
    ```tsx
    <Tooltip key={idx}>
      <TooltipTrigger asChild>
        <button type="button" data-testid="agent-link" title={token.href}
          onClick={() => void openExternal(token.href)}
          className="cursor-pointer wrap-anywhere text-left text-text-primary underline decoration-text-tertiary underline-offset-3 hover:decoration-text-primary">
          <InlineNodes tokens={token.children} />
        </button>
      </TooltipTrigger>
      <TooltipContent side="top" className="max-w-96 break-all">{token.href}</TooltipContent>
    </Tooltip>
    ```
    - If `underline-offset-3` is not a valid Tailwind v4 utility in this project, use `underline-offset-2`.
    - Check whether `AgentProse` is rendered outside a `TooltipProvider` in tests. If `renderToString` throws without a provider, keep the `title` attribute and drop the Tooltip wrapper. The `title` is enough.
  - Imports: `openExternal` from `@/bridge/browser`, `fileRef` from `./fileRef`, `useTranscriptActions` from `./transcriptActions`.

  In `TranscriptStream.tsx`, wrap the root `<div data-testid="transcript-stream" …>` children in `<TranscriptActionsProvider value={actions}>`, where `const actions = React.useMemo(() => ({ onOpenFile }), [onOpenFile]);`.
- [ ] **Step 4:** Run `bun test src/modules/agenthub && bunx tsc --noEmit -p .`. Expected: PASS. Update any existing assertion that pinned `target="_blank"` or `text-status-info underline` on links.
- [ ] **Step 5:** Commit `agenthub: links open in the browser, file refs open in the editor`.

### Task 9: Sidebar model — Needs you, second line, default collapse

**Files:** Modify `frontend/src/modules/agenthub/sidebar/model.ts`, `sidebar/model.test.ts`.

- [ ] **Step 1: Failing tests** in `model.test.ts`:
  ```ts
  import { needsYou, secondLine, groupCollapsed } from "./model";

  const s = (id: string, status: string, extra: Partial<AgentSession> = {}) =>
    ({ id, agentType: "claude", projectPath: "/w/api", status, parentSessionId: null, depth: 0, title: id, acpSessionId: "", updatedAt: 0, ...extra }) as AgentSession;

  describe("needsYou", () => {
    test("waiting or failed sessions, newest first", () => {
      const list = [s("a", "idle", { updatedAt: 1 }), s("b", "error", { updatedAt: 3 }), s("c", "running", { updatedAt: 2 }), s("d", "idle", { updatedAt: 4 })];
      const attention = { c: { permissions: 1, questions: 0 } };
      expect(needsYou(list, attention).map((x) => x.id)).toEqual(["b", "c"]);
    });
    test("closed sessions never need you", () => {
      expect(needsYou([s("a", "closed")], { a: { permissions: 1, questions: 0 } })).toEqual([]);
    });
  });

  describe("secondLine", () => {
    test("a pending ask wins and is a warning", () => {
      expect(secondLine({ session: s("a", "running", { preview: "old" }), ask: "Allow migrate up?", folder: null }))
        .toEqual({ text: "Allow migrate up?", tone: "warning", folder: null });
    });
    test("an error shows the preview as danger", () => {
      expect(secondLine({ session: s("a", "error", { preview: "Usage limit." }), ask: null, folder: "api" }))
        .toEqual({ text: "Usage limit.", tone: "danger", folder: "api" });
    });
    test("otherwise the preview, plain", () => {
      expect(secondLine({ session: s("a", "idle", { preview: "Done." }), ask: null, folder: null }))
        .toEqual({ text: "Done.", tone: "plain", folder: null });
    });
    test("nothing to say", () => {
      expect(secondLine({ session: s("a", "idle"), ask: null, folder: null })).toEqual({ text: "", tone: "plain", folder: null });
    });
  });

  describe("groupCollapsed", () => {
    test("the current workspace is open unless collapsed", () => {
      expect(groupCollapsed("workspace:1", "1", new Set(), new Set())).toBe(false);
      expect(groupCollapsed("workspace:1", "1", new Set(["workspace:1"]), new Set())).toBe(true);
    });
    test("other groups are collapsed unless expanded", () => {
      expect(groupCollapsed("workspace:2", "1", new Set(), new Set())).toBe(true);
      expect(groupCollapsed("workspace:2", "1", new Set(), new Set(["workspace:2"]))).toBe(false);
      expect(groupCollapsed("project:/x", "1", new Set(), new Set())).toBe(true);
    });
    test("with no current workspace every group follows the collapsed list", () => {
      expect(groupCollapsed("workspace:2", null, new Set(), new Set())).toBe(false);
    });
  });
  ```
  Use the import style `model.test.ts` already has for `AgentSession`.
- [ ] **Step 2:** `bun test src/modules/agenthub/sidebar/model.test.ts`. Expected: FAIL (exports missing).
- [ ] **Step 3: Implement** in `model.ts`:
  ```ts
  /**
   * The sessions that wait on the person, across every workspace: a pending
   * permission or question, or a failed turn. Newest first. They are drawn
   * once, here, and left out of their own group.
   */
  export function needsYou(sessions: readonly AgentSession[], attention: Record<string, Attention>): AgentSession[] {
    return sessions
      .filter((x) => x.status !== "closed" && (x.status === "error" || attentionKind(x, attention[x.id]) !== null))
      .sort(compareRecent);
  }

  export type SecondLine = { text: string; tone: "plain" | "warning" | "danger"; folder: string | null };

  /** What a row says under its title — see spec §1.1 for the order. */
  export function secondLine(input: { session: Pick<AgentSession, "status" | "preview">; ask: string | null; folder: string | null }): SecondLine {
    const { session, ask, folder } = input;
    if (ask) return { text: ask, tone: "warning", folder };
    const text = session.preview ?? "";
    if (session.status === "error") return { text, tone: "danger", folder };
    return { text, tone: "plain", folder };
  }

  /**
   * Whether a group is folded. The workspace chosen in the title bar opens by
   * default; every other group folds by default, so the rail is about the
   * work in front of you. With no current workspace, nothing folds by default.
   */
  export function groupCollapsed(key: string, currentWorkspaceId: string | null, collapsed: ReadonlySet<string>, expanded: ReadonlySet<string>): boolean {
    if (!currentWorkspaceId || key === `workspace:${currentWorkspaceId}`) return collapsed.has(key);
    return !expanded.has(key);
  }
  ```
  Use the real group-key format from `groupSessions` (`workspace:<id>`, `project:<path>`, scratch). Confirm in `model.ts` around L99 and adjust the test if it differs. `compareRecent` already exists (L181).
- [ ] **Step 4:** Run the test. Expected: PASS.
- [ ] **Step 5:** Commit `agenthub: sidebar model for needs-you, second line and folding`.

### Task 10: Expanded keys in the pins store

**Files:** Modify `frontend/src/stores/agentPins.ts` (state :20-28, actions near `toggleCollapsed` :80-93, persistence :109-119). Test: the store test next to it, or `sidebar/AgentSessionSidebar.test.tsx` "store tests".

- [ ] **Step 1: Failing test:**
  ```ts
  test("expanded keys toggle and persist like collapsed", () => {
    const { toggleExpanded } = useAgentPinsStore.getState();
    toggleExpanded("workspace:2");
    expect(useAgentPinsStore.getState().expanded).toEqual(["workspace:2"]);
    toggleExpanded("workspace:2");
    expect(useAgentPinsStore.getState().expanded).toEqual([]);
  });
  ```
- [ ] **Step 2:** Run it. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - Add `expanded: string[]` (default `[]`) and `toggleExpanded(key)`, mirroring `collapsed` / `toggleCollapsed` exactly.
  - Include `expanded` in whatever `partialize` / persisted shape the store uses.
  - If the persisted state is versioned, bump the version and default `expanded` to `[]` in the migrate function.
- [ ] **Step 4:** Run it. Expected: PASS.
- [ ] **Step 5:** Commit `stores: remember which non-current workspace groups are open`.

### Task 11: Session avatar and its motion

**Files:** Create `frontend/src/modules/agenthub/sidebar/SessionAvatar.tsx`. Modify `frontend/src/index.css` (near `.agent-card-enter` ~L1856, reduced-motion ~L1915). Test: `sidebar/SessionAvatar.test.tsx` (new).

- [ ] **Step 1: Failing test:**
  ```tsx
  import { describe, expect, test } from "bun:test";
  import { renderToString } from "react-dom/server";
  import { SessionAvatar } from "./SessionAvatar";

  describe("SessionAvatar", () => {
    test("running draws a spinning arc, nothing else", () => {
      const html = renderToString(<SessionAvatar vendor="claude" state="running" />);
      expect(html).toContain("agent-avatar-spin");
      expect(html).not.toContain("agent-avatar-badge");
    });
    test("waiting draws a breathing warning dot", () => {
      const html = renderToString(<SessionAvatar vendor="codex" state="waiting" />);
      expect(html).toContain("agent-avatar-badge");
      expect(html).toContain("bg-status-warning");
      expect(html).toContain("agent-avatar-breathe");
    });
    test("error is a still danger dot; idle is just the face", () => {
      expect(renderToString(<SessionAvatar vendor="claude" state="error" />)).toContain("bg-status-danger");
      const idle = renderToString(<SessionAvatar vendor="claude" state={null} />);
      expect(idle).not.toContain("agent-avatar-badge");
      expect(idle).not.toContain("agent-avatar-spin");
    });
  });
  ```
- [ ] **Step 2:** Run it. Expected: FAIL.
- [ ] **Step 3: Implement.**
  `SessionAvatar.tsx`:
  ```tsx
  import { cn } from "@/lib/utils";
  import { VendorIcon } from "../surfaces/VendorIcon";

  export type AvatarState = "running" | "waiting" | "error" | null;

  /**
   * A session's face: the vendor's mark on a 30px disc, and its state drawn on
   * the disc rather than beside it — an arc going round while it works, a dot
   * when it waits on you or failed. Idle draws nothing: a row of identical
   * "idle" marks says nothing.
   */
  export function SessionAvatar({ vendor, state, className }: { vendor: string; state: AvatarState; className?: string }) {
    return (
      <span data-slot="session-avatar" data-state={state ?? "idle"} className={cn("relative inline-flex size-7.5 shrink-0", className)}>
        <span className="flex size-full items-center justify-center rounded-full bg-surface-base ring-1 ring-border-subtle ring-inset">
          <VendorIcon vendor={vendor} className="size-3.75" />
        </span>
        {state === "running" ? (
          <span aria-hidden="true" className="agent-avatar-spin pointer-events-none absolute -inset-0.75 rounded-full" />
        ) : null}
        {state === "waiting" || state === "error" ? (
          <span aria-hidden="true" className={cn(
            "agent-avatar-badge absolute -right-px -bottom-px size-2.5 rounded-full ring-2 ring-(--avatar-ground,var(--background-app))",
            state === "waiting" ? "agent-avatar-breathe bg-status-warning" : "bg-status-danger",
          )} />
        ) : null}
      </span>
    );
  }
  ```
  - The arc's border lives in the `.agent-avatar-spin` CSS class below, not in utilities: a 1.5px border would need an arbitrary `[1.5px]` value, which `designSystemConformance` bans in `modules/agenthub`.
  - `size-7.5` (30px), `size-3.75` and `-inset-0.75` are valid spacing-scale steps (0.25rem units). If eslint or tsc reject any of them, use `size-8` / `size-4` / `-inset-1`.

  `index.css` (next to the other `agent-*` rules):
  ```css
  /* Session avatar (sidebar/SessionAvatar.tsx). The arc is a quarter-ring
     turning slowly; the badge sits on the row's ground (--avatar-ground,
     set by the row: app on rest, raised when selected or hovered). */
  .agent-avatar-spin {
    border: 1.5px solid transparent;
    border-top-color: var(--text-primary);
    border-right-color: var(--text-primary);
    animation: agent-avatar-spin 1.4s linear infinite;
  }
  @keyframes agent-avatar-spin { to { transform: rotate(360deg); } }
  .agent-avatar-breathe { animation: agent-avatar-breathe 2.4s ease-in-out infinite; }
  @keyframes agent-avatar-breathe { 50% { opacity: 0.4; } }
  ```
  and inside the existing `@media (prefers-reduced-motion: reduce)` block add `.agent-avatar-spin, .agent-avatar-breathe { animation: none; }`.
- [ ] **Step 4:** Run `bun test src/modules/agenthub/sidebar src/smoothnessRatchet.test.ts src/designSystemConformance.test.ts`. Expected: PASS. If the smoothness ratchet counts new keyframes or animations, update its baseline only if the test's own comment says new intentional motion should be counted, and report it.
- [ ] **Step 5:** Commit `agenthub: session avatar with state on its face`.

### Task 12: The session row

**Files:** Modify `frontend/src/modules/agenthub/sidebar/AgentSessionCard.tsx` (whole view :136-316), `sidebar/AgentSessionSidebar.test.tsx` (card assertions).

New props (replacing `subagents`, `narrow`):
`session, selected, attention?, ask: string | null, folder: string | null, pinned, parentTitle?, now, enter?`.
Keep `subagents` in the tooltip summary by passing it through for `cardSummary` only.

- [ ] **Step 1: Update tests first.** In `AgentSessionSidebar.test.tsx`, replace the card assertions that pin the old look:
  - Drop: `agent-card-meta` / `text-caption-11` on meta, `data-meta="Running"` glyphs, `agent-card-bar`, `ring-border ring-inset` on selected, avatar-in-row assertions (`agent-subagent-avatar` inside cards).
  - Add:
    ```tsx
    test("a row is two fixed lines: title + age, then the second line", () => {
      const html = renderSidebar({ sessions: [session("a", { status: "idle", preview: "Done." })] }); // use the file's own render helper
      const card = cardHtml(html, "a");
      expect(card).toContain("h-12.5");
      expect(card).toContain('data-testid="agent-card-title"');
      expect(card).toContain('data-testid="agent-card-line"');
      expect(card).toContain(">Done.<");
      expect(card).toContain('data-slot="session-avatar"');
    });
    test("a waiting row says what it waits for, in the warning tone", () => {
      // render with a pending permission titled "Allow migrate up?" for session a (use the helper that injects attention/ask)
      expect(cardHtml(html, "a")).toMatch(/agent-card-line[^>]*text-status-warning[^>]*>[^<]*Allow migrate up\?/);
    });
    test("selected is a raised ground, no ring", () => {
      expect(cardHtml(html, "a")).toContain("bg-surface-raised");
      expect(cardHtml(html, "a")).not.toContain("ring-inset");
    });
    ```
    Keep every other card test (`aria-current`, `agent-card-description`, `agent-card-age`, `Actions for …`, `after:inset-0`, `focus-visible:after:ring-2`, no nested buttons).
- [ ] **Step 2:** `bun test src/modules/agenthub/sidebar`. Expected: FAIL on the new assertions.
- [ ] **Step 3: Implement** the row in `AgentSessionCard.tsx`. Keep the structure: div card, stretched open button, hover menu, context menu, tooltip, `useRisingEdge` flash.
  - Root classes:
    ```ts
    "group/card relative flex h-12.5 items-center gap-2.75 rounded-xl px-2 transition-colors duration-(--motion-fast) [--avatar-ground:var(--background-app)]",
    selected ? "bg-surface-raised [--avatar-ground:var(--surface-raised)]" : "hover:bg-state-soft focus-within:bg-state-soft",
    entering && "agent-card-enter",
    flash && "agent-attention-flash",
    ```
    `[--avatar-ground:…]` is an arbitrary property. If `shadow-no-arbitrary-values` rejects it, set `style={{ "--avatar-ground": selected ? "var(--surface-raised)" : "var(--background-app)" } as React.CSSProperties}` on the root instead.
  - Delete `agent-card-bar` (the avatar dot replaces it).
  - Avatar: `<SessionAvatar vendor={session.agentType} state={avatarState} />`, where:
    ```ts
    const kind = attentionKind(session, attention);
    const avatarState: AvatarState = kind ? "waiting" : session.status === "error" ? "error" : session.status === "running" ? "running" : null;
    ```
  - Text block (`min-w-0 flex-1`):
    ```tsx
    <div className="flex items-baseline gap-2">
      {/* the existing open button with agent-card-title inside; title classes: */}
      {/* "block truncate text-body-13", tone: selected||running||waiting → text-text-primary; idle → text-text-secondary; closed → text-text-tertiary */}
      {age ? <span data-testid="agent-card-age" aria-hidden="true" className="relative z-10 shrink-0 text-caption-11 text-text-tertiary tabular-nums transition-opacity duration-(--motion-fast) group-hover/card:opacity-0 group-focus-within/card:opacity-0">{age}</span> : null}
    </div>
    <span data-testid="agent-card-line" className={cn("mt-0.5 block truncate text-body-12",
      line.tone === "warning" ? "text-status-warning" : line.tone === "danger" ? "text-status-danger" : "text-text-tertiary")}>
      {line.folder ? <span className="opacity-70">{line.folder} · </span> : null}{line.text}
    </span>
    ```
    with `const line = secondLine({ session, ask, folder });`.
    - When `line.text` and `line.folder` are both empty, still render the span with a non-breaking space (` `) so the row keeps its two-line rhythm.
    - `text-body-12` must exist; check `index.css` `@utility text-body-12`. Otherwise use `text-caption-11`.
  - Hover menu: keep it, positioned `absolute top-1.5 right-1.5`.
  - Remove `MetaIcon`, `IndicatorIcon`, `AgentSubagentAvatars` and the pin/child glyphs from the row. Pinned and child status stay in the tooltip summary and in `cardSummary` (unchanged).
- [ ] **Step 4:** Run `bun test src/modules/agenthub && bunx tsc --noEmit -p . && bunx eslint src/modules/agenthub/sidebar`. Expected: PASS.
- [ ] **Step 5:** Commit `agenthub: one quiet row per session — face, title, where it got to`.

### Task 13: Group chrome, stack row, list motion

**Files:** Modify `frontend/src/modules/agenthub/sidebar/AgentSessionGroup.tsx` (header :213-284, `StackRow` :78-109, list containers :286-307), `sidebar/AgentSessionSidebar.test.tsx`.

- [ ] **Step 1: Update tests.**
  - Stack: assert `agent-stack-toggle` contains `>17 older sessions<` when given 22 sessions, has `border-b` hairline markup (`data-testid="agent-stack-rule"`), and does **not** contain `bg-surface-raised/50`.
  - Group label: `agent-group-name` has `text-caption-11 text-text-tertiary`, and `agent-group-count` is a `<sup>`.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - Header (keep the toggle button, ContextMenu, `+` button and `WaitingBadge`). Restyle:
    - remove the `GroupIcon` and the chevron from the default state; show the chevron only on hover/focus (`opacity-0 group-hover/header:opacity-100`);
    - name `text-caption-11 text-text-tertiary`, or `text-text-secondary` when it contains the active session;
    - count `<sup data-testid="agent-group-count" className="ml-0.5 text-caption-11 text-text-tertiary/70">{n}</sup>`;
    - padding `px-2 pt-4 pb-1.5`.
  - `StackRow`:
    ```tsx
    <button type="button" data-sidebar-nav="" data-testid="agent-stack-toggle" aria-expanded={open} aria-controls={controlsId} onClick={onToggle}
      className="flex h-8 w-full min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 text-left text-body-12 text-text-tertiary outline-none hover:text-text-secondary focus-visible:ring-2 focus-visible:ring-border-focus/50">
      <span className="shrink-0">{label}</span>
      <span aria-hidden="true" data-testid="agent-stack-rule" className="h-px min-w-4 flex-1 bg-border-subtle" />
      <ChevronDown aria-hidden="true" className={cn("size-3.5 shrink-0 transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none", open && "rotate-180")} />
    </button>
    ```
    and delete the two peek spans and the outer `relative mt-0.5 mb-1.5` wrapper margins (keep `mt-0.5`).
  - Lists: put the shown list and the stacked list each in a `div role="list"` with `ref` from
    ```ts
    const [shownRef] = useAutoAnimate<HTMLDivElement>({ duration: MOTION_BASE * 1000, easing: EASE_OUT_CSS });
    ```
    and the same for stacked, imported from `@formkit/auto-animate/react` and `@/lib/motion`.
    - Remove the `entering`/`previous` bookkeeping only if auto-animate now handles enters. Keep `enter` if the focus-restore layout effect depends on it.
    - auto-animate respects reduced motion.
  - Pass `ask`, `folder` and `attention` through to `AgentSessionCard` (Task 12 props).
    - `folder`: when `group.workspace && group.workspace.paths.length > 1`, use `projectLabel(session.projectPath)`; otherwise `null`.
    - `ask` comes from a new group prop `asks: Record<string, string>` (`asks[session.id] ?? null`).
- [ ] **Step 4:** Run `bun test src/modules/agenthub && bunx tsc --noEmit -p .`. Expected: PASS.
- [ ] **Step 5:** Commit `agenthub: quieter group labels, a hairline stack, animated moves`.

### Task 14: Needs-you group and workspace folding

**Files:** Modify `frontend/src/modules/agenthub/sidebar/AgentSessionSidebar.tsx` (props :29-65, groups :146-157, render), `frontend/src/modules/agenthub/AgentHubSidebar.tsx` (:78-225), `sidebar/AgentSessionSidebar.test.tsx`.

- [ ] **Step 1: Failing tests.**
  ```tsx
  test("sessions that need you are drawn once, on top", () => {
    // sessions: a(idle), b(error), c(running + pending permission "Allow?"), all in workspace 1
    const html = renderSidebar({ sessions, attention: { c: { permissions: 1, questions: 0 } }, asks: { c: "Allow?" }, currentWorkspaceId: "1" });
    expect(html).toContain('data-testid="agent-needs-you"');
    const needs = html.slice(html.indexOf('data-testid="agent-needs-you"'), html.indexOf('data-testid="agent-session-group"'));
    expect(cardIdsIn(needs)).toEqual(["b", "c"]);       // newest first per fixture updatedAt
    expect(cardIds(html).filter((id) => id === "c").length).toBe(1);
  });
  test("other workspaces fold by default", () => {
    const html = renderSidebar({ sessions: [inWs("x", "2")], workspaces: [ws("1"), ws("2")], currentWorkspaceId: "1" });
    expect(html).toMatch(/data-group-key="workspace:2"[^>]*data-collapsed="true"/);
  });
  test("no needs-you section when nothing waits", () => {
    expect(renderSidebar({ sessions: [session("a", { status: "idle" })] })).not.toContain("agent-needs-you");
  });
  ```
  Write the small fixture helpers (`inWs`, `ws`, `cardIdsIn`) next to the file's existing ones.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - `AgentSessionSidebar` new props: `asks?: Record<string, string>`, `currentWorkspaceId?: string | null`, `expandedKeys?: string[]`, `onToggleExpanded?: (key: string) => void`.
  - Compute:
    ```ts
    const waiting = React.useMemo(() => needsYou(allSessions, attention), [allSessions, attention]);
    const waitingIds = React.useMemo(() => new Set(waiting.map((x) => x.id)), [waiting]);
    const rest = React.useMemo(() => allSessions.filter((x) => !waitingIds.has(x.id)), [allSessions, waitingIds]);
    const groups = groupSessions(rest, scratchRoot, workspaces, drawnWhenHidingClosed(hideClosed, activeSessionId));
    ```
  - Render, before the groups, when `waiting.length`:
    ```tsx
    <section data-testid="agent-needs-you" aria-label="Needs you" className="flex flex-col">
      <div className="px-2 pt-1 pb-1.5 text-caption-11 text-text-tertiary">Needs you<sup className="ml-0.5 text-text-tertiary/70">{waiting.length}</sup></div>
      <div role="list" ref={needsRef} className="flex flex-col gap-0.5">{waiting.map((session) => <AgentSessionCard key={session.id} … ask={asks[session.id] ?? null} folder={null} />)}</div>
    </section>
    ```
    Give `needsRef` auto-animate like Task 13. Use the same `SessionActions` context; cards there behave exactly like grouped ones.
  - Each group's `collapsed` becomes `groupCollapsed(group.key, currentWorkspaceId ?? null, collapsedSet, expandedSet)`. Its toggle calls `onToggleCollapsed` for the current workspace's group and `onToggleExpanded` for every other group (exactly the branch `groupCollapsed` uses).
  - A collapsed non-current group's header shows `● N running` when any of its sessions is running: `<span className="ml-auto flex items-center gap-1 text-caption-11 text-text-tertiary"><span className="size-1.5 rounded-full bg-text-primary" />{n} running</span>`.
  - Header title: replace `SidebarSection label="Sessions"` with the reference-style header. Keep both existing action buttons and their test ids:
    ```tsx
    <div className="flex items-center gap-2 px-2 pt-1.5 pb-3">
      <h2 className="flex-1 text-title-14 text-text-primary">Agents</h2>
      {/* existing agent-tree-toggle-closed ghost icon button */}
      <Button data-testid="agent-tree-new" aria-label="New session" size="xs" className="rounded-full" onClick={onNewSession}><Plus aria-hidden="true" />New</Button>
    </div>
    ```
    Check that `text-title-14` exists (`index.css`); otherwise use `text-body-14-medium`. Use `Button`'s real size names.
  - `AgentHubSidebar.tsx`:
    ```ts
    const asks = useMemo(() => askBySession(pendingQueue, elicitationQueue), [pendingQueue, elicitationQueue]);
    const currentWorkspaceId = useWorkspaceSwitcherStore((s) => s.currentId);
    const expanded = useAgentPinsStore((s) => s.expanded);
    const toggleExpanded = useAgentPinsStore((s) => s.toggleExpanded);
    ```
    Pass these down. `askBySession` goes in `sidebar/model.ts` (with a test):
    ```ts
    /** The first thing each session asks: a permission's title wins over a question's message. */
    export function askBySession(permissions: readonly Pick<PendingPermission, "sessionId" | "title">[], questions: readonly Pick<PendingElicitation, "sessionId" | "message">[]): Record<string, string> {
      const out: Record<string, string> = {};
      for (const q of questions) if (q.message && !out[q.sessionId]) out[q.sessionId] = q.message;
      for (const p of permissions) if (p.title) out[p.sessionId] = p.title;
      return out;
    }
    ```
    Add a test where `{ sessionId: "a", title: "Allow?" }` plus `{ sessionId: "a", message: "Which?" }` gives `{ a: "Allow?" }`, and a question alone gives its message.
    - Read the switcher store with a narrow selector (`useWorkspaceSwitcherStore((s) => s.currentId)`, ratchet 6.1).
- [ ] **Step 4:** Run `bun test src/modules/agenthub src/storeSubscriptionRatchet.test.ts && bunx tsc --noEmit -p .`. Expected: PASS.
- [ ] **Step 5:** Commit `agenthub: a needs-you group across workspaces; other workspaces fold`.

### Task 15: Summary model

**Files:** Create `frontend/src/modules/agenthub/summary/summaryModel.ts`, `summary/summaryModel.test.ts`.

- [ ] **Step 1: Failing tests:**
  ```ts
  import { describe, expect, test } from "bun:test";
  import { latestPlan, sessionChanges, toolSources, linkHosts } from "./summaryModel";

  const item = (x: object) => x as never;

  describe("latestPlan", () => {
    test("the last plan item in the session wins", () => {
      const items = [item({ kind: "plan", plan: [{ content: "a", status: "completed" }] }), item({ kind: "agent_message_chunk", text: "x" }), item({ kind: "plan", plan: [{ content: "b", status: "in_progress" }] })];
      expect(latestPlan(items)).toEqual([{ content: "b", status: "in_progress" }]);
    });
    test("none", () => { expect(latestPlan([])).toEqual([]); });
  });

  describe("sessionChanges", () => {
    test("sums per path across checkpoints and keeps the newest checkpoint for the diff", () => {
      const index = new Map([
        ["p1", { id: "c1", createdAt: "2026-10-13T10:00:00Z", status: "done", changed: [{ path: "a.go", added: 2, removed: 1, binary: false }] }],
        ["p2", { id: "c2", createdAt: "2026-10-13T10:05:00Z", status: "done", changed: [{ path: "a.go", added: 3, removed: 0, binary: false }, { path: "b.go", added: 1, removed: 0, binary: false }] }],
      ]) as never;
      expect(sessionChanges(index)).toEqual([
        { path: "a.go", added: 5, removed: 1, binary: false, checkpointId: "c2" },
        { path: "b.go", added: 1, removed: 0, binary: false, checkpointId: "c2" },
      ]);
    });
  });

  describe("toolSources", () => {
    test("counts DevKit data tools by module, either name form", () => {
      const items = [
        item({ kind: "tool_call", toolCall: { name: "mcp__devkit__db_query" } }),
        item({ kind: "tool_call", toolCall: { name: "db.explain" } }),
        item({ kind: "tool_call", toolCall: { name: "kafka.peekMessages" } }),
        item({ kind: "tool_call", toolCall: { name: "Read" } }),
      ];
      expect(toolSources(items)).toEqual([{ module: "database", label: "Database", count: 2 }, { module: "kafka", label: "Kafka", count: 1 }]);
    });
  });

  describe("linkHosts", () => {
    test("hosts from user messages and fetch tool inputs, deduplicated, first seen first", () => {
      const items = [
        item({ kind: "user_message_chunk", text: "see https://grafana.local/d/auth and https://grafana.local/x" }),
        item({ kind: "tool_call", toolCall: { kind: "fetch", rawInput: { url: "https://docs.example.com/a" } } }),
      ];
      expect(linkHosts(items)).toEqual([{ host: "grafana.local", url: "https://grafana.local/d/auth" }, { host: "docs.example.com", url: "https://docs.example.com/a" }]);
    });
  });
  ```
  Before writing, read `modules/agenthub/types.ts`:
  - the `TranscriptItem` shape (where the plan, text and tool call live, and the exact `kind` strings, e.g. `"plan"`, `"tool_call"`, `UpdateKind` constants);
  - `ChangedFile` / `CheckpointSummary` field names.

  Shape the fixtures to the real types, and keep the asserted outputs as above.
- [ ] **Step 2:** `bun test src/modules/agenthub/summary`. Expected: FAIL.
- [ ] **Step 3: Implement** `summaryModel.ts`:
  ```ts
  import type { CheckpointIndex } from "../checkpoints";
  import type { PlanEntry, TranscriptItem } from "../types";

  export function latestPlan(items: readonly TranscriptItem[]): PlanEntry[] {
    for (let i = items.length - 1; i >= 0; i -= 1) {
      const it = items[i];
      if (it.kind === "plan" && it.plan) return it.plan;
    }
    return [];
  }

  export type SessionChange = { path: string; added: number; removed: number; binary: boolean; checkpointId: string };

  /** Every file the session touched, its lines summed over turns; the diff opens from the newest checkpoint that touched it. */
  export function sessionChanges(index: CheckpointIndex): SessionChange[] {
    const records = [...index.values()].filter((r) => r.changed?.length).sort((a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt));
    const byPath = new Map<string, SessionChange>();
    for (const record of records) {
      for (const file of record.changed ?? []) {
        const prev = byPath.get(file.path);
        byPath.set(file.path, {
          path: file.path,
          added: (prev?.added ?? 0) + file.added,
          removed: (prev?.removed ?? 0) + file.removed,
          binary: Boolean(prev?.binary || file.binary),
          checkpointId: record.id,
        });
      }
    }
    return [...byPath.values()];
  }

  const DATA_MODULES: Record<string, string> = { db: "Database", kafka: "Kafka", ssh: "SSH" };
  const WIRE = "mcp__devkit__";

  function devkitPrefix(name: string): string | null {
    const plain = name.startsWith(WIRE) ? name.slice(WIRE.length).split("_")[0] : name.split(".")[0];
    return plain in DATA_MODULES ? plain : null;
  }

  export type ToolSource = { module: string; label: string; count: number };

  export function toolSources(items: readonly TranscriptItem[]): ToolSource[] {
    const counts = new Map<string, number>();
    for (const it of items) {
      const name = it.toolCall?.name;
      if (!name) continue;
      const prefix = devkitPrefix(name);
      if (prefix) counts.set(prefix, (counts.get(prefix) ?? 0) + 1);
    }
    return [...counts].map(([prefix, count]) => ({ module: prefix === "db" ? "database" : prefix, label: DATA_MODULES[prefix], count }));
  }

  const URL_RE = /https?:\/\/[^\s<>()"'`]+/g;

  export type LinkHost = { host: string; url: string };

  export function linkHosts(items: readonly TranscriptItem[]): LinkHost[] {
    const seen = new Map<string, string>();
    const add = (raw: string) => {
      try {
        const u = new URL(raw);
        if (!seen.has(u.host)) seen.set(u.host, raw);
      } catch { /* not a URL */ }
    };
    for (const it of items) {
      if (it.kind === "user_message_chunk" && typeof it.text === "string") for (const m of it.text.matchAll(URL_RE)) add(m[0]);
      const input = it.toolCall?.rawInput as { url?: unknown } | undefined;
      if (it.toolCall?.kind === "fetch" && typeof input?.url === "string") add(input.url);
    }
    return [...seen].map(([host, url]) => ({ host, url }));
  }
  ```
  Field accessors (`it.toolCall`, `it.text`, `it.plan`, the user message kind string) must match the real `TranscriptItem`. Fix the code, not the assertions. The `module` value for Database must equal that module's manifest id (check `modules/database/manifest.ts`).
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit `agenthub: derive the session summary from transcript and checkpoints`.

### Task 16: The summary card

**Files:** Create `frontend/src/modules/agenthub/summary/SessionSummary.tsx`, `summary/SessionSummary.test.tsx`.

Props:
```ts
type SessionSummaryProps = {
  sessionId: string;
  items: readonly TranscriptItem[];
  checkpoints: CheckpointIndex;
  usage: Usage | null | undefined;
  turnCount: number;
  live: boolean;
  onOpenFile?: (path: string, line?: number) => void;
};
```
Exports:
- `SessionSummaryCard` (the docked card);
- `SessionSummaryButton` (the collapsed corner button + popover);
- `SessionSummaryBody` (the shared sections, used by both).

- [ ] **Step 1: Failing tests** (`renderToString`; the documents query needs a `QueryClientProvider`; reuse a test helper if one exists, else create a `new QueryClient()` in the test):
  ```tsx
  test("renders only the sections that have something", () => {
    const html = render(<SessionSummaryBody sessionId="s" items={[planItem]} checkpoints={EMPTY_CHECKPOINTS} usage={null} turnCount={0} live={false} />);
    expect(html).toContain('data-testid="summary-plan"');
    expect(html).not.toContain('data-testid="summary-changes"');
    expect(html).not.toContain('data-testid="summary-context"');
  });
  test("plan rows say done / now / pending", () => { /* assert data-state="completed|in_progress|pending" on summary-plan-row */ });
  test("a section shows five then 'See all (N)'", () => { /* 7 changed files → 5 summary-change-row + text 'See all (7)' */ });
  test("the docked card is a labelled section", () => {
    const html = render(<SessionSummaryCard {...props} />);
    expect(html).toContain('aria-label="Session summary"');
  });
  test("nothing at all renders nothing", () => {
    expect(render(<SessionSummaryCard sessionId="s" items={[]} checkpoints={EMPTY_CHECKPOINTS} usage={null} turnCount={0} live={false} />)).toBe("");
  });
  ```
- [ ] **Step 2:** `bun test src/modules/agenthub/summary`. Expected: FAIL.
- [ ] **Step 3: Implement.**

  Data:
  ```ts
  const plan = React.useMemo(() => latestPlan(items), [items]);
  const changes = React.useMemo(() => sessionChanges(checkpoints), [checkpoints]);
  const tools = React.useMemo(() => toolSources(items), [items]);
  const links = React.useMemo(() => linkHosts(items), [items]);
  const docs = useQuery({ queryKey: ["agenthub", "documents", sessionId], queryFn: () => agentHubListDocuments(sessionId), enabled: Boolean(sessionId), staleTime: 0 }).data ?? [];
  const tracked = useAgentSessionsStore((s) => s.subagents[sessionId]);
  const subagents = React.useMemo(() => sessionRunningSubagents([...items], tracked, live), [items, tracked, live]);
  ```

  Sections, in order. Each section is `<section data-testid="summary-<name>">` with a header row:
  ```tsx
  <h4 className="flex items-center px-3.5 pt-2 pb-1 text-body-13 text-text-secondary">{title}<sup className="ml-0.5 text-caption-11 text-text-tertiary/70">{count}</sup></h4>
  ```
  Item rows are `flex h-7.5 items-center gap-2.5 px-3.5 text-body-13 text-text-primary`; clickable rows are `<button>` with `hover:bg-state-soft`. Sections are separated by `border-t border-border-subtle`.
  - **Plan** (`summary-plan`, title "Plan", count `done/total`): each row `summary-plan-row data-state={entry.status ?? "pending"}`. A 14px circle: completed is filled `bg-text-primary` with a check; in_progress has a `ring-1.5 ring-text-primary`; pending has `ring-1.5 ring-text-tertiary`. Text is `text-text-tertiary` when completed. The status colour change uses `transition-colors duration-(--motion-fast)`.
  - **Changes** (`summary-changes`, "Changes", count = files): row `summary-change-row`: `FileCode` icon, path truncated from the left (`direction: rtl` trick is not allowed; use the existing `truncate` plus the full path in `title`), `+a` in `text-status-success`, `−r` in `text-status-danger`, "binary" when binary. Clicking opens a `Dialog` holding `<ChangedFileRow checkpointId={c.checkpointId} file={…ChangedFile with kind/source from the checkpoint…} overlap={null} loadDiff={loadCheckpointFileDiff} defaultOpen />`. Add `defaultOpen?: boolean` to `ChangedFileRow` (`AgentTurnChanges.tsx:147`); it initialises `useState(defaultOpen ?? false)`, and when true the diff is loaded on mount by the same effect.
  - **Sources** (`summary-sources`, "Sources", count = docs + tools + links):
    - PDFs: `FileText` icon in `text-status-danger`, name, `N pages`.
    - Tools: `Database` / `Radio` / `Terminal` icons, label, `N calls`; clicking calls `dispatchIntent` to open that module (`openModule` helper or `useWorkbenchTabsStore.getState().setActiveModule(module)`, whichever the codebase uses for "go to module"; find it via `grep -rn "setActiveModule\|activateModule" frontend/src/workbench`).
    - Links: `Globe` icon, host; clicking calls `openExternal(url)`.
  - **Subagents** (`summary-subagents`, "Subagents"): name and state word (`subagentStateWord` from `sidebar/model.ts`); clicking calls `useSubagentPanel.getState().show(sessionId, subagent.id)`.
  - **Context** (`summary-context`, "Context", only when `usage?.size`): a 4px meter `h-1 rounded-full bg-state-soft` with inner `bg-text-primary` width `%`, then `text-caption-11 text-text-tertiary` with `{pct}% · {turnCount} turns · {amount}` (reuse `shortCount` and the currency formatting from `AgentContextMeter`).
  - **Collapsing:** at most 5 rows per section; then a `summary-see-all` button `See all (N)` toggles a local `expanded` state. Use `AnimatedCollapse` for the extra rows.
  - **Motion:** new rows get auto-animate on each section list (180ms).

  `SessionSummaryBody` returns `null` when every section is empty. Callers check that via the same derivations, through a small exported `hasSummary(...)`.

  Wrappers:
  ```tsx
  export function SessionSummaryCard(props: SessionSummaryProps) {
    if (!hasSummary(props)) return null;
    return (
      <section aria-label="Session summary" data-testid="session-summary-card"
        className="session-summary-enter pointer-events-auto max-h-full w-70 overflow-y-auto overscroll-contain rounded-2xl bg-surface-base py-1.5 shadow-canvas-popover ring-1 ring-border-subtle">
        <SessionSummaryBody {...props} />
      </section>
    );
  }

  export function SessionSummaryButton(props: SessionSummaryProps) {
    if (!hasSummary(props)) return null;
    return (
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label="Session summary" data-testid="session-summary-button" className="rounded-full bg-surface-base shadow-canvas-chip ring-1 ring-border-subtle">
            <PanelRight aria-hidden="true" />
          </Button>
        </PopoverTrigger>
        <PopoverContent side="bottom" align="end" className="w-70 p-0 py-1.5">
          <SessionSummaryBody {...props} />
        </PopoverContent>
      </Popover>
    );
  }
  ```
  - `hasSummary` must be cheap: reuse the memoised derivations by lifting them into a `useSessionSummaryData(props)` hook used by both wrappers and the body. Do not run them twice per render.
  - `index.css`:
    ```css
    /* The summary card arrives once with its session view (it stays mounted across tab switches). */
    .session-summary-enter { transition: opacity var(--motion-base) var(--ease-out-strong), translate var(--motion-base) var(--ease-out-strong); @starting-style { opacity: 0; translate: 0 8px; } }
    ```
    Add `.session-summary-enter { transition: none; }` to the reduced-motion block.
  - `w-70` = 280px, `h-7.5` = 30px. Use the nearest valid scale step if the linter objects.
- [ ] **Step 4:** Run `bun test src/modules/agenthub && bunx tsc --noEmit -p . && bunx eslint src/modules/agenthub/summary`. Expected: PASS.
- [ ] **Step 5:** Commit `agenthub: session summary card — plan, changes, sources, subagents, context`.

### Task 17: Wide layout — column, docking, card placement, tray

**Files:**
- Modify `surfaces/TranscriptStream.tsx` (props :19-49, column :593-596, viewport :574-592).
- Modify `surfaces/AgentHubContent.tsx` (conversation :391-398, transcript :432-446, composer region :455-459).
- Modify `surfaces/composer/ComposerContextBar.tsx` (direct chips).
- Tests: `surfaces/composerLayout.test.tsx` (:115-120), `designSystemConformance.test.ts` comment only.

- [ ] **Step 1: Update tests.** In `composerLayout.test.tsx`, change "shares the transcript's reading column and padding":
  ```tsx
  expect(region).toContain("px-6");
  expect(region).toContain("max-w-190");
  expect(region).toContain("@6xl/agent-conversation:pr-80");
  ```
  Add:
  ```tsx
  test("the summary docks on wide conversations and folds to a button on narrow ones", () => {
    const html = renderContent({ session, items: [planItem] }); // the file's existing helper
    expect(html).toMatch(/data-testid="session-summary-dock"[^>]*hidden[^>]*@6xl\/agent-conversation:block/);
    expect(html).toMatch(/data-testid="session-summary-corner"[^>]*@6xl\/agent-conversation:hidden/);
  });
  test("the rail keeps the 720 column", () => {
    expect(renderRail({…})).toContain("max-w-180");
  });
  ```
- [ ] **Step 2:** Run `bun test src/modules/agenthub/surfaces/composerLayout.test.tsx`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - `TranscriptStream`:
    - new optional props `columnClassName?: string` (default `"max-w-180"`) and `viewportClassName?: string`;
    - the column becomes `cn("relative mx-auto w-full", columnClassName, !virtualized && "flex flex-col gap-6")`;
    - the viewport becomes `cn("size-full px-6 [&>div]:block!", viewportClassName)`;
    - update its doc comment: "The column is max-w-180 by default (the rail); the Hub passes a wider one".
  - `AgentHubContent`:
    - Conversation container: `agent-conversation` is already a size container, but unnamed. Name it in `index.css`, which already has `container-name: agent-conversation`. Tailwind's `@6xl/agent-conversation:` variant targets it.
    - Transcript: pass `columnClassName="max-w-190"` and `viewportClassName="@6xl/agent-conversation:pr-80"`, so the column centres in the space left of the card.
    - Composer region: `className="composer-region-cap flex min-h-0 w-full shrink-0 flex-col px-6 pb-3 @6xl/agent-conversation:pr-80"`; the inner column `max-w-180` becomes `max-w-190`.
    - Card placement, inside the conversation container after the composer region:
      ```tsx
      {session && !draft ? (
        <>
          <div data-testid="session-summary-dock" className="pointer-events-none absolute top-4 right-4 bottom-4 z-10 hidden @6xl/agent-conversation:block">
            <SessionSummaryCard sessionId={session.id} items={items} checkpoints={checkpoints} usage={usage} turnCount={turnCount} live={streaming} onOpenFile={onOpenFile} />
          </div>
          <div data-testid="session-summary-corner" className="absolute top-3 right-3 z-10 @6xl/agent-conversation:hidden">
            <SessionSummaryButton sessionId={session.id} items={items} checkpoints={checkpoints} usage={usage} turnCount={turnCount} live={streaming} onOpenFile={onOpenFile} />
          </div>
        </>
      ) : null}
      ```
      Lift the inline `turnCount` computation (L510–519) into a `const turnCount = React.useMemo(...)` above, and use it for both the composer and the summary.
    - Both wrappers render: CSS decides which is visible. Both are the same tree, but `useSessionSummaryData` runs per wrapper. Acceptable: it is memoised on `items`/`checkpoints` identity and costs O(items) once per publish. If the profiler shows otherwise, render one wrapper chosen by a `useContainerWidth` hook later.
  - `ComposerContextBar`: give the direct chip whose key is `"subagents"` an extra class `@6xl/agent-conversation:hidden`. Pass it through `TrayChip`'s `className`, or add one if missing. The rail is never ≥ @6xl, so it keeps the chip.
  - `designSystemConformance.test.ts` L46–47 comment: change "720 column is max-w-180" to mention `max-w-190` for the Hub and `max-w-180` for the rail. Comment only.
- [ ] **Step 4:** Run `bun test src/modules/agenthub src/designSystemConformance.test.ts && bunx tsc --noEmit -p . && cd frontend && bun run build`. Expected: PASS. Then confirm the variant compiled: `grep -c "agent-conversation" dist/assets/*.css` should be ≥ 3. If Tailwind did not emit `@container agent-conversation (width >= 72rem)`, the named-container syntax is wrong; try `@6xl/agent-conversation` vs `@min-6xl/agent-conversation` per the Tailwind version in `package.json` and fix.
- [ ] **Step 5:** Commit `agenthub: a wider conversation that makes room for the session summary`.

### Task 18: Transcript details — attachment cards and the fold line

**Files:** Modify `surfaces/AgentTurn.tsx` (prompt attachments :158-169, work header :191-195), `transcript/AgentFileChip.tsx` (render `detail` in the button branch too), `surfaces/AgentTurn.test.tsx`.

- [ ] **Step 1: Failing tests:**
  ```tsx
  test("attachments are cards above the prompt", () => {
    const html = renderTurn(turnWithAttachment({ name: "spec-auth-v3.pdf", kind: "document", pages: 12 }));
    expect(html).toContain('data-testid="turn-attachment-card"');
    expect(html).toContain(">spec-auth-v3.pdf<");
    expect(html).toContain(">PDF · 12 pages<");
  });
  test("the work line keeps 'Worked for' with a hairline and a chevron", () => {
    const html = renderTurn(finishedTurn);
    expect(html).toMatch(/data-testid="turn-work"[\s\S]*Worked for 4m 7s[\s\S]*border-b border-border-subtle/);
  });
  ```
  Use the existing fixtures/helpers in `AgentTurn.test.tsx` (it already asserts "Worked for 4m 7s").
- [ ] **Step 2:** Run `bun test src/modules/agenthub/surfaces/AgentTurn.test.tsx`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - Attachments (keep `data-testid="turn-prompt-attachments"` on the wrapper, `flex flex-wrap justify-end gap-2`). Each attachment:
    ```tsx
    <div data-testid="turn-attachment-card" className="flex items-center gap-2.5 rounded-xl bg-surface-tinted py-2 pr-3.5 pl-2">
      <span className="flex size-9 items-center justify-center rounded-lg bg-surface-base">
        {attachment.kind === "document" ? <FileText className="size-4 text-status-danger" aria-hidden="true" /> : attachment.kind === "image" ? <Image className="size-4 text-text-secondary" aria-hidden="true" /> : <File className="size-4 text-text-secondary" aria-hidden="true" />}
      </span>
      <span className="flex min-w-0 flex-col">
        <span className="max-w-60 truncate text-body-13 text-text-primary">{attachment.name}</span>
        <span className="text-body-12 text-text-tertiary">{attachmentDetail(attachment)}</span>
      </span>
    </div>
    ```
    with
    ```ts
    function attachmentDetail(a: PromptAttachment): string {
      if (a.kind === "document") return a.pages ? `PDF · ${a.pages} pages` : "PDF";
      if (a.kind === "image") return "Image";
      return a.name.includes(".") ? a.name.slice(a.name.lastIndexOf(".") + 1).toUpperCase() : "File";
    }
    ```
    Attachments are display-only in Phase 1. `PromptAttachment` carries no openable path.
  - Work header (L191–195): keep `SwapText` and `turn-work`. Add a trailing `ChevronRight` (`size-3.5`) after the text in the same line, and keep `border-b border-border-subtle pb-2`. When the turn is finished, the line stays "Worked for …". No other text change: the "Worked for" copy is pinned by `turns.test.ts`.
  - `AgentFileChip` button branch: render `detail` like the span branch does. This fixes the existing bug where the button branch drops it.
- [ ] **Step 4:** Run `bun test src/modules/agenthub && bunx tsc --noEmit -p .`. Expected: PASS.
- [ ] **Step 5:** Commit `agenthub: attachment cards on the prompt; a quieter work line`.

### Task 19: Docs and verification

**Files:** Modify `docs/superpowers/specs/2026-10-13-agent-hub-wide-layout-design.md` ("Spec deviations" section), `AGENTS.md` (Agent Hub bullet list).

- [ ] **Step 1: Spec.** Append a section "Decisions made while planning Phase 1" listing the deviations from this plan's header.
- [ ] **Step 2: AGENTS.md.** In "Agent Hub", add one bullet:
  > **The rail and the main view are a quiet list and a column with a summary card**
  > - Rows are 50px: vendor face with its state on it, title, and `AgentSession.preview`, which Go writes at turn end. MCP blanks the preview.
  > - "Needs you" lists waiting/failed sessions across workspaces.
  > - Groups other than the title-bar workspace fold by default (`agentPins.expanded`).
  > - At `@6xl/agent-conversation` the session summary card docks top-right (`summary/`) and the column (`max-w-190`) moves left. Below that size a corner button opens it.
  > - Links open via `bridge/browser.ts` `openExternal`; inline-code file refs (`transcript/fileRef.ts`) open in CodeGraph.
- [ ] **Step 3: Full checks.**
  - `cd frontend && bun test 2>&1 | tail -3`: 0 fail.
  - `bun run build`: succeeds, budget passes.
  - `bunx eslint src 2>&1 | grep problems`: 47, the pre-existing count.
  - `go test ./internal/... ./cmd/mocksyncserver/ -count=1 2>&1 | grep -v "^ok" | head`: no FAIL lines (cgo warnings are fine).
  - `go vet -tags server ./internal/...`.
- [ ] **Step 4: Visual check.**
  - Run server mode with a scratch `HOME`/`XDG_CONFIG_HOME`, as done for the theme work. Agent Hub sessions fail in server mode, so seed the sidebar visually through the dev gallery if needed (`?gallery`), or skip live sessions and screenshot the empty-state rail, the Settings, and a gallery Agent Hub section.
  - At window widths 1280, 1600 and 2200, light and dark, capture:
    - the rail;
    - the docked versus corner summary (the gallery fixture or a mocked session);
    - a link and a file ref in prose.
  - Save the shots to the scratchpad and report them.
- [ ] **Step 5:** Commit `docs: Agent Hub wide layout, phase 1`.
