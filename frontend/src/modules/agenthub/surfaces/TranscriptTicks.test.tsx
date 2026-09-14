import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TranscriptTicks, ticksOf } from "./TranscriptTicks";

describe("ticksOf", () => {
  it("makes one tick per prompt, with the answer as plain text", () => {
    const ticks = ticksOf([
      { id: "t0", answer: [{ text: "before anyone spoke" }] },
      {
        id: "t1",
        prompt: { text: "  explore\nthe repo " },
        answer: [{ text: "## Cấu trúc\n\n- `dev-kit-backend` — **Gradle**" }],
      },
      {
        id: "t3",
        prompt: { text: "table" },
        answer: [{ text: "```bash\n./gradlew build\n```\n| A | B |\n| --- | ---: |\n| x | 1 |" }],
      },
      { id: "t2", prompt: { text: "run tests" }, answer: [] },
    ]);
    expect(ticks).toEqual([
      { id: "t1", prompt: "explore the repo", answer: "Cấu trúc dev-kit-backend — Gradle" },
      { id: "t3", prompt: "table", answer: "./gradlew build A B x 1" },
      { id: "t2", prompt: "run tests", answer: undefined },
    ]);
  });
});

describe("TranscriptTicks", () => {
  const ticks = [
    { id: "a", prompt: "first" },
    { id: "b", prompt: "second" },
  ];

  it("marks the prompt being read", () => {
    const html = renderToString(<TranscriptTicks ticks={ticks} active={1} onJump={() => {}} />);
    expect(html.match(/data-testid="transcript-tick"/g)).toHaveLength(2);
    expect(html).toContain('aria-label="second" aria-current="location"');
  });

  it("stays out of the way of a one-prompt conversation", () => {
    const html = renderToString(<TranscriptTicks ticks={ticks.slice(0, 1)} active={0} onJump={() => {}} />);
    expect(html).toBe("");
  });
});
