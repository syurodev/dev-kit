import { expect, test } from "bun:test";
import { dispatchAttachIntent, hasAttachIntent, registerAttachIntent } from "./intents";
import type { EntityRef } from "./intents";

const ref: EntityRef = { module: "codegraph", kind: "endpoint", id: "POST /x", label: "POST /x" };

test("dispatchAttachIntent is unavailable when nobody registered the name", async () => {
  expect(hasAttachIntent("agent.attach.nobody-registered-this")).toBe(false);
  expect(await dispatchAttachIntent("agent.attach.nobody-registered-this", [ref])).toBe("unavailable");
});

test("registerAttachIntent hands the handler every ref and the optional prompt", async () => {
  const seen: Array<{ refs: EntityRef[]; prompt?: string }> = [];
  const unregister = registerAttachIntent("agent.attach.test", (refs, prompt) => {
    seen.push({ refs, prompt });
  });
  try {
    expect(hasAttachIntent("agent.attach.test")).toBe(true);
    const result = await dispatchAttachIntent("agent.attach.test", [ref], "why does this happen?");
    expect(result).toBe("ok");
    expect(seen).toEqual([{ refs: [ref], prompt: "why does this happen?" }]);
  } finally {
    unregister();
  }
  expect(hasAttachIntent("agent.attach.test")).toBe(false);
});

test("registering the same name twice replaces the handler", async () => {
  const calls: string[] = [];
  registerAttachIntent("agent.attach.replace-test", () => {
    calls.push("first");
  });
  const unregisterSecond = registerAttachIntent("agent.attach.replace-test", () => {
    calls.push("second");
  });
  await dispatchAttachIntent("agent.attach.replace-test", [ref]);
  expect(calls).toEqual(["second"]);
  unregisterSecond();
});
