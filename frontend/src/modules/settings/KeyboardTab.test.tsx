import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { KeyboardTab } from "./KeyboardTab";

describe("KeyboardTab", () => {
  it("draws the shell's shortcuts under a search field, on the first frame", () => {
    // No module is registered under test, so this is the shell's own list: what
    // matters is that it is there at once, with no empty state and no fetch.
    const html = renderToString(<KeyboardTab intro={<h2>Keyboard</h2>} />);
    expect(html).toContain('aria-label="Search shortcuts"');
    expect(html).toContain("Everywhere");
    expect(html).toContain("Command palette");
    expect(html).toContain("Peek a module");
    expect(html).not.toContain("No shortcut matches");
  });

  it("marks no section as where you are when the module has no shortcuts", () => {
    expect(renderToString(<KeyboardTab intro={null} />)).not.toContain("you are here");
  });
});
