import "@/testing/happyDom";
import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MotionGlobalConfig } from "framer-motion";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import "@/modules/git/manifest";
import type { ToolCall, TranscriptItem } from "../types";
import { AgentRunningCommand } from "./AgentRunningCommand";
import { AgentToolActivity } from "./AgentToolActivity";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
MotionGlobalConfig.skipAnimations = true;

/*
 * A call's status changes while the component stays mounted: pending, then running,
 * then done. A server render draws each state in a fresh tree, so it can never see
 * a component call a different number of hooks from one render to the next; React
 * throws on exactly that (2026-10-30: the window showed "Something went wrong" while
 * agents ran). These tests re-render ONE mounted tree through the states.
 */
const qc = new QueryClient();

describe("the settle hook runs on every render", () => {
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
  const draw = (node: React.ReactNode) => act(async () => root.render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>));

  const push = (status: string): ToolCall => ({ id: "c1", name: "mcp__devkit__git_push", kind: "other", title: "git push", status, rawInput: { repo: "r" } });

  test("a tool activity card goes through every status without a hook-count change", async () => {
    for (const status of ["pending", "in_progress", "completed", "in_progress", "failed", "pending", "completed"]) {
      await draw(<AgentToolActivity call={push(status)} sessionId="s1" origin="a1" />);
      expect(container.textContent).not.toBe("");
    }
  });

  test("a running-command clock goes through running and finished without a hook-count change", async () => {
    const item = (status: string): TranscriptItem =>
      ({
        id: "t1",
        kind: "tool_call",
        sessionId: "s1",
        createdAt: new Date(Date.now() - 60_000).toISOString(),
        toolCall: { id: "c2", name: "Bash", kind: "execute", title: "sleep 60", status, rawInput: { command: "sleep 60" } },
      }) as unknown as TranscriptItem;
    for (const status of ["pending", "in_progress", "completed", "in_progress", "failed"]) {
      await draw(<AgentRunningCommand item={item(status)} />);
    }
    expect(true).toBe(true);
  });
});
