import "@/testing/happyDom";
import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { MotionGlobalConfig } from "framer-motion";
import * as React from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import { TooltipProvider } from "@/components/ui/tooltip";
import { panelCloseMs } from "@/lib/panelMotion";
import { PaneSlotsRow } from "./pane-slots";
import { SplitPanelProvider, useSplitPanelActions } from "./split-panel";
import type { BoundPanelActions } from "./splitPanelModel";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
// happy-dom's animations reject when framer-motion cancels one mid-flight; these tests read the
// DOM the shell ends in, not the tween.
MotionGlobalConfig.skipAnimations = true;

let actions: BoundPanelActions<React.ReactNode>;
function Opener() {
  actions = useSplitPanelActions();
  return null;
}

let mounts = 0;
function Body({ label }: { label: string }) {
  React.useEffect(() => void mounts++, []);
  return <p data-testid="body">{label}</p>;
}

const MODULE_LABEL = "Panel"; // the shell owner's own label: no module of that id is registered

let container: HTMLDivElement;
let root: Root;
beforeEach(async () => {
  mounts = 0;
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () =>
    root.render(
      <SplitPanelProvider>
        <TooltipProvider>
          <Opener />
          <PaneSlotsRow activeModuleId={null}>
            <div />
          </PaneSlotsRow>
        </TooltipProvider>
      </SplitPanelProvider>,
    ),
  );
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
});

const open = (content: React.ReactNode, opts: Parameters<typeof actions.openPanel>[1]) => act(async () => actions.openPanel(content, opts));
const close = (discard = false) => act(async () => actions.closePanel({ discard }));
const settle = () => act(async () => void (await new Promise((resolve) => setTimeout(resolve, panelCloseMs(false) + 150))));
const shown = (selector: string) => document.body.querySelector<HTMLElement>(selector);
const labelled = (prefix: string) => [...document.body.querySelectorAll<HTMLElement>(`[aria-label^='${prefix}']`)].map((el) => el.getAttribute("aria-label"));
const header = { title: "Two", titleHint: "Two, in full", status: <span data-testid="hs">s</span>, actions: <span data-testid="ha">a</span> };

describe("the shell's panes, mounted", () => {
  test("a headerless pane draws exactly the module's label, and Close <label>", async () => {
    await open(<Body label="one" />, { onClose: () => {}, key: "one" });
    expect(shown("[data-testid=body]")?.textContent).toBe("one");
    expect(labelled("Close")).toEqual([`Close ${MODULE_LABEL}`]);
    expect(labelled("Move")).toContain(`Move ${MODULE_LABEL} to…`);
    expect(shown("[data-testid=hs]")).toBeNull();
  });

  test("a headed pane draws the header's title, status and actions; the shell's buttons keep the module's label", async () => {
    await open(<Body label="two" />, { onClose: () => {}, key: "two", header });
    expect(document.body.textContent).toContain("Two");
    expect(shown("[title='Two, in full']")?.textContent).toBe("Two");
    expect(shown("[data-testid=hs]")).not.toBeNull();
    expect(shown("[data-testid=ha]")).not.toBeNull();
    expect(labelled("Close")).toEqual([`Close ${MODULE_LABEL}`]);
    expect(labelled("Move")).toContain(`Move ${MODULE_LABEL} to…`);
  });

  test("the same key with a new header updates the row and does not remount the body", async () => {
    await open(<Body label="two" />, { onClose: () => {}, key: "two", header });
    await open(<Body label="two" />, { onClose: () => {}, key: "two", header: { ...header, title: "Two (done)" } });
    expect(shown("[title='Two, in full']")?.textContent).toBe("Two (done)");
    expect(mounts).toBe(1);
  });

  test("switching to a headerless pane under another key shows no stale header at once", async () => {
    await open(<Body label="two" />, { onClose: () => {}, key: "two", header });
    await open(<Body label="three" />, { onClose: () => {}, key: "three" });
    expect(shown("[title='Two, in full']")).toBeNull();
    expect(shown("[data-testid=hs]")).toBeNull();
    expect(labelled("Close")).toEqual([`Close ${MODULE_LABEL}`]);
    expect(shown("[data-testid=body]")?.textContent).toBe("three");
  });

  test("closing with discard keeps the header and the body while the slot closes, then drops both", async () => {
    await open(<Body label="two" />, { onClose: () => {}, key: "two", header });
    await close(true);
    expect(shown("[title='Two, in full']")).not.toBeNull();
    expect(shown("[data-testid=body]")).not.toBeNull();
    await settle();
    expect(shown("[title='Two, in full']")).toBeNull();
    expect(shown("[data-testid=body]")).toBeNull();
  });
});
