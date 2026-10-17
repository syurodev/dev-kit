import { describe, expect, test } from "bun:test";
import { readableLicenses } from "./thirdPartyLicenses";

describe("readableLicenses", () => {
  test("drops the file title, fences and heading marks but keeps every notice line", () => {
    const text = readableLicenses([
      "# Third-party licenses", "", "Intro.", "", "## Packages", "", "### MIT", "", "- a@1.0.0 (npm)", "",
      "```text", "MIT License", "", "Copyright (c) A", "```", "",
    ].join("\n"));
    expect(text.startsWith("Intro.")).toBe(true);
    expect(text).toContain("PACKAGES");
    expect(text).toContain("\nMIT\n");
    expect(text).toContain("- a@1.0.0 (npm)");
    expect(text).toContain("Copyright (c) A");
    expect(text).not.toContain("```");
    expect(text).not.toContain("#");
  });
  test("reads the summary table as plain lines and drops inline marks", () => {
    const text = readableLicenses("# T\n\n| License | Packages |\n| --- | ---: |\n| MIT | 459 |\n\n- **Geist** via `@fontsource/geist`");
    expect(text).toBe("MIT — 459\n\n- Geist via @fontsource/geist");
  });
  test("collapses long runs of blank lines", () => {
    expect(readableLicenses("a\n\n\n\n\n\nb")).toBe("a\n\n\nb");
  });
});
