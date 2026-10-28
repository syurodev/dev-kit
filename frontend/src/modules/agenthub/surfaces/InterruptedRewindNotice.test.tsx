import { describe, expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { InterruptedRewindNoticeView } from "./InterruptedRewindNotice";
import { UndoRewindFilesBody } from "./UndoRewindFilesDialog";
import type { NoticeState } from "../interruptedRewind";
import { RestoreMode, type RestorePreview } from "../../../../bindings/devkit/internal/agenthub/models";

const state = (overrides: Partial<NoticeState> = {}): NoticeState => ({
  canUndo: true,
  sessionRunning: false,
  confirmingKeep: false,
  busy: false,
  restored: false,
  ...overrides,
});

const view = (props: Partial<Parameters<typeof InterruptedRewindNoticeView>[0]> = {}) =>
  renderToStaticMarkup(
    <InterruptedRewindNoticeView item={{ canUndo: true, others: 0 }} state={state()} onAction={() => {}} {...props} />,
  );

const text = (html: string) => html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();

describe("the notice for a rewind that did not finish", () => {
  it("says what is true, in plain words", () => {
    const words = text(view());
    expect(words).toContain("A rewind did not finish");
    expect(words).toContain("may have been put back, or only partly");
    expect(words).toContain("The conversation was not rewound");
    expect(words).toContain("kept a copy of the files as they were before the rewind");
  });

  it("offers to put the files back or to keep them, and nothing that would finish the rewind", () => {
    const html = view();
    expect(html).toContain('data-testid="interrupted-rewind-undo"');
    expect(html).toContain('data-testid="interrupted-rewind-keep"');
    expect(html).toContain("Put the files back as they were");
    expect(html).toContain("Keep the files as they are");
    expect(text(html)).not.toMatch(/retry|finish the rewind|complete the rewind|resume|try again/i);
    expect((html.match(/<button/g) ?? []).length).toBe(2);
  });

  it("offers no undo when no copy is held, and does not say one is kept", () => {
    const html = view({ item: { canUndo: false, others: 0 }, state: state({ canUndo: false }) });
    expect(html).not.toContain("interrupted-rewind-undo");
    expect(text(html)).toContain("cannot be put back from here");
    expect(text(html)).not.toContain("kept a copy");
  });

  it("disables undo while the agent is running and says why", () => {
    const html = view({ state: state({ sessionRunning: true }) });
    expect(html).toMatch(/data-testid="interrupted-rewind-undo"[^>]*disabled|disabled=""[^>]*data-testid="interrupted-rewind-undo"/);
    expect(text(html)).toContain("Available when the agent is not running.");
  });

  it("asks before it closes the way back", () => {
    const html = view({ state: state({ confirmingKeep: true }) });
    expect(text(html)).toContain("does not change any file");
    expect(html).toContain('data-testid="interrupted-rewind-confirmKeep"');
    expect(html).toContain('data-testid="interrupted-rewind-cancelKeep"');
    expect(html).not.toContain("interrupted-rewind-undo");
  });

  it("reports a refused close truthfully and keeps the actions", () => {
    const html = view({ error: "Something went wrong. The notice is still open." });
    expect(html).toContain('role="alert"');
    expect(text(html)).toContain("The notice is still open.");
    expect(html).toContain('data-testid="interrupted-rewind-keep"');
  });

  it("after a restore says what it did and offers to close", () => {
    const html = view({ state: state({ restored: true }), restoredLine: "2 files put back." });
    expect(text(html)).toContain("The last restore: 2 files put back.");
    expect(html).toContain('data-testid="interrupted-rewind-done"');
    expect(html).not.toContain('data-testid="interrupted-rewind-keep"');
  });
});

const preview = (overrides: Partial<RestorePreview> = {}): RestorePreview => ({
  token: "t1",
  sessionId: "s1",
  mode: RestoreMode.RestoreUndoRestore,
  actions: [{ path: "src/a.ts", kind: "write" }, { path: "gone.txt", kind: "delete" }],
  conflicts: [
    { path: "edited.ts", kind: "write", reason: "changed_since", overridable: true },
    { path: "odd.ts", kind: "write", reason: "protected", overridable: false },
  ],
  warnings: ["Another session is running in this project right now (x). Its changes may be overwritten."],
  ...overrides,
});

describe("the preview the person confirms", () => {
  const body = (props: Partial<Parameters<typeof UndoRewindFilesBody>[0]> = {}) =>
    renderToStaticMarkup(
      <UndoRewindFilesBody preview={preview()} forced={new Set()} onToggle={() => {}} outcome="" error="" {...props} />,
    );

  it("lists what will change, what is left alone and why, and the backend's warnings", () => {
    const html = body();
    expect(html).toContain("src/a.ts");
    expect(html).toContain("gone.txt");
    expect((html.match(/data-testid="rewind-action"/g) ?? []).length).toBe(2);
    expect((html.match(/data-testid="rewind-conflict"/g) ?? []).length).toBe(2);
    expect(text(html)).toContain("Changed since this turn");
    expect(text(html)).toContain("Not in any snapshot");
    expect(text(html)).toContain("Another session is running in this project right now");
  });

  it("shows a failed apply as an alert, hides the spent plan and does not say nothing changed", () => {
    const html = body({
      preview: null,
      outcome: "Agent session is busy. DevKit cannot tell whether any file changed before it stopped. Check again to see what is left.",
    });
    expect(html).toContain('data-testid="undo-rewind-outcome"');
    expect(html).toContain('role="alert"');
    expect(html).not.toContain("rewind-action");
    expect(text(html)).not.toMatch(/nothing (was )?changed/i);
  });

  it("shows a failed preview as an error", () => {
    const html = body({ preview: null, error: "Agent session is busy." });
    expect(html).toContain('data-testid="undo-rewind-error"');
  });
});
