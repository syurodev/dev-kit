import "@/testing/happyDom";
import { describe, expect, mock, spyOn, test } from "bun:test";

import {
  createClientErrorReporter,
  installWindowErrorReporting,
  MAX_CLIENT_ERROR_REPORTS,
  type ClientErrorSender,
} from "./clientErrors";

type Sent = { label: string; message: string; stack: string; componentStack: string };

function recorder() {
  const sent: Sent[] = [];
  const send: ClientErrorSender = (label, message, stack, componentStack) => {
    sent.push({ label, message, stack, componentStack });
    return Promise.resolve();
  };
  return { sent, send };
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("createClientErrorReporter", () => {
  test("sends the label, message, stack and component stack of an Error", () => {
    const { sent, send } = recorder();
    const error = new Error("boom");
    createClientErrorReporter(send)("app", error, "\n    at Card");
    expect(sent.length).toBe(1);
    expect(sent[0].label).toBe("app");
    expect(sent[0].message).toBe("boom");
    expect(sent[0].stack).toBe(error.stack ?? "");
    expect(sent[0].componentStack).toBe("\n    at Card");
  });

  test("a thrown string is the message; an object is only named, never read", () => {
    const { sent, send } = recorder();
    const report = createClientErrorReporter(send);
    report("a", "plain text", undefined);
    report("a", { token: "secret-in-a-prop", nested: { deep: 1 } }, undefined);
    report("a", null, undefined);
    expect(sent.map((s) => s.message)).toEqual(["plain text", "[object Object]", "null"]);
    expect(sent.map((s) => s.stack)).toEqual(["", "", ""]);
    expect(JSON.stringify(sent)).not.toContain("secret-in-a-prop");
  });

  test("a value that cannot be read still reports, and never throws", () => {
    const { sent, send } = recorder();
    const report = createClientErrorReporter(send);
    const hostileError = new Error("x");
    Object.defineProperty(hostileError, "message", {
      get() {
        throw new Error("no");
      },
    });
    const hostileObject = {
      toString() {
        throw new Error("no");
      },
    };
    expect(() => report("a", hostileError, undefined)).not.toThrow();
    expect(() => report("a", hostileObject, undefined)).not.toThrow();
    expect(sent.map((s) => s.message)).toEqual(["[unprintable error]", "[object Object]"]);
  });

  test("clips what it sends so a huge stack does not cross the bridge whole", () => {
    const { sent, send } = recorder();
    const error = new Error("m".repeat(50_000));
    error.stack = "s".repeat(50_000);
    createClientErrorReporter(send)("a", error, "c".repeat(50_000));
    expect(sent[0].message.length).toBe(1000);
    expect(sent[0].stack.length).toBe(4000);
    expect(sent[0].componentStack.length).toBe(2000);
  });

  test("never throws when the bridge call throws or rejects", async () => {
    const throws: ClientErrorSender = () => {
      throw new Error("bridge is gone");
    };
    const rejects: ClientErrorSender = () => Promise.reject(new Error("denied"));
    const notAPromise = (() => undefined) as unknown as ClientErrorSender;
    for (const send of [throws, rejects, notAPromise]) {
      expect(() => createClientErrorReporter(send)("a", new Error("x"), "y")).not.toThrow();
    }
    await tick();
  });

  test("handles the rejection of the bridge call itself, so it is never left unhandled", () => {
    // Bun does not surface every unhandled rejection from inside a test, so look for the handler.
    const rejection = Promise.reject(new Error("denied"));
    const attached = spyOn(rejection, "then");
    createClientErrorReporter(() => rejection)("a", new Error("x"), "y");
    expect(attached).toHaveBeenCalled();
  });

  test("a sender that throws does not stop the next report", () => {
    let calls = 0;
    const send: ClientErrorSender = () => {
      calls += 1;
      if (calls === 1) throw new Error("first fails");
      return Promise.resolve();
    };
    const report = createClientErrorReporter(send);
    report("a", "one", undefined);
    report("a", "two", undefined);
    expect(calls).toBe(2);
  });

  test("an identical message and stack is reported once per page load, whatever the label", () => {
    const { sent, send } = recorder();
    const report = createClientErrorReporter(send);
    const error = new Error("loop");
    report("app", error, "in A");
    report("app", error, "in A");
    report("window.error", error, undefined);
    report("app", new Error("loop"), "in A"); // same message, different stack
    expect(sent.map((s) => s.label)).toEqual(["app", "app"]);
  });

  test("reports at most MAX_CLIENT_ERROR_REPORTS per page load", () => {
    expect(MAX_CLIENT_ERROR_REPORTS).toBe(20);
    const { sent, send } = recorder();
    const report = createClientErrorReporter(send);
    for (let i = 0; i < 100; i++) report("a", `distinct ${i}`, undefined);
    expect(sent.length).toBe(20);
    expect(sent[19].message).toBe("distinct 19");
  });

  test("each reporter has its own page-load state", () => {
    const one = recorder();
    const two = recorder();
    createClientErrorReporter(one.send)("a", "same", undefined);
    createClientErrorReporter(two.send)("a", "same", undefined);
    expect(one.sent.length + two.sent.length).toBe(2);
  });
});

describe("installWindowErrorReporting", () => {
  function setup() {
    const { sent, send } = recorder();
    const target = new EventTarget() as unknown as Window;
    const report = createClientErrorReporter(send);
    installWindowErrorReporting(target, report);
    return { sent, target };
  }

  function fireError(target: Window, init: { message?: string; error?: unknown; filename?: string; lineno?: number; colno?: number }) {
    const event = Object.assign(new Event("error", { cancelable: true }), init);
    target.dispatchEvent(event);
    return event;
  }

  function fireRejection(target: Window, reason: unknown) {
    const event = Object.assign(new Event("unhandledrejection", { cancelable: true }), { reason });
    target.dispatchEvent(event);
    return event;
  }

  test("a window error reports under window.error with the Error's message and stack", () => {
    const { sent, target } = setup();
    const error = new TypeError("x is not a function");
    fireError(target, { message: "Uncaught TypeError: x is not a function", error });
    expect(sent.length).toBe(1);
    expect(sent[0].label).toBe("window.error");
    expect(sent[0].message).toBe("x is not a function");
    expect(sent[0].stack).toBe(error.stack ?? "");
    expect(sent[0].componentStack).toBe("");
  });

  test("an error event without an Error object reports its message and where it came from", () => {
    const { sent, target } = setup();
    fireError(target, { message: "Uncaught SyntaxError: bad", filename: "http://localhost/assets/a.js", lineno: 3, colno: 9 });
    expect(sent[0].message).toBe("Uncaught SyntaxError: bad (http://localhost/assets/a.js:3:9)");
  });

  test("an unhandled rejection reports under window.unhandledrejection", () => {
    const { sent, target } = setup();
    const reason = new Error("request failed");
    fireRejection(target, reason);
    fireRejection(target, "just a string");
    expect(sent.map((s) => s.label)).toEqual(["window.unhandledrejection", "window.unhandledrejection"]);
    expect(sent.map((s) => s.message)).toEqual(["request failed", "just a string"]);
    expect(sent[0].stack).toBe(reason.stack ?? "");
  });

  test.each([
    "ResizeObserver loop limit exceeded",
    "ResizeObserver loop completed with undelivered notifications.",
    "Uncaught ResizeObserver loop completed with undelivered notifications.",
  ])("ignores the benign %p notification", (message) => {
    const { sent, target } = setup();
    fireError(target, { message });
    fireError(target, { message, error: new Error(message) });
    fireRejection(target, new Error(message));
    expect(sent.length).toBe(0);
  });

  test("ignores the cross-origin Script error. with no detail, but not one our code threw", () => {
    const { sent, target } = setup();
    fireError(target, { message: "Script error." });
    fireError(target, { message: "Script error.", error: null });
    expect(sent.length).toBe(0);
    fireError(target, { message: "Script error.", error: new Error("Script error.") });
    expect(sent.length).toBe(1);
  });

  test("changes no behaviour: the event is not cancelled, and a throwing report is swallowed", () => {
    const target = new EventTarget() as unknown as Window;
    const throwing = mock(() => {
      throw new Error("report failed");
    });
    installWindowErrorReporting(target, throwing);
    let error: Event | undefined;
    expect(() => {
      error = fireError(target, { message: "boom", error: new Error("boom") });
      fireRejection(target, new Error("boom"));
    }).not.toThrow();
    expect(throwing).toHaveBeenCalledTimes(2);
    expect(error?.defaultPrevented).toBe(false);
  });

  test("installs once per window: a second install adds no second listener", () => {
    const { sent, send } = recorder();
    const target = new EventTarget() as unknown as Window;
    const report = createClientErrorReporter(send);
    installWindowErrorReporting(target, report);
    installWindowErrorReporting(target, report);
    fireError(target, { message: "once", error: new Error("once") });
    // Dedupe would hide a double listener, so count the calls to report instead.
    const counted = mock(() => {});
    const other = new EventTarget() as unknown as Window;
    installWindowErrorReporting(other, counted);
    installWindowErrorReporting(other, counted);
    fireError(other, { message: "once", error: new Error("once") });
    expect(sent.length).toBe(1);
    expect(counted).toHaveBeenCalledTimes(1);
  });
});
