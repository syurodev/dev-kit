import { afterEach, describe, expect, test } from "bun:test";
import { useCodeGraphStore } from "./codegraphStore";
import { isFocusedService } from "./codegraphActions";

describe("isFocusedService", () => {
  afterEach(() => useCodeGraphStore.setState({ projectPath: "" }));

  test("an event with no folder means the focused project", () => {
    expect(isFocusedService("")).toBe(true);
  });

  test("matches the focused folder, trailing separator or not", () => {
    useCodeGraphStore.setState({ projectPath: "/ws/orders" });
    expect(isFocusedService("/ws/orders")).toBe(true);
    expect(isFocusedService("/ws/orders/")).toBe(true);
  });

  test("another workspace service is not the focused one", () => {
    useCodeGraphStore.setState({ projectPath: "/ws/orders" });
    expect(isFocusedService("/ws/billing")).toBe(false);
  });

  test("with nothing focused only an unnamed event counts", () => {
    expect(isFocusedService("/ws/orders")).toBe(false);
  });
});
