import { describe, expect, test } from "bun:test";
import { clipLines, lineCount, parseRunCommand } from "./parse";

describe("ssh.runCommand", () => {
  test("reads the command from the input and the output from the result", () => {
    expect(parseRunCommand({ command: "uptime" }, { output: " 10:00 up 3 days\n" })).toEqual({
      command: "uptime",
      output: " 10:00 up 3 days\n",
    });
  });

  test("an input with no command still shows the output", () => {
    expect(parseRunCommand(undefined, { output: "ok" })).toEqual({ command: "", output: "ok" });
  });

  test("anything that is not {output} falls back to the generic view", () => {
    expect(parseRunCommand({ command: "ls" }, { stdout: "x" })).toBeNull();
    expect(parseRunCommand({ command: "ls" }, "plain")).toBeNull();
    expect(parseRunCommand({ command: "ls" }, null)).toBeNull();
  });
});

describe("lineCount", () => {
  test("counts lines, not the newline that ends the last one", () => {
    expect(lineCount("")).toBe(0);
    expect(lineCount("one")).toBe(1);
    expect(lineCount("one\n")).toBe(1);
    expect(lineCount("one\ntwo\nthree\n")).toBe(3);
    expect(lineCount("a\r\nb")).toBe(2);
  });
});

describe("clipLines", () => {
  const text = Array.from({ length: 25 }, (_, i) => `line ${i + 1}`).join("\n") + "\n";

  test("keeps the first lines and says how many it held back", () => {
    const clip = clipLines(text, 20);
    expect(clip.hidden).toBe(5);
    expect(clip.shown.split("\n")).toHaveLength(20);
    expect(clip.shown.startsWith("line 1\n")).toBe(true);
    expect(clip.shown.endsWith("line 20")).toBe(true);
  });

  test("output that fits is left alone, its trailing newline dropped", () => {
    expect(clipLines("a\nb\n", 20)).toEqual({ shown: "a\nb", hidden: 0 });
  });
});
