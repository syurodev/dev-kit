import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { IdleReleaseEditor } from "./IdleReleaseEditor";

const render = (extra: Partial<Parameters<typeof IdleReleaseEditor>[0]> = {}) =>
  renderToStaticMarkup(
    <IdleReleaseEditor
      stored={{ enabled: true, childMinutes: 10, topMinutes: 30 }}
      saving={false}
      error=""
      onSave={async () => true}
      {...extra}
    />,
  );

describe("IdleReleaseEditor", () => {
  test("says what is released and what is kept", () => {
    const html = render();
    expect(html).toContain("Idle sessions");
    expect(html).toContain("stays in the list");
    expect(html).toContain("conversation is");
    expect(html).toContain("never released while it is working");
  });

  test("shows the stored times in the fields and in one sentence", () => {
    const html = render({ stored: { enabled: true, childMinutes: 5, topMinutes: 90 } });
    expect(html).toContain('value="5"');
    expect(html).toContain('value="90"');
    expect(html).toContain("Sub-sessions after 5 minutes idle, your own after 1 hour 30 minutes.");
  });

  test("off disables the time fields and says every session keeps its process", () => {
    const html = render({ stored: { enabled: false, childMinutes: 10, topMinutes: 30 } });
    expect(html).toContain("every session keeps its process until it is closed");
    expect(html.match(/<input[^>]*disabled=""[^>]*type="number"|<input[^>]*type="number"[^>]*disabled=""/g)?.length).toBe(2);
  });

  test("the fields take whole minutes from 2 to 1440, and Save is off until something changes", () => {
    const html = render();
    expect(html).toContain('min="2"');
    expect(html).toContain('max="1440"');
    expect(html).toMatch(/<button[^>]*disabled=""[^>]*>Save</);
  });

  test("an error reaches the person as an alert", () => {
    expect(render({ error: "boom" })).toContain('role="alert"');
  });
});
