import "@/testing/happyDom";
import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { MotionGlobalConfig } from "framer-motion";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import type { TranscriptItem } from "../types";
import { AgentLoadedTools } from "./AgentLoadedTools";
import { EarlierTurnProvider } from "./toolSettle";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
// happy-dom's animations reject when framer-motion cancels one mid-flight; the tests read the
// DOM the disclosure ends in, not the tween.
MotionGlobalConfig.skipAnimations = true;

let seq = 0;
const search = (query: string, status = "completed"): TranscriptItem => ({
  id: `s${++seq}`,
  kind: "tool_call",
  toolCall: { id: `c${seq}`, kind: "other", title: "ToolSearch", name: "ToolSearch", status, rawInput: { query, max_results: 5 } },
});

describe("AgentLoadedTools", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  const toggle = () => container.querySelector<HTMLButtonElement>("[data-testid=loaded-tools-toggle]")!;
  const draw = (node: React.ReactNode) => act(async () => root.render(node));

  test("is one closed row; a single search says no count", async () => {
    await draw(<AgentLoadedTools items={[search("select:mcp__devkit__db_query")]} />);
    expect(toggle().tagName).toBe("BUTTON");
    expect(toggle().getAttribute("type")).toBe("button");
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
    expect(toggle().textContent).toBe("Loaded tools");
    expect(container.querySelector("[data-testid=loaded-tools]")?.getAttribute("data-state")).toBe("collapsed");
    expect(container.textContent).not.toContain("db_query");
  });

  test("says how many searches it folds", async () => {
    await draw(<AgentLoadedTools items={[search("select:A"), search("select:B"), search("select:C")]} />);
    expect(toggle().textContent).toBe("Loaded tools (3)");
  });

  test("opens onto each search: tool names for a select, the words for a keyword query", async () => {
    await draw(<AgentLoadedTools items={[search("select:TaskStop,Monitor"), search("+slack send"), search("select:" + "N".repeat(200))]} />);
    await act(async () => toggle().click());
    expect(toggle().getAttribute("aria-expanded")).toBe("true");
    const rows = Array.from(container.querySelectorAll("[data-testid=loaded-tools-call]")).map((el) => el.textContent);
    expect(rows).toHaveLength(3);
    expect(rows[0]).toBe("TaskStop, Monitor");
    expect(rows[1]).toBe("+slack send");
    // Clipped, not 200 characters wide.
    expect(rows[2]!.length).toBeLessThanOrEqual(80);
    await act(async () => toggle().click());
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
    expect(container.querySelector("[data-testid=loaded-tools]")?.getAttribute("data-state")).toBe("collapsed");
  });

  test("a search with no readable query still lists as a row", async () => {
    const blank = search("");
    await draw(<AgentLoadedTools items={[blank]} defaultOpen />);
    expect(container.querySelectorAll("[data-testid=loaded-tools-call]")).toHaveLength(1);
  });

  test("shows loading while one is still going, and not once its turn is over", async () => {
    await draw(<AgentLoadedTools items={[search("select:A"), search("select:B", "in_progress")]} />);
    expect(toggle().textContent).toContain("Loading tools (2)");
    await draw(
      <EarlierTurnProvider earlier>
        <AgentLoadedTools items={[search("select:A"), search("select:B", "in_progress")]} />
      </EarlierTurnProvider>,
    );
    expect(toggle().textContent).toBe("Loaded tools (2)");
  });

  test("is quiet: the row itself is muted text with a muted icon, and nothing carries a colour of its own", async () => {
    await draw(<AgentLoadedTools items={[search("select:A")]} defaultOpen />);
    expect(toggle().className).toContain("text-text-tertiary");
    expect(toggle().className).not.toMatch(/text-status-|text-text-primary/);
    expect(toggle().querySelector("svg")?.getAttribute("class")).toContain("text-icon-secondary");
    expect(container.innerHTML).not.toContain("text-status-");
    expect(container.innerHTML).not.toMatch(/style="[^"]*(color|background)/);
  });

  describe("a call that has not ended", () => {
    const rows = (...status: string[]) => status.map((s, i) => search(`select:T${i}`, s));
    const label = () => toggle().textContent;

    test.each(["pending", "in_progress", "awaiting_permission"])("%s: loading, while its session is busy", async (status) => {
      await draw(
        <EarlierTurnProvider earlier={false} quiet={false}>
          <AgentLoadedTools items={rows("completed", status)} />
        </EarlierTurnProvider>,
      );
      expect(label()).toBe("Loading tools (2)");
    });

    test.each(["pending", "in_progress", "awaiting_permission"])("%s: loaded, once the session is quiet", async (status) => {
      await draw(
        <EarlierTurnProvider earlier={false} quiet>
          <AgentLoadedTools items={rows("completed", status)} />
        </EarlierTurnProvider>,
      );
      expect(label()).toBe("Loaded tools (2)");
    });

    test("a subagent's search is not settled by a quiet session, only by its turn ending", async () => {
      const sub = [{ ...search("select:A", "in_progress"), origin: "a1" }];
      await draw(
        <EarlierTurnProvider earlier={false} quiet>
          <AgentLoadedTools items={sub} />
        </EarlierTurnProvider>,
      );
      expect(label()).toBe("Loading tools");
      await draw(
        <EarlierTurnProvider earlier quiet>
          <AgentLoadedTools items={sub} />
        </EarlierTurnProvider>,
      );
      expect(label()).toBe("Loaded tools");
    });
  });
});
