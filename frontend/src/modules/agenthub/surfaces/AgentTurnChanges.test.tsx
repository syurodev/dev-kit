import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { AgentTurnChanges, ChangedFileRow } from "./AgentTurnChanges";
import { AgentTurn } from "./AgentTurn";
import { groupTurns } from "../turns";
import {
  ChangedSource,
  CheckpointKind,
  CheckpointStatus,
  type ChangedFile,
  type CheckpointRecord,
} from "../../../../bindings/devkit/internal/agenthub/models";

function record(overrides: Partial<CheckpointRecord> = {}): CheckpointRecord {
  return {
    id: "cp1",
    sessionId: "s1",
    projectKey: "k",
    promptMessageId: "p1",
    kind: CheckpointKind.CheckpointTurn,
    beforeTree: "aaa",
    afterTree: "bbb",
    status: CheckpointStatus.CheckpointDone,
    outcome: "ok",
    createdAt: "2026-09-17T10:00:00Z",
    ...overrides,
  } as CheckpointRecord;
}

function file(overrides: Partial<ChangedFile> = {}): ChangedFile {
  return {
    path: "src/a.ts",
    kind: "modified",
    added: 2,
    removed: 1,
    binary: false,
    source: ChangedSource.SourceAgent,
    ...overrides,
  } as ChangedFile;
}

describe("AgentTurnChanges", () => {
  it("shows the count and the totals, folded", () => {
    const html = renderToString(
      <AgentTurnChanges
        checkpoint={record({ changed: [file(), file({ path: "b.ts", added: 5, removed: 0 })] })}
      />,
    );
    expect(html).toContain("2 files changed");
    expect(html).toContain("+7");
    expect(html).toContain("−1");
    // Folded: the paths are in the DOM but the disclosure is closed.
    expect(html).toContain('data-state="collapsed"');
  });

  // A turn with no checkpoint and a turn that changed nothing look identical in
  // a zero, and only one of them should be silent.
  it("draws nothing at all when there is nothing to say", () => {
    expect(renderToString(<AgentTurnChanges checkpoint={record({ changed: [] })} />)).toBe("");
    expect(
      renderToString(
        <AgentTurnChanges checkpoint={record({ status: CheckpointStatus.CheckpointFailed, outcome: "unavailable" })} />,
      ),
    ).toBe("");
  });

  // Outside the fold: a warning behind a disclosure is a warning nobody reads.
  it("says a half-recorded turn cannot be undone without needing to be opened", () => {
    const html = renderToString(
      <AgentTurnChanges checkpoint={record({ outcome: "before_missing", changed: [file()] })} />,
    );
    expect(html).toContain('data-testid="turn-changes-partial"');
    expect(html).toContain("cannot be undone");
    expect(html).toContain('data-state="collapsed"');
  });

  it("carries no warning for a turn recorded on both sides", () => {
    const html = renderToString(<AgentTurnChanges checkpoint={record({ changed: [file()] })} />);
    expect(html).not.toContain('data-testid="turn-changes-partial"');
  });
});

// The rows live inside the disclosure, which renders nothing while closed — so
// they are exercised directly, the way AgentTurn.test.tsx renders StepGroup.
describe("a changed file's row", () => {
  it("labels a file nothing accounts for as unattributed, never as the agent's", () => {
    const html = renderToString(
      <ChangedFileRow
        checkpointId="cp1"
        file={file({ path: "by-script.sh", source: ChangedSource.SourceUnattributed })}
        overlap="Another session was running in this project at the same time."
      />,
    );
    expect(html).toContain("Unattributed");
    expect(html).not.toContain(">Agent<");
    // The evidence behind the label, not a disclaimer on every row.
    expect(html).toContain('data-testid="turn-changed-file-overlap"');
    expect(html).toContain("Another session was running");
  });

  it("keeps the overlap note off a file the agent owns", () => {
    const html = renderToString(
      <ChangedFileRow
        checkpointId="cp1"
        file={file({ source: ChangedSource.SourceAgent })}
        overlap="Another session was running in this project at the same time."
      />,
    );
    expect(html).toContain("Agent");
    expect(html).not.toContain('data-testid="turn-changed-file-overlap"');
  });

  it("says Binary instead of line counts for a file git counted none for", () => {
    const html = renderToString(
      <ChangedFileRow
        checkpointId="cp1"
        file={file({ path: "logo.png", binary: true, added: 0, removed: 0 })}
        overlap={null}
      />,
    );
    expect(html).toContain("Binary");
    expect(html).toContain("logo.png");
    expect(html).not.toContain("+0");
  });

  it("offers no drawer for a binary file: there is nothing a diff view could show", () => {
    const html = renderToString(
      <ChangedFileRow
        checkpointId="cp1"
        file={file({ binary: true })}
        overlap={null}
        loadDiff={() => Promise.reject(new Error("must not be called"))}
      />,
    );
    expect(html).toContain("disabled");
  });
});

// The rewind button on a prompt, and what the pencil means once it is there.
//
// Unlike the file half, a conversation rewind needs no checkpoint — the
// message id is the whole requirement — so the button is on any prompt DevKit
// gave an id to. Which of the three rewinds are actually on offer is the
// dialog's question, and is pinned in rewind.test.ts.
describe("the rewind affordance on a prompt", () => {
  function turnWith(checkpoint: CheckpointRecord | undefined, messageId = "p1", onRewind?: () => void) {
    const [turn] = groupTurns([
      { id: "u", kind: "user_message_chunk", text: "do the thing", messageId, createdAt: "2026-09-17T10:00:00Z" },
      { id: "a", kind: "agent_message_chunk", text: "done" },
    ]);
    return renderToString(
      <AgentTurn turn={turn} checkpoint={checkpoint} onRewind={onRewind ?? (() => {})} onEditPrompt={() => {}} />,
    );
  }

  it("is there on a turn that changed files", () => {
    expect(turnWith(record({ promptMessageId: "p1", changed: [file()] }))).toContain('data-testid="message-rewind"');
  });

  // The conversation can be rewound whether or not anything was written to
  // disk: forgetting a message is not a file operation.
  it("is there on a turn with no checkpoint at all", () => {
    expect(turnWith(undefined)).toContain('data-testid="message-rewind"');
  });

  it("is there on a turn whose opening snapshot never finished", () => {
    expect(
      turnWith(record({ promptMessageId: "p1", beforeTree: "", outcome: "before_missing", changed: [file()] })),
    ).toContain('data-testid="message-rewind"');
  });

  // Nothing to name: a prompt with no message id is one no rewind could point
  // at, and the button would open a dialog that could only fail.
  it("is absent on a prompt with no message id", () => {
    expect(turnWith(undefined, "")).not.toContain('data-testid="message-rewind"');
  });

  it("is absent when nothing is listening for it", () => {
    const [turn] = groupTurns([
      { id: "u", kind: "user_message_chunk", text: "do the thing", messageId: "p1" },
    ]);
    expect(renderToString(<AgentTurn turn={turn} onEditPrompt={() => {}} />)).not.toContain(
      'data-testid="message-rewind"',
    );
  });

  // The pencil becomes "edit and run again from here": putting the old text in
  // the composer without taking the conversation back would send it into a
  // session that has already answered it.
  it("keeps the pencil, which now goes through the rewind", () => {
    const html = turnWith(record({ promptMessageId: "p1", changed: [file()] }));
    expect(html).toContain('data-testid="message-edit"');
    expect(html).toContain('data-testid="message-rewind"');
  });
});
