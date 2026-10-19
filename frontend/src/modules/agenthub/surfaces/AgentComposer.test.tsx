import { describe, expect, it } from "bun:test";
import { renderToString as renderRaw } from "react-dom/server";

import { TooltipProvider } from "@/components/ui/tooltip";
import { chipToken, fileChip } from "../composerDraft";
import type { ConfigOption, SessionMode } from "../types";
import {
  AgentComposer,
  composerDefaultPlaceholder,
  composerModeChoices,
  handleComposerKeyDown,
  heldChoice,
  isPermissionBypassChoice,
  requestComposerChoice,
  selectComposerMode,
  shouldCancelComposerOnEscape,
} from "./AgentComposer";

const renderToString = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);
const base = { value: "", onChange: () => {}, onSend: () => {}, onCancel: () => {} };

it("uses a human prompt for unnamed sessions while preserving explicit instructions", () => {
  expect(composerDefaultPlaceholder(undefined, false, "Session da4c04e2")).toBe("Ask agent…");
  expect(composerDefaultPlaceholder(undefined, true, "Session da4c04e2")).toBe("Agent is working…");
  expect(composerDefaultPlaceholder("Send to reopen this session", false, "Session da4c04e2")).toBe("Send to reopen this session");
  expect(composerDefaultPlaceholder(undefined, false, "Refund worker")).toBe("Ask Refund worker…");
});

describe("confirming a bypass picked in a menu", () => {
  it("opens the confirmation only once the menu has closed, and only once", () => {
    const held = heldChoice();
    const opened: Array<() => void> = [];
    let applied = 0;

    selectComposerMode({ value: "bypassPermissions" }, () => applied++, held.hold);
    expect(opened).toHaveLength(0); // still inside onSelect: the menu is only starting to close

    held.release((apply) => opened.push(apply)); // the menu's close finished
    expect(opened).toHaveLength(1);
    held.release((apply) => opened.push(apply)); // the next close of that menu
    expect(opened).toHaveLength(1);

    expect(applied).toBe(0);
    opened[0]!();
    expect(applied).toBe(1);
  });

  it("opens nothing when the menu closes without a bypass", () => {
    const held = heldChoice();
    const opened: Array<() => void> = [];
    let applied = 0;

    selectComposerMode({ value: "plan" }, () => applied++, held.hold);
    held.release((apply) => opened.push(apply));
    expect(opened).toHaveLength(0);
    expect(applied).toBe(1);
  });
});

describe("a paused queue", () => {
  const queued = [{ id: "q1", text: "next", images: [], attachments: [], refs: [] }];

  it("says why it paused when the refusal said", () => {
    const html = renderToString(
      <AgentComposer {...base} queuedPrompts={queued} queuePaused queuePausedReason="Claude is not signed in" onResumeQueued={() => {}} />,
    );
    const row = html.match(/<div[^>]*data-testid="composer-queue-paused"[^>]*>.*?<\/button>/)?.[0] ?? "";
    expect(row).toContain("Paused: Claude is not signed in");
    expect(row).toContain('title="Claude is not signed in"');
    expect(row).toContain('aria-label="Resume queued prompts"');
  });

  it("says only Paused when there is no reason", () => {
    const html = renderToString(<AgentComposer {...base} queuedPrompts={queued} queuePaused />);
    const row = html.match(/<div[^>]*data-testid="composer-queue-paused"[^>]*>.*?<\/div>/)?.[0] ?? "";
    expect(row).toContain(">Paused</span>");
    expect(row).not.toContain("Paused:");
  });
});

