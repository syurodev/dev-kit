import "@/testing/happyDom";
import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from "bun:test";
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";

import { ComposerEditorBoundary } from "./ComposerEditorBoundary";

type Render = { epoch: number; tick: number };

/**
 * The owner of the boundary, in miniature: it holds the draft and the editor
 * epoch the way AgentComposer does, with a stand-in editor that draws the draft
 * it was built from and throws whenever `throwsWhen` says so for that render.
 * (A pure function of the render, not a counter: React replays a render that
 * threw once before it hands the error to a boundary.)
 */
describe("ComposerEditorBoundary", () => {
  let container: HTMLDivElement;
  let root: Root;
  let consoleError: ReturnType<typeof spyOn>;
  let throwsWhen: (render: Render) => boolean;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    consoleError = spyOn(console, "error").mockImplementation(mock(() => {}));
    throwsWhen = () => false;
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    consoleError.mockRestore();
  });

  function FakeEditor({ draft, epoch, tick }: { draft: string } & Render) {
    if (throwsWhen({ epoch, tick })) throw new Error("slate-desync");
    return <div data-testid="editor">{draft}</div>;
  }

  function Owner({ initial, healthyAfterMs }: { initial: string; healthyAfterMs?: number }) {
    const [draft, setDraft] = useState(initial);
    const [epoch, setEpoch] = useState(0);
    const [tick, setTick] = useState(0);
    return (
      <div>
        <span data-testid="sibling">send, model, mode</span>
        <button type="button" data-testid="poke" onClick={() => setTick((n) => n + 1)}>
          poke
        </button>
        <ComposerEditorBoundary
          epoch={epoch}
          draft={draft}
          onRemount={() => setEpoch((n) => n + 1)}
          onResetPrompt={() => {
            setDraft("");
            setEpoch((n) => n + 1);
          }}
          healthyAfterMs={healthyAfterMs}
        >
          <FakeEditor draft={draft} epoch={epoch} tick={tick} />
        </ComposerEditorBoundary>
      </div>
    );
  }

  const text = (id: string) => container.querySelector(`[data-testid="${id}"]`)?.textContent ?? null;
  const failed = () => container.querySelector('[data-testid="composer-editor-failed"]') !== null;
  const click = (el: Element | null | undefined) => act(async () => (el as HTMLButtonElement).click());
  const resetButton = () => Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Reset prompt");
  const poke = () => click(container.querySelector('[data-testid="poke"]'));

  test("a throw inside the editor leaves the rest mounted and the draft in a fresh editor", async () => {
    throwsWhen = ({ epoch }) => epoch === 0; // the first editor is the broken one
    await act(async () => root.render(<Owner initial="/get /get /" />));
    expect(text("sibling")).toBe("send, model, mode");
    expect(text("editor")).toBe("/get /get /");
    expect(failed()).toBe(false);
  });

  test("an error that comes straight back stops retrying and offers Reset prompt", async () => {
    throwsWhen = () => true;
    await act(async () => root.render(<Owner initial={"line one\ntwo"} />));
    expect(text("sibling")).toBe("send, model, mode");
    expect(container.querySelector('[data-testid="editor"]')).toBeNull();
    expect(failed()).toBe(true);
    expect(text("composer-editor-failed-draft")).toBe("line one\ntwo");
    expect(resetButton()).toBeDefined();
  });

  test("Reset prompt clears the draft and brings a working editor back", async () => {
    let broken = true;
    throwsWhen = () => broken;
    await act(async () => root.render(<Owner initial="keep me" />));
    expect(failed()).toBe(true);
    broken = false; // whatever broke the editor went with the draft
    await click(resetButton());
    expect(failed()).toBe(false);
    expect(text("editor")).toBe("");
    expect(text("sibling")).toBe("send, model, mode");
  });

  test("an editor that has run soundly for a while gets a retry again for a later, separate error", async () => {
    throwsWhen = ({ epoch, tick }) => epoch === 0 || (epoch === 1 && tick === 1);
    await act(async () => root.render(<Owner initial="draft" healthyAfterMs={5} />));
    expect(text("editor")).toBe("draft"); // epoch 1, after the first throw
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30));
    });
    await poke(); // epoch 1 throws on its next render, long after mounting
    expect(failed()).toBe(false);
    expect(text("editor")).toBe("draft"); // epoch 2
  });

  test("without that time a second throw is the same problem and stops", async () => {
    throwsWhen = ({ epoch, tick }) => epoch === 0 || (epoch === 1 && tick === 1);
    await act(async () => root.render(<Owner initial="draft" healthyAfterMs={60_000} />));
    expect(text("editor")).toBe("draft");
    await poke();
    expect(failed()).toBe(true);
    expect(text("sibling")).toBe("send, model, mode");
  });
});
