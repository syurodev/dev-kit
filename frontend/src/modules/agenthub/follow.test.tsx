import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { TranscriptItem } from "./types";
import { AgentActivityPanel, FOLLOW_FEED_PAGE, activityFeed, feedWindow, latestTarget, revealIntent } from "./follow";
import { useAgentPinsStore } from "@/stores/agentPins";
import { hasEntityPreview, renderEntityPreview } from "@/workbench/toolActivity";
import "@/modules/tasks/manifest";
import "@/modules/git/manifest";

useAgentPinsStore.persist.setOptions({ storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });

const item = (id: string, name: string, status: string, rawInput: unknown, rawOutput?: unknown, kind = "other"): TranscriptItem => ({
  id,
  kind: "tool_call",
  createdAt: new Date(Date.now() - 10_000).toISOString(),
  toolCall: { id, name, kind, status, rawInput, rawOutput },
});

const items: TranscriptItem[] = [
  item("read", "Read", "completed", {}, undefined, "read"),
  item("task", "mcp__devkit__tasks_create", "completed", { title: "Fix login" }, { id: "t1", key: "BE-1", title: "Fix login", status: "todo" }),
  item("commit", "mcp__devkit__git_commit", "completed", { message: "Fix" }, { sha: "abc1234def", short: "abc1234", subject: "Fix" }),
  item("build", "Bash", "in_progress", { command: "npm run build" }, undefined, "execute"),
  item("board", "mcp__devkit__tasks_board_create", "in_progress", { name: "Ops", keyPrefix: "ops" }),
];

describe("following a session", () => {
  it("lists what it changes and what it is still running, newest first — never its reads", () => {
    expect(activityFeed(items).map((i) => i.id)).toEqual(["board", "build", "commit", "task"]);
  });

  it("previews the newest finished change it can open", () => {
    expect(latestTarget(activityFeed(items))).toEqual({
      ref: { module: "git", kind: "commit", id: "abc1234def", label: "abc1234" },
      label: "abc1234",
    });
    expect(latestTarget(activityFeed(items.slice(0, 2)))?.ref).toMatchObject({ module: "tasks", kind: "task", id: "t1" });
    // The live view skips what it cannot draw: the commit is newer, the task is shown.
    expect(latestTarget(activityFeed(items), hasEntityPreview)?.ref).toMatchObject({ module: "tasks", kind: "task", id: "t1" });
  });

  it("embeds a task live, and offers nothing it cannot draw", () => {
    expect(renderEntityPreview({ module: "tasks", kind: "task", id: "t1", label: "" })).not.toBeNull();
    expect(renderEntityPreview({ module: "git", kind: "commit", id: "x", label: "" })).toBeNull();
  });

  it("is per session, and remembered", () => {
    useAgentPinsStore.setState({ following: [] });
    useAgentPinsStore.getState().toggleFollowing("s1");
    expect(useAgentPinsStore.getState().following).toEqual(["s1"]);
    useAgentPinsStore.getState().toggleFollowing("s1");
    expect(useAgentPinsStore.getState().following).toEqual([]);
  });

  it("opens through each module's reveal intent", () => {
    expect(revealIntent("tasks")).toBe("tasks.reveal");
    expect(revealIntent("apiclient")).toBe("api.reveal");
  });

  it("says so when nothing has changed yet", () => {
    const html = renderToString(<AgentActivityPanel sessionId="s-empty" />);
    expect(html).toContain("Nothing changed yet");
  });

  it("draws only the newest cards of a long feed, and counts the rest", () => {
    const feed = Array.from({ length: FOLLOW_FEED_PAGE + 25 }, (_, i) => i);
    const first = feedWindow(feed, FOLLOW_FEED_PAGE);
    expect(first.drawn).toHaveLength(FOLLOW_FEED_PAGE);
    expect(first.drawn[0]).toBe(0);
    expect(first.hidden).toBe(25);
    // One more page reaches the end, and a short feed is handed back as it is.
    const all = feedWindow(feed, FOLLOW_FEED_PAGE * 2);
    expect(all.hidden).toBe(0);
    expect(all.drawn).toBe(feed);
  });
});
