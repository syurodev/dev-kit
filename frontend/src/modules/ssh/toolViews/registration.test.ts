import { expect, test } from "bun:test";
import "../manifest";
import { renderToolResult } from "@/workbench/toolResultViews";

test("ssh.runCommand gets a card under its Go name and Claude's wire name", () => {
  expect(renderToolResult({ name: "ssh.runCommand", rawInput: { command: "ls" }, rawOutput: { output: "a" } })).not.toBeNull();
  expect(
    renderToolResult({ name: "mcp__devkit__ssh_runCommand", rawInput: { command: "ls" }, rawOutput: { output: "a" } }),
  ).not.toBeNull();
});

test("a result that is not {output} falls back to the generic view", () => {
  expect(renderToolResult({ name: "ssh.runCommand", rawInput: { command: "ls" }, rawOutput: { truncated: true } })).toBeNull();
});
