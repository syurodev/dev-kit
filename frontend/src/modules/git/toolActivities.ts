import { parseToolOutput } from "@/workbench/toolResultViews";
import { firstLine, registerToolActivity, stringField, toolRecord } from "@/workbench/toolActivity";
import { parseCommitResult } from "./commitResult";

/**
 * What the git.* tools that change something do (workbench/toolActivity.ts).
 * Push is here above all: it is the one that leaves the machine.
 */
export function registerGitActivities(): void {
  registerToolActivity("git.commit", {
    module: "git",
    running: "Committing",
    done: "Committed",
    failed: "Couldn't commit",
    preload: () => import("./GitCommitResultCard"),
    target: ({ rawInput, rawOutput, state }) => {
      const commit = state === "done" ? parseCommitResult(rawOutput) : null;
      if (commit) {
        return { label: commit.subject, detail: commit.short, ref: { module: "git", kind: "commit", id: commit.sha, label: commit.short } };
      }
      const message = firstLine(stringField(rawInput, "message"));
      return message ? { label: message } : null;
    },
  });
  registerToolActivity("git.push", {
    module: "git",
    running: "Pushing",
    done: "Pushed",
    failed: "Couldn't push",
    target: ({ rawInput, rawOutput, state }) => {
      const out = state === "done" ? toolRecord(parseToolOutput(rawOutput)) : null;
      const branch = stringField(rawInput, "branch") || stringField(out, "branch");
      const remote = stringField(rawInput, "remote") || stringField(out, "remote");
      if (!branch && !remote) return null;
      return { label: branch || "current branch", detail: remote ? `to ${remote}` : undefined };
    },
  });
  registerToolActivity("git.branch.create", {
    module: "git",
    running: "Creating branch",
    done: "Created branch",
    failed: "Couldn't create branch",
    target: ({ rawInput }) => {
      const name = stringField(rawInput, "name");
      const from = stringField(rawInput, "from");
      return name ? { label: name, detail: from ? `from ${from}` : undefined } : null;
    },
  });
  registerToolActivity("git.checkout", {
    module: "git",
    running: "Switching branch",
    done: "Switched branch",
    failed: "Couldn't switch branch",
    target: ({ rawInput }) => {
      const name = stringField(rawInput, "name");
      return name ? { label: name } : null;
    },
  });
}
