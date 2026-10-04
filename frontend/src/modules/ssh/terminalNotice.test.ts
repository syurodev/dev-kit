import { expect, test } from "bun:test";
import { terminalExitNotice } from "./terminalNotice";

test("transport loss shows reconnect guidance, but intentionally closing a tab stays quiet", () => {
  expect(terminalExitNotice(-1, "exited")).toContain("lost");
  expect(terminalExitNotice(-1, "exited")).toContain("reconnect");
  expect(terminalExitNotice(-1, "closed")).toBe("");
  expect(terminalExitNotice(0, "exited")).toBe("Remote terminal session ended.");
  expect(terminalExitNotice(7, "exited")).toContain("exit code 7");
});
