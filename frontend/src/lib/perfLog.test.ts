import { expect, test } from "bun:test";
import { recordCommit, takeCommits, WORKBENCH_PROFILER } from "./perfLog";

test("the Workbench total breaks down by zone, largest first, with the rest as other", () => {
  recordCommit(WORKBENCH_PROFILER, "update", 50);
  recordCommit("rail", "update", 10);
  recordCommit("views", "update", 25);
  recordCommit("views", "update", 5);
  recordCommit("tabs", "update", 0.4);
  expect(takeCommits()).toEqual({ total: 50, zones: "views 30, rail 10, other 10" });
});

test("reading starts the count again", () => {
  recordCommit(WORKBENCH_PROFILER, "update", 12);
  takeCommits();
  expect(takeCommits()).toEqual({ total: 0, zones: "" });
});
