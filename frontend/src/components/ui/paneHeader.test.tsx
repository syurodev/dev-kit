import "@/testing/happyDom";
import { afterEach, beforeEach, expect, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { Bot } from "lucide-react";

import { TooltipProvider } from "@/components/ui/tooltip";
import { PaneHeader, SlotsContext, type SlotsCtx } from "./pane-slots";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const PANE = "context:agenthub";
const closed: string[] = [];

const ctx = (headers: SlotsCtx["headers"]): SlotsCtx =>
  ({
    titles: { [PANE]: "Agent Hub" },
    headers,
    focusedPane: null,
    terminalHasModule: false,
    close: (pane: string) => void closed.push(pane),
    moveTo: () => {},
    targets: () => [],
    beginDrag: () => {},
  }) as unknown as SlotsCtx;

let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  closed.length = 0;
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
});

async function draw(headers: SlotsCtx["headers"]) {
  const card = { current: null };
  await act(async () =>
    root.render(
      <TooltipProvider>
        <SlotsContext.Provider value={ctx(headers)}>
          <PaneHeader pane={PANE} cardRef={card} originRef={card} />
        </SlotsContext.Provider>
      </TooltipProvider>,
    ),
  );
}

test("a pane without a header is drawn as before: the module's label, the layout and close buttons", async () => {
  await draw({});
  expect(container.textContent).toContain("Agent Hub");
  expect(container.querySelector("[aria-label='Close Agent Hub']")).not.toBeNull();
  expect(container.querySelector("[aria-label='Move Agent Hub to…']")).not.toBeNull();
});

test("a pane with a header draws it in the one row, in place of the module label", async () => {
  await draw({
    [PANE]: {
      title: "Review whitelist",
      titleHint: "Check the whitelist",
      icon: <Bot data-testid="header-icon" />,
      status: <span data-testid="header-status">running</span>,
      actions: <button type="button" data-testid="header-action">Stop turn</button>,
    },
  });
  // One row, not a header over a header.
  expect(container.querySelectorAll("header")).toHaveLength(0);
  expect(container.textContent).toContain("Review whitelist");
  expect(container.textContent).not.toContain("Agent Hub");
  expect(container.querySelector("[title='Check the whitelist']")?.textContent).toBe("Review whitelist");
  for (const id of ["header-icon", "header-status", "header-action"]) expect(container.querySelector(`[data-testid=${id}]`)).not.toBeNull();
  // The shell's own buttons stay, named for the module as ever: "Close Review whitelist" would read as closing the subagent.
  expect(container.querySelector("[aria-label='Move Agent Hub to…']")).not.toBeNull();
  const close = container.querySelector<HTMLButtonElement>("[aria-label='Close Agent Hub']");
  expect(container.querySelectorAll("[aria-label^='Close']")).toHaveLength(1);
  await act(async () => close!.click());
  expect(closed).toEqual([PANE]);
});

test("status and actions come in order: title, status, actions, then the shell's buttons", async () => {
  await draw({
    [PANE]: {
      title: "T",
      status: <span data-testid="s" />,
      actions: <span data-testid="a" />,
    },
  });
  const order = [...container.querySelectorAll("[data-testid=s], [data-testid=a], [aria-label^='Move Agent Hub to'], [aria-label='Close Agent Hub']")].map(
    (el) => el.getAttribute("data-testid") ?? el.getAttribute("aria-label"),
  );
  expect(order).toEqual(["s", "a", "Move Agent Hub to…", "Close Agent Hub"]);
});

test("a long title truncates and the controls keep their size", async () => {
  await draw({ [PANE]: { title: "x".repeat(200), status: <span />, actions: <span data-testid="a" /> } });
  const title = [...container.querySelectorAll("span")].find((el) => el.textContent === "x".repeat(200))!;
  for (const cls of ["min-w-0", "flex-1", "truncate"]) expect(title.classList.contains(cls)).toBe(true);
});
