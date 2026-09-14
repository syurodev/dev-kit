import { describe, expect, it } from "bun:test";

import { handleComposerKeyDown, shouldCancelComposerOnEscape } from "./AgentComposer";

// No DOM in bun test here, so the textarea's onKeyDown is driven through the
// handler it delegates to, with the fields React's KeyboardEvent carries.
function keyDown(key: string, init: { shiftKey?: boolean; keyCode?: number; isComposing?: boolean } = {}) {
  let prevented = false;
  return {
    event: {
      key,
      shiftKey: init.shiftKey ?? false,
      keyCode: init.keyCode ?? 0,
      nativeEvent: { isComposing: init.isComposing ?? false },
      preventDefault: () => {
        prevented = true;
      },
    },
    prevented: () => prevented,
  };
}

function run(key: string, init: Parameters<typeof keyDown>[1], streaming: boolean) {
  const calls: string[] = [];
  const k = keyDown(key, init);
  handleComposerKeyDown(k.event, {
    streaming,
    onSend: () => calls.push("send"),
    onCancel: () => calls.push("cancel"),
  });
  return { calls, prevented: k.prevented() };
}

describe("composer keys under an IME", () => {
  it("sends on Enter and cancels a running turn on Escape when not composing", () => {
    expect(run("Enter", {}, false)).toEqual({ calls: ["send"], prevented: true });
    expect(run("Escape", {}, true)).toEqual({ calls: ["cancel"], prevented: true });
    expect(run("Enter", { shiftKey: true }, false).calls).toEqual([]);
  });

  it("leaves Enter and Escape to the IME while a composition is open", () => {
    // Enter picks the candidate, Escape drops it — neither is the composer's.
    expect(run("Enter", { isComposing: true }, false)).toEqual({ calls: [], prevented: false });
    expect(run("Escape", { isComposing: true }, true)).toEqual({ calls: [], prevented: false });
  });

  it("treats WebKit's keyCode 229 as composing", () => {
    // WKWebView reports the keydown that commits a composition as 229 with
    // isComposing already false.
    expect(run("Enter", { keyCode: 229 }, false).calls).toEqual([]);
    expect(run("Escape", { keyCode: 229 }, true).calls).toEqual([]);
  });

  it("does not cancel on Escape while composing", () => {
    expect(shouldCancelComposerOnEscape("Escape", true, true)).toBe(false);
    expect(shouldCancelComposerOnEscape("Escape", true, false)).toBe(true);
  });
});
