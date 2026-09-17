import { describe, expect, it } from "bun:test";

import {
  checkpointProblemLines,
  checkpointStatusLine,
  formatExcludes,
  parseExcludes,
  parsePositiveInteger,
} from "./agentCheckpoints";
import type { CheckpointAvailability } from "../../../bindings/devkit/internal/agenthub/models";

function status(overrides: Partial<CheckpointAvailability> = {}): CheckpointAvailability {
  return {
    available: true,
    enabled: true,
    gitPath: "/usr/bin/git",
    gitVersion: "2.43.0",
    root: "/data/checkpoints",
    ...overrides,
  } as CheckpointAvailability;
}

describe("the status line", () => {
  it("names the git it is using when it is on", () => {
    const line = checkpointStatusLine(status());
    expect(line.text).toBe("On, using git 2.43.0 at /usr/bin/git.");
    expect(line.switchEnabled).toBe(true);
    expect(line.tone).toBe("plain");
  });

  // "Off" on its own reads as broken. Off by choice says so, and still names
  // the git, so turning it back on is visibly going to work.
  it("says off by choice differently from off because it cannot", () => {
    const chosen = checkpointStatusLine(status({ enabled: false }));
    expect(chosen.text).toContain("Off.");
    expect(chosen.text).toContain("2.43.0");
    expect(chosen.tone).toBe("plain");
    expect(chosen.switchEnabled).toBe(true);
  });

  // The backend's reason already names the fix; repeating it here in different
  // words would be two answers to one question.
  it("shows the machine's own reason and kills the switch", () => {
    const line = checkpointStatusLine(
      status({
        available: false,
        enabled: true,
        reason: "Git needs Apple's Command Line Tools. Run `xcode-select --install`, then reopen DevKit.",
      }),
    );
    expect(line.text).toContain("xcode-select");
    expect(line.tone).toBe("warning");
    expect(line.switchEnabled).toBe(false);
  });

  it("has something to say before the answer arrives", () => {
    const line = checkpointStatusLine(null);
    expect(line.text).toBe("Checking…");
    expect(line.switchEnabled).toBe(false);
  });
});

describe("the projects that could not be checkpointed", () => {
  // Without this a project that is simply too large looks exactly like the
  // feature quietly not working.
  it("lists them with the reason, unwrapped", () => {
    const lines = checkpointProblemLines(
      status({
        problems: [
          { project: "/home/me/huge", reason: "checkpoint: project is too large to checkpoint: 90000 changed files exceeds the 50000 file ceiling" },
        ],
      }),
    );
    expect(lines).toHaveLength(1);
    expect(lines[0].project).toBe("/home/me/huge");
    expect(lines[0].reason).toStartWith("Project is too large");
    // The prefix is gone from the front; "to checkpoint:" inside the sentence
    // is the sentence, not the wrapper.
    expect(lines[0].reason).not.toStartWith("checkpoint:");
  });

  it("says nothing when the feature is off or unavailable", () => {
    const problems = [{ project: "/p", reason: "x" }];
    expect(checkpointProblemLines(status({ enabled: false, problems }))).toHaveLength(0);
    expect(checkpointProblemLines(status({ available: false, problems }))).toHaveLength(0);
    expect(checkpointProblemLines(null)).toHaveLength(0);
  });
});

describe("what the inputs accept", () => {
  it("takes whole positive numbers and nothing else", () => {
    expect(parsePositiveInteger("10")).toBe(10);
    expect(parsePositiveInteger("  25 ")).toBe(25);
    expect(parsePositiveInteger("0")).toBe(0);
    for (const bad of ["", "-1", "1.5", "ten", "1e9", "10px"]) {
      expect(parsePositiveInteger(bad)).toBeNull();
    }
  });

  // One pattern per line, and a blank line is not a pattern: an empty gitignore
  // line matches nothing, and keeping it would make the list look longer.
  it("reads the excludes box a line at a time", () => {
    expect(parseExcludes("generated/\n\n  *.snapshot  \n")).toEqual(["generated/", "*.snapshot"]);
    expect(parseExcludes("")).toEqual([]);
    expect(formatExcludes(["a/", "b/"])).toBe("a/\nb/");
    expect(formatExcludes(null)).toBe("");
  });
});
