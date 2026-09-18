import { describe, expect, it } from "bun:test";
import { squirclePath } from "./squircle";

describe("squirclePath", () => {
  it("starts the curve 1.6 radii from the corner at Apple's smoothing", () => {
    // A circular corner starts bending one radius out; a continuous one
    // earlier, which is the whole visible difference.
    expect(squirclePath(400, 300, 12).startsWith("M 380.8 0")).toBe(true);
    expect(squirclePath(400, 300, 12, 0).startsWith("M 388 0")).toBe(true);
  });

  it("never draws a negative box", () => {
    expect(squirclePath(-5, 10, 4)).not.toContain("NaN");
  });
});

describe("shell squircle", () => {
  it("draws the ring half a pixel in, in the resolved border colour", async () => {
    const { shellRingImage } = await import("./shellSquircle");
    const svg = decodeURIComponent(shellRingImage(400, 300, 12, "rgba(255, 255, 255, 0.1)"));
    expect(svg).toContain("translate(0.5 0.5)");
    expect(svg).toContain("stroke='rgba(255, 255, 255, 0.1)'");
    // One pixel narrower than the card, so the stroke's outer edge is the card's.
    expect(svg).toContain("M 380.6 0"); // 399 − 1.6 × 11.5
  });

  it("does nothing outside the macOS window", async () => {
    const { shellSquircleRef } = await import("./shellSquircle");
    expect(shellSquircleRef(null)).toBeUndefined();
  });
});

describe("corner-shape progressive enhancement", () => {
  it("gives large surfaces continuous corners wherever the engine supports it, but not the shell cards", async () => {
    const { readFileSync } = await import("node:fs");
    const css = readFileSync(new URL("../index.css", import.meta.url), "utf8");
    const block = css.slice(css.indexOf("@supports (corner-shape: squircle)"));
    expect(block).toContain(".rounded-lg,");
    expect(block).toContain(".rounded-float {\n    corner-shape: squircle;");
    // Shell cards follow the window's own corner instead (shellSquircle.ts).
    expect(block.slice(0, block.indexOf("}\n}"))).not.toContain(".shell-surface");
  });
});
