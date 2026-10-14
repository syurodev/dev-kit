import { expect, test } from "bun:test";
import { shortAge } from "./time";
import { shortAge as sidebarShortAge } from "@/modules/agenthub/sidebar/model";

test("shortAge: the shortest honest age, empty when unknown", () => {
  const now = Date.parse("2026-10-14T12:00:00Z");
  expect(shortAge(now - 30_000, now)).toBe("now");
  expect(shortAge(now - 2 * 3_600_000, now)).toBe("2h");
  expect(shortAge(undefined, now)).toBe("");
});

test("the Agent Hub rail reads the same function", () => {
  expect(sidebarShortAge).toBe(shortAge);
});
