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

// ProjectAnalyzer.Index.uniqueId (analyzer/.../ProjectAnalyzer.java) qualifies
// a colliding class id — and, since a class's label is its id, its label too
// — with " [module]" rather than dropping one of the two. Dots and brackets
// are stripped from the qualifier before it is inserted, so it never adds a
// new "." for split() to trip over; these pin that the bracket survives
// intact rather than being cut in half or swallowed.
test("a collision-qualified class label keeps its bracket", () => {
  expect(shortLabel("com.acme.Application [services_billing]", "class")).toBe("Application [services_billing]");
});

test("a method on a collision-qualified class reads Class [module].method", () => {
  expect(shortLabel("com.acme.Application [services_billing].create", "method")).toBe(
    "Application [services_billing].create",
  );
});
