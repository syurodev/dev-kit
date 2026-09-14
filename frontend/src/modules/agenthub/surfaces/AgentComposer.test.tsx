import { describe, expect, it } from "bun:test";
import { renderToString as renderRaw } from "react-dom/server";

import { TooltipProvider } from "@/components/ui/tooltip";
import type { ConfigOption, SessionMode } from "../types";
import {
  AgentComposer,
  composerModeChoices,
  handleComposerKeyDown,
  isPermissionBypassChoice,
  requestComposerChoice,
  selectComposerMode,
  shouldCancelComposerOnEscape,
} from "./AgentComposer";

const renderToString = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);
const base = { value: "", onChange: () => {}, onSend: () => {}, onCancel: () => {} };

describe("composer controls", () => {
  it("names the icon-only settings button", () => {
    const html = renderToString(
      <AgentComposer {...base} configOptions={[{ id: "model", options: [{ value: "sonnet" }] }]} onSetConfigOption={() => {}} />,
    );
    const tag = html.match(/<button[^>]*data-testid="composer-config"[^>]*>/)?.[0] ?? "";
    expect(tag).toContain('aria-label="Agent settings"');
  });

  it("removes an attached file with a named 28px button, not by clicking the 24px chip", () => {
    const html = renderToString(
      <AgentComposer {...base} attachedFiles={["src/app.ts"]} onRemoveAttachment={() => {}} />,
    );
    const remove = html.match(/<button[^>]*aria-label="Remove src\/app.ts"[^>]*>/)?.[0] ?? "";
    expect(remove).toContain("control-height-sm");
    // The chip itself is a label now, not a control.
    expect(html).not.toMatch(/<button[^>]*data-testid="agent-file-chip"/);
  });
});

describe("confirming a permission bypass", () => {
  it("recognises a bypass by what the choice means, wherever it is listed", () => {
    expect(isPermissionBypassChoice({ value: "bypassPermissions" })).toBe(true);
    expect(isPermissionBypassChoice({ value: "yolo", name: "Dangerously skip permissions" })).toBe(true);
    expect(isPermissionBypassChoice({ value: "skip-permissions" })).toBe(true);
    // An option that IS the bypass switch: turning it on needs confirming, off does not.
    const option = { id: "dangerously_skip_permissions" };
    expect(isPermissionBypassChoice({ value: "true" }, option)).toBe(true);
    expect(isPermissionBypassChoice({ value: "false" }, option)).toBe(false);
    expect(isPermissionBypassChoice({ value: "off" }, option)).toBe(false);

    expect(isPermissionBypassChoice({ value: "plan" })).toBe(false);
    expect(isPermissionBypassChoice({ value: "acceptEdits", name: "Accept edits" })).toBe(false);
    expect(isPermissionBypassChoice({ value: "sonnet" }, { id: "model", name: "Model" })).toBe(false);
  });

  function choose(isBypass: boolean) {
    const applied: string[] = [];
    let pending: (() => void) | null = null;
    requestComposerChoice(isBypass, () => applied.push("set"), (apply) => {
      pending = apply;
    });
    return { applied, confirm: () => (pending as (() => void) | null)?.(), asked: () => pending !== null };
  }

  it("applies an ordinary choice at once", () => {
    const { applied, asked } = choose(false);
    expect(applied).toEqual(["set"]);
    expect(asked()).toBe(false);
  });

  it("holds a bypass until it is confirmed, and drops it on Cancel", () => {
    const confirmed = choose(true);
    expect(confirmed.asked()).toBe(true);
    expect(confirmed.applied).toEqual([]);
    confirmed.confirm();
    expect(confirmed.applied).toEqual(["set"]);

    // Cancel is never calling the held choice.
    const cancelled = choose(true);
    expect(cancelled.applied).toEqual([]);
  });

  it("does not render the confirmation until something asks for it", () => {
    const html = renderToString(<AgentComposer {...base} mode="default" onSetMode={() => {}} />);
    expect(html).not.toContain("without asking");
  });
});

describe("composer mode list", () => {
  const modeOption: ConfigOption = {
    id: "mode",
    category: "mode",
    currentValue: "plan",
    options: [
      { value: "default", name: "Default" },
      { value: "plan", name: "Plan" },
    ],
  };

  it("offers only the modes the agent advertised, not a built-in list", () => {
    expect(composerModeChoices("default", []).choices).toEqual([{ value: "default" }]);
    expect(composerModeChoices("plan", [modeOption]).choices).toEqual([
      { value: "plan", name: "Plan" },
      { value: "default", name: "Default" },
    ]);
  });

  it("finds the mode option by its ACP category and takes its current value", () => {
    const option: ConfigOption = { ...modeOption, id: "session-kind" };
    const { modeOption: found, current, choices } = composerModeChoices(null, [option]);
    expect(found).toBe(option);
    expect(current).toBe("plan");
    expect(choices.map((choice) => choice.value)).toEqual(["plan", "default"]);
  });

  const advertised: SessionMode[] = [
    { id: "default", name: "Default", description: "Ask before edits" },
    { id: "plan", name: "Plan" },
  ];

  it("lists the session's advertised modes, in the agent's order, over a mode config option", () => {
    const { current, choices } = composerModeChoices("plan", [modeOption], advertised);
    expect(current).toBe("plan");
    expect(choices).toEqual([
      { value: "default", name: "Default", description: "Ask before edits" },
      { value: "plan", name: "Plan" },
    ]);
    const html = renderToString(
      <AgentComposer {...base} mode="default" availableModes={advertised} onSetMode={() => {}} />,
    );
    expect(html).toContain('data-testid="composer-mode"');
    // The trigger names the mode, not its id.
    expect(html).toContain(">Default</span>");
  });

  it("still asks before switching to an advertised mode that skips permissions", () => {
    const modes: SessionMode[] = [
      { id: "default", name: "Default" },
      { id: "bypassPermissions", name: "Bypass Permissions" },
      { id: "yolo", name: "Dangerously skip permissions" },
    ];
    const applied: string[] = [];
    const held: (() => void)[] = [];
    for (const choice of composerModeChoices("default", [], modes).choices) {
      selectComposerMode(choice, (mode) => applied.push(mode), (apply) => held.push(apply));
    }
    expect(applied).toEqual(["default"]);
    expect(held).toHaveLength(2);
    for (const apply of held) apply();
    expect(applied).toEqual(["default", "bypassPermissions", "yolo"]);
  });

  it("has nothing to offer when the agent advertised no mode", () => {
    expect(composerModeChoices(null, [{ id: "model", options: [{ value: "sonnet" }] }]).choices).toEqual([]);
    const html = renderToString(<AgentComposer {...base} onSetMode={() => {}} />);
    expect(html).not.toContain('data-testid="composer-mode"');
  });

  it("shows the picker from a mode config option before any mode update arrived", () => {
    const html = renderToString(<AgentComposer {...base} mode={null} configOptions={[modeOption]} onSetMode={() => {}} />);
    expect(html).toContain('data-testid="composer-mode"');
    expect(html).toContain("plan");
  });
});

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
