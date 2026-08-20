import { expect, test } from "bun:test";
import { shortLabel } from "./labels";

test("a method keeps its class and method name", () => {
  expect(
    shortLabel("com.synx.devkit.identity.adapter.web.DeviceEnrollmentController.enroll", "method"),
  ).toBe("DeviceEnrollmentController.enroll");
});

test("a class keeps only its own name", () => {
  expect(shortLabel("com.synx.devkit.identity.adapter.web.SyncRequestAuthorizer", "class")).toBe(
    "SyncRequestAuthorizer",
  );
});

test("a nested class's method still reads as Inner.method", () => {
  expect(shortLabel("com.acme.Outer.Inner.run", "method")).toBe("Inner.run");
});

test("an already-short label is left alone", () => {
  expect(shortLabel("Main", "class")).toBe("Main");
  expect(shortLabel("Main.run", "method")).toBe("Main.run");
});

test("a default-package method is left alone rather than losing its class", () => {
  expect(shortLabel("Main.run", "method")).toBe("Main.run");
});

test("an unknown kind is treated as a class, not dropped", () => {
  expect(shortLabel("com.acme.Thing", "")).toBe("Thing");
});
