import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { SubagentLimitsEditor } from "./SubagentLimitsEditor";

const render = (extra: Partial<Parameters<typeof SubagentLimitsEditor>[0]> = {}) =>
  renderToStaticMarkup(<SubagentLimitsEditor stored={null} saving={false} error="" onSave={async () => true} {...extra} />);

describe("SubagentLimitsEditor", () => {
  test("is labelled Codex only, warns about cost, and says Claude has no control", () => {
    const html = render();
    expect(html).toContain("Subagent limits");
    expect(html).toContain("Codex only");
    expect(html).toContain("multiply the model calls");
    expect(html).toContain("Claude: a subagent cannot start another subagent");
    // The depth setting does nothing in 0.163, so no control for it is drawn.
    expect(html).toContain("ignores it");
    expect(html.match(/<input/g)?.length).toBe(1);
  });

  test("unset shows an empty field and no reset; a stored value shows the field, its meaning and a reset", () => {
    const unset = render();
    expect(unset).toContain("Codex decides");
    expect(unset).not.toContain("Use Codex&#x27;s default");
    const set = render({ stored: 2 });
    expect(set).toContain('value="2"');
    expect(set).toContain("Up to 2 subagents at once");
    expect(set).toContain("Use Codex&#x27;s default");
  });

  test("Save is off until the value differs, and says an open session keeps its limit", () => {
    expect(render()).toMatch(/<button[^>]*disabled=""[^>]*>Save</);
    expect(render({ error: "boom" })).toContain('role="alert"');
    expect(render()).toContain("Reload session");
  });

  test("the field takes whole numbers from 0 to 16", () => {
    const html = render();
    expect(html).toContain('min="0"');
    expect(html).toContain('max="16"');
  });
});
