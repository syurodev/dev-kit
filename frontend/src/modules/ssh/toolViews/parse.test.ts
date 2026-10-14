import { describe, expect, test } from "bun:test";
import { clipLines, displayOutput, lineCount, parseRunCommand } from "./parse";

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

describe("displayOutput", () => {
  test("drops ANSI colour and cursor escapes", () => {
    expect(displayOutput("\x1b[1;32mok\x1b[0m done\x1b[?25h")).toBe("ok done");
  });

  test("drops CSI with private and intermediate parameters", () => {
    expect(displayOutput("a\x1b[>4;2mb\x1b[=1uc\x1b[1 qd\x1b[<0;10;5Me")).toBe("abcde");
  });

  test("drops OSC strings, hyperlinks included, ended by BEL or ST", () => {
    expect(displayOutput("\x1b]0;window title\x07ok")).toBe("ok");
    expect(displayOutput("\x1b]8;;https://example.test/a\x1b\\link\x1b]8;;\x1b\\ done")).toBe("link done");
  });

  test("drops DCS, SOS, PM and APC strings", () => {
    expect(displayOutput("a\x1bPq#0;2;0;0;0\x1b\\b\x1bXsos\x1b\\c\x1b^pm\x1b\\d\x1b_apc\x1b\\e")).toBe("abcde");
  });

  test("drops charset designators and two-character escapes", () => {
    expect(displayOutput("\x1b(Bplain\x1b)0 \x1b=keypad\x1b> \x1bMup\x1b7save\x1b8")).toBe("plain keypad upsave");
  });

  test("a line redrawn with \\r shows only its last state", () => {
    expect(displayOutput("start\n 10%\r 50%\r100%\nend\n")).toBe("start\n100%\nend\n");
  });

  test("a line that ends in \\r keeps what it said", () => {
    expect(displayOutput("progress 100%\r")).toBe("progress 100%");
    expect(displayOutput("x\r\r\ny")).toBe("x\ny");
    expect(displayOutput(" 10%\r 50%\r100%\r\ndone")).toBe("100%\ndone");
  });

  test("CRLF line ends are plain line ends", () => {
    expect(displayOutput("a\r\nb\r\n")).toBe("a\nb\n");
  });
});
