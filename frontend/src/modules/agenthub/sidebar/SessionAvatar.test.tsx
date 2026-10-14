import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { SessionAvatar } from "./SessionAvatar";

describe("SessionAvatar", () => {
  test("running draws a spinning arc, nothing else", () => {
    const html = renderToString(<SessionAvatar vendor="claude" state="running" />);
    expect(html).toContain("agent-avatar-spin");
    expect(html).not.toContain("agent-avatar-badge");
  });
  test("waiting draws a breathing warning dot", () => {
    const html = renderToString(<SessionAvatar vendor="codex" state="waiting" />);
    expect(html).toContain("agent-avatar-badge");
    expect(html).toContain("bg-status-warning");
    expect(html).toContain("agent-avatar-breathe");
  });
  test("error is a still danger dot; idle is just the face", () => {
    const error = renderToString(<SessionAvatar vendor="claude" state="error" />);
    expect(error).toContain("bg-status-danger");
    expect(error).not.toContain("agent-avatar-breathe");
    const idle = renderToString(<SessionAvatar vendor="claude" state={null} />);
    expect(idle).not.toContain("agent-avatar-badge");
    expect(idle).not.toContain("agent-avatar-spin");
  });
});
