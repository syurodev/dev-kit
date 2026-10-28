import "@/testing/happyDom";
import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from "bun:test";
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";

import { TooltipProvider } from "@/components/ui/tooltip";

// The real field, made to throw while `failing.on`. The error is a pure
// function of the flag, not a counter: React replays a render that threw once
// before it hands the error to a boundary.
const failing = { on: false };
const realEditor = await import("@/components/ui/editor");
// Held apart: the module's live binding is the mock once it is installed.
const RealEditor = realEditor.Editor;
mock.module("@/components/ui/editor", () => ({
  ...realEditor,
  Editor: (props: React.ComponentProps<typeof RealEditor>) => {
    if (failing.on) throw new Error("slate-desync");
    return <RealEditor {...props} />;
  },
}));

const { AgentComposer } = await import("./AgentComposer");

/**
 * AgentComposer itself, mounted: a render error inside its prompt field keeps
 * to the field. The draft lives in the owner (the session's prompt in the
 * store), and a fresh editor is built from it.
 */
describe("AgentComposer when its editor throws", () => {
  let container: HTMLDivElement;
  let root: Root;
  let consoleError: ReturnType<typeof spyOn>;
  let draft: string;
  let sent: number;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    consoleError = spyOn(console, "error").mockImplementation(mock(() => {}));
    failing.on = false;
    draft = "";
    sent = 0;
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    consoleError.mockRestore();
  });

  function Owner({ initial }: { initial: string }) {
    const [value, setValue] = useState(initial);
    draft = value;
    return (
      <TooltipProvider>
        <AgentComposer value={value} onChange={setValue} onSend={() => void sent++} onCancel={() => {}} />
      </TooltipProvider>
    );
  }

  const field = () => container.querySelector('[data-testid="agent-composer-textarea"]');
  const failedPanel = () => container.querySelector('[data-testid="composer-editor-failed"]');
  const send = () => container.querySelector('[data-testid="composer-send-button"]');
  const resetButton = () => Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Reset prompt");

  test("mounts the field with the draft while nothing throws", async () => {
    await act(async () => root.render(<Owner initial="hello world" />));
    expect(field()?.textContent).toContain("hello world");
    expect(failedPanel()).toBeNull();
  });

  test("a throw in the field leaves the send button up and, once the cause is gone, the draft in a fresh field", async () => {
    // Throws on the first editor; the retry's editor is built after it cleared.
    failing.on = true;
    const clearOnCatch = spyOn(console, "error").mockImplementation(() => {
      failing.on = false;
    });
    await act(async () => root.render(<Owner initial="/get /get /" />));
    clearOnCatch.mockRestore();
    expect(send()).not.toBeNull();
    expect(failedPanel()).toBeNull();
    expect(field()?.textContent).toContain("/get /get /");
    expect(draft).toBe("/get /get /");
  });

  test("an error that persists shows Reset prompt, keeps the rest of the composer, and the reset clears the draft", async () => {
    failing.on = true;
    await act(async () => root.render(<Owner initial="keep until asked" />));
    expect(field()).toBeNull();
    expect(failedPanel()?.textContent).toContain("keep until asked");
    expect(send()).not.toBeNull();
    expect(draft).toBe("keep until asked");

    failing.on = false;
    await act(async () => (resetButton() as HTMLButtonElement).click());
    expect(draft).toBe("");
    expect(failedPanel()).toBeNull();
    expect(field()).not.toBeNull();
  });
});
