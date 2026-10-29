import "@/testing/happyDom";
import { afterEach, beforeEach, expect, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import { useAgentPinsStore } from "@/stores/agentPins";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { AgentActivityPanel } from "./follow";
import type { TranscriptItem } from "./types";
import "@/modules/git/manifest";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
useAgentPinsStore.persist.setOptions({ storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });

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

const commit = (id: string): TranscriptItem =>
  ({ id, kind: "tool_call", toolCall: { id, name: "git.commit", kind: "other", status: "in_progress", rawInput: { message: id } } }) as TranscriptItem;
const user = (id: string): TranscriptItem => ({ id, kind: "user_message_chunk", text: id }) as TranscriptItem;

test("the follow panel settles a card of an earlier turn and keeps the latest turn's spinning", async () => {
  const items = [user("u1"), commit("old"), user("u2"), commit("new")];
  useAgentSessionsStore.setState({ transcriptsById: { s1: { items } } as never });
  await act(async () => root.render(<AgentActivityPanel sessionId="s1" title="t" onStop={() => {}} />));
  const states = [...container.querySelectorAll("[data-testid=tool-activity]")].map((el) => `${el.textContent?.includes("old") ? "old" : "new"}:${el.getAttribute("data-state")}`);
  expect(states.sort()).toEqual(["new:running", "old:unfinished"]);
});
