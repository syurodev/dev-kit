import { afterEach, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { registerConfirmHost, requestConfirm } from "./confirmRequest";

let unregister: (() => void) | null = null;
afterEach(() => {
  unregister?.();
  unregister = null;
});

describe("asking the person from code", () => {
  test("with no dialog on screen the answer is no", async () => {
    expect(await requestConfirm({ title: "t", description: "d", confirmLabel: "Go" })).toBe(false);
  });

  test("the host's answer is the answer", async () => {
    const asked: string[] = [];
    unregister = registerConfirmHost(async (request) => (asked.push(request.description), true));
    expect(await requestConfirm({ title: "t", description: "Switch?", confirmLabel: "Switch" })).toBe(true);
    expect(asked).toEqual(["Switch?"]);
  });

  test("an unmounted host stops answering", async () => {
    registerConfirmHost(async () => true)();
    expect(await requestConfirm({ title: "t", description: "d", confirmLabel: "Go" })).toBe(false);
  });

  test("the Workbench mounts the host", () => {
    const source = readFileSync(join(import.meta.dir, "Workbench.tsx"), "utf8");
    expect(source).toContain("<ConfirmHost />");
  });
});
