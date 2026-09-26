import { describe, expect, test } from "bun:test";
import type { Board } from "@/bridge/tasks";
import { canRunFromPlan, homeBoardOf } from "./boardHome";

function board(overrides: Partial<Board> = {}): Board {
  return {
    id: "b1",
    name: "DevKit",
    keyPrefix: "DEV",
    color: "blue",
    workflow: { statuses: [] },
    workspaceId: "",
    ...overrides,
  };
}

describe("homeBoardOf", () => {
  test("picks the current workspace's first board by name, not by list order", () => {
    const boards = [
      board({ id: "b2", name: "Zeta", workspaceId: "w1" }),
      board({ id: "b1", name: "Alpha", workspaceId: "w1" }),
      board({ id: "b3", name: "Other workspace's board", workspaceId: "w2" }),
    ];
    expect(homeBoardOf(boards, "w1")?.id).toBe("b1");
  });

  test("null with no current workspace", () => {
    expect(homeBoardOf([board({ workspaceId: "w1" })], null)).toBeNull();
  });

  test("null when the current workspace has no board of its own", () => {
    const boards = [board({ workspaceId: "w2" }), board({ id: "b2", workspaceId: "" })];
    expect(homeBoardOf(boards, "w1")).toBeNull();
  });
});

describe("canRunFromPlan", () => {
  test("only for a board bound to a workspace", () => {
    expect(canRunFromPlan({ workspaceId: "w1" })).toBe(true);
    expect(canRunFromPlan({ workspaceId: "" })).toBe(false);
    expect(canRunFromPlan({ workspaceId: undefined })).toBe(false);
  });
});
