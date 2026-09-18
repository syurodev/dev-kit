import { describe, expect, it } from "bun:test";
import { renderToString as renderRaw } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";

import { applySessionUpdate, createInitialTranscriptModel } from "./transcript";
import { AgentSessionFailure } from "./surfaces/AgentSessionFailure";
import { rateLimitDetail, rateLimitLabel } from "./surfaces/AgentContextMeter";
import { failureKey } from "./AgentHubView";

const render = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);

const failure = {
  id: "f1",
  revision: 1,
  category: "access",
  severity: "error",
  title: "Sign in to continue using Claude.",
  reason: "auth_required",
  actions: ["login"],
};

describe("what the vendor says about the session", () => {
  it("keeps the latest failure and the goal as session state", () => {
    let model = createInitialTranscriptModel();
    model = applySessionUpdate(model, { kind: "session_info_update", failure });
    expect(model.failure?.title).toBe(failure.title);
    // A title-only session_info_update is the session being named; it says
    // nothing about the failure and must not clear it.
    model = applySessionUpdate(model, { kind: "session_info_update", title: "Refund worker" });
    expect(model.failure?.title).toBe(failure.title);

    model = applySessionUpdate(model, { kind: "session_info_update", goal: { objective: "a green build" } });
    expect(model.goal?.objective).toBe("a green build");
    model = applySessionUpdate(model, { kind: "session_info_update", goalCleared: true });
    expect(model.goal).toBeNull();
  });

  it("dismisses a failure by id and revision, so a republished one is news again", () => {
    expect(failureKey({ id: "f1", revision: 1 })).toBe("f1:1");
    expect(failureKey({ id: "f1", revision: 2 })).not.toBe(failureKey({ id: "f1", revision: 1 }));
  });

  it("offers the vendor's own way out, and nothing this app cannot do", () => {
    const html = render(
      <AgentSessionFailure
        failure={{ ...failure, actions: ["login", "new_session", "call_support"] }}
        onAction={() => {}}
        onDismiss={() => {}}
      />,
    );
    expect(html).toContain("Sign in to continue using Claude.");
    expect(html).toContain('data-testid="session-failure-login"');
    expect(html).toContain('data-testid="session-failure-new_session"');
    // An action with nothing behind it would be a button that does nothing.
    expect(html).not.toContain("call_support");
  });

  it("tones a warning down: a notice is not a session that stopped", () => {
    const html = render(
      <AgentSessionFailure failure={{ ...failure, severity: "warning", actions: [] }} onAction={() => {}} />,
    );
    expect(html).toContain('data-severity="warning"');
    expect(html).toContain("bg-status-warning");
    expect(html).not.toContain("bg-status-danger");
  });
});

describe("the subscription's rate limit", () => {
  it("says what the status means", () => {
    expect(rateLimitLabel("rejected")).toBe("Usage limit reached");
    expect(rateLimitLabel("allowed_warning")).toBe("Usage limit close");
    expect(rateLimitLabel("allowed")).toBe("Usage limit");
  });

  it("counts a long window in hours and a short one in minutes", () => {
    const now = Date.UTC(2026, 0, 1, 12, 0, 0);
    const inThreeHours = { status: "allowed_warning", resetsAt: (now + 3 * 3_600_000) / 1000, utilization: 82.4 };
    expect(rateLimitDetail(inThreeHours, now)).toBe("82% · resets in 3h");

    const inTwenty = { status: "allowed", resetsAt: (now + 20 * 60_000) / 1000 };
    expect(rateLimitDetail(inTwenty, now)).toBe("resets in 20m");

    // Nothing to say is better than a made-up number.
    expect(rateLimitDetail({ status: "allowed" }, now)).toBe("no detail");
  });
});

describe("steering a running turn", () => {
  const base = { status: "running", text: "wait, use the other API", attachments: 0, images: 0, busy: false, retrying: false };

  it("puts a mid-turn message into the turn", async () => {
    const { shouldSteer } = await import("./AgentHubView");
    expect(shouldSteer(base)).toBe(true);
  });

  it("queues instead when there is nothing to steer into, or files to send", async () => {
    const { shouldSteer } = await import("./AgentHubView");
    // Idle or parked on a permission: the turn is not taking input.
    expect(shouldSteer({ ...base, status: "idle" })).toBe(false);
    expect(shouldSteer({ ...base, status: "awaiting_permission" })).toBe(false);
    // Files belong to a turn of their own.
    expect(shouldSteer({ ...base, attachments: 1 })).toBe(false);
    expect(shouldSteer({ ...base, images: 1 })).toBe(false);
    // A retry replays a whole turn; a send already in flight owns the session.
    expect(shouldSteer({ ...base, retrying: true })).toBe(false);
    expect(shouldSteer({ ...base, busy: true })).toBe(false);
    expect(shouldSteer({ ...base, text: "" })).toBe(false);
  });
});