describe("composer controls", () => {
  it("names the model button and shows the current model on its face", () => {
    const html = renderToString(
      <AgentComposer {...base} configOptions={[{ id: "model", options: [{ value: "sonnet" }] }]} onSetConfigOption={() => {}} />,
    );
    const tag = html.match(/<button[^>]*data-testid="composer-config"[^>]*>/)?.[0] ?? "";
    // No model chosen yet: the button says so rather than naming nothing.
    expect(tag).toContain('aria-label="Model and effort: Select model"');
    const chosen = renderToString(
      <AgentComposer
        {...base}
        configOptions={[
          { id: "model", name: "Model", currentValue: "sonnet", options: [{ value: "opus", name: "Opus" }, { value: "sonnet", name: "Sonnet 4" }] },
        ]}
        onSetConfigOption={() => {}}
      />,
    );
    expect(chosen.match(/<button[^>]*data-testid="composer-config"[^>]*>.*?<\/button>/)?.[0]).toContain("Sonnet 4");
    expect(chosen).toContain('aria-label="Model and effort: Sonnet 4"');
  });

  it("draws an attached file as a chip in the field, removed by a named button, not by clicking the chip", () => {
    const html = renderToString(<AgentComposer {...base} value={`compare ${chipToken(fileChip("/work/src/app.ts"))} please`} />);
    const chip = html.match(/<span[^>]*data-testid="attachment-chip"[^>]*>/)?.[0] ?? "";
    expect(chip).toContain('data-kind="file"');
    const remove = html.match(/<button[^>]*aria-label="Remove app.ts"[^>]*>/)?.[0] ?? "";
    expect(remove).not.toBe("");
    // The chip itself is a label, not a control.
    expect(html).not.toMatch(/<button[^>]*data-testid="attachment-chip"/);
    // No strip above the field: the draft holds the file, once.
    expect(html).not.toContain('data-testid="composer-attachments"');
    expect(html.match(/data-testid="attachment-chip"/g)).toHaveLength(1);
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
    expect(html).toContain('aria-label="Permission mode: Default"');
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

describe("the slash-command list over the field", () => {
  const commands = [
    { name: "compact", description: "summarise the turn" },
    { name: "review", input: { hint: "path" } },
    { name: "init" },
  ];

  it("offers the commands the typed name matches, and nothing else", () => {
    const html = renderToString(<AgentComposer {...base} value="/c" commands={commands} onSelectCommand={() => {}} />);
    expect(html).toContain('data-testid="composer-command-list"');
    expect(html).toContain("/compact");
    expect(html).toContain("summarise the turn");
    expect(html).not.toContain("/review");
  });

  it("says what a command expects after its name", () => {
    const html = renderToString(<AgentComposer {...base} value="/rev" commands={commands} onSelectCommand={() => {}} />);
    // The hint is the argument's, so the field says what to type next.
    expect(html).toContain("path");
  });

  it("stays shut when the prompt is not naming a command", () => {
    for (const value of ["", "hello", "/compact "]) {
      const html = renderToString(<AgentComposer {...base} value={value} commands={commands} onSelectCommand={() => {}} />);
      expect(html).not.toContain('data-testid="composer-command-list"');
    }
  });

  it("stays shut when nothing matches, rather than showing an empty box", () => {
    const html = renderToString(<AgentComposer {...base} value="/zzz" commands={commands} onSelectCommand={() => {}} />);
    expect(html).not.toContain('data-testid="composer-command-list"');
  });
});

function runWithList(
  key: string,
  init: Parameters<typeof keyDown>[1],
  { streaming = false, open = true }: { streaming?: boolean; open?: boolean } = {},
) {
  const calls: string[] = [];
  const k = keyDown(key, init);
  handleComposerKeyDown(k.event, {
    streaming,
    onSend: () => calls.push("send"),
    onCancel: () => calls.push("cancel"),
    commandList: {
      open,
      onMove: (delta) => calls.push(`move:${delta}`),
      onAccept: () => calls.push("accept"),
      onClose: () => calls.push("close"),
    },
  });
  return { calls, prevented: k.prevented() };
}

describe("composer keys while the slash-command list is open", () => {
  it("picks the command instead of sending the half-typed name", () => {
    expect(runWithList("Enter", {})).toEqual({ calls: ["accept"], prevented: true });
    expect(runWithList("Tab", {})).toEqual({ calls: ["accept"], prevented: true });
  });

  it("closes the list instead of cancelling the turn running behind it", () => {
    // The turn is streaming: without the list taking Escape first, one keypress
    // would kill a turn the user only meant to stop naming a command for.
    expect(runWithList("Escape", {}, { streaming: true })).toEqual({ calls: ["close"], prevented: true });
  });

  it("moves the highlight with the arrows", () => {
    expect(runWithList("ArrowDown", {})).toEqual({ calls: ["move:1"], prevented: true });
    expect(runWithList("ArrowUp", {})).toEqual({ calls: ["move:-1"], prevented: true });
  });

  it("leaves Shift+Enter as a newline", () => {
    expect(runWithList("Enter", { shiftKey: true })).toEqual({ calls: [], prevented: false });
  });

  it("still leaves every key to an open IME composition", () => {
    // The list is open and a candidate is being picked: Enter is the IME's.
    expect(runWithList("Enter", { isComposing: true })).toEqual({ calls: [], prevented: false });
    expect(runWithList("Enter", { keyCode: 229 })).toEqual({ calls: [], prevented: false });
  });

  it("hands the keys back once the list is closed", () => {
    expect(runWithList("Enter", {}, { open: false })).toEqual({ calls: ["send"], prevented: true });
    expect(runWithList("Escape", {}, { streaming: true, open: false })).toEqual({
      calls: ["cancel"],
      prevented: true,
    });
  });
});
