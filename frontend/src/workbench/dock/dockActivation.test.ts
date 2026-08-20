import { expect, test } from "bun:test";
import { isPointerNear, shortcutLabel, moduleIndexFromEvent } from "./dockActivation";

const rect = { left: 660, top: 880, right: 770, bottom: 893 };

test("a pointer inside the pill is near", () => {
  expect(isPointerNear({ x: 700, y: 885 }, rect, 64)).toBe(true);
});

test("a pointer within the activation margin is near", () => {
  expect(isPointerNear({ x: 700, y: 830 }, rect, 64)).toBe(true);
  expect(isPointerNear({ x: 610, y: 885 }, rect, 64)).toBe(true);
});

test("a pointer beyond the margin is not near", () => {
  expect(isPointerNear({ x: 700, y: 700 }, rect, 64)).toBe(false);
  expect(isPointerNear({ x: 200, y: 885 }, rect, 64)).toBe(false);
});

test("the margin applies on every side, including below the pill", () => {
  // The pill sits 8px off the window bottom; the few pixels under it must
  // still count, or approaching from the very edge never expands it.
  expect(isPointerNear({ x: 700, y: 899 }, rect, 64)).toBe(true);
});

test("shortcut labels are one-based", () => {
  expect(shortcutLabel(0)).toBe("⌘1");
  expect(shortcutLabel(6)).toBe("⌘7");
});

test("only the first nine modules get a shortcut", () => {
  expect(shortcutLabel(8)).toBe("⌘9");
  expect(shortcutLabel(9)).toBeNull();
});

test("a meta+digit event maps to a zero-based module index", () => {
  expect(moduleIndexFromEvent({ key: "1", metaKey: true, ctrlKey: false, altKey: false, shiftKey: false })).toBe(0);
  expect(moduleIndexFromEvent({ key: "7", metaKey: false, ctrlKey: true, altKey: false, shiftKey: false })).toBe(6);
});

test("digit zero is not a module shortcut", () => {
  // Not merely "there is no module 0": ⌘0 is Actual Size in the default macOS
  // menu Wails installs, and a native menu accelerator fires before the
  // webview sees the key. Claiming it here would be dead code.
  expect(moduleIndexFromEvent({ key: "0", metaKey: true, ctrlKey: false, altKey: false, shiftKey: false })).toBeNull();
});

test("modified variants are left alone for other handlers", () => {
  // ⌘⇧1 and ⌘⌥1 must stay available — claiming every ⌘-digit combination
  // would silently shadow shortcuts other surfaces may add later.
  expect(moduleIndexFromEvent({ key: "1", metaKey: true, ctrlKey: false, altKey: false, shiftKey: true })).toBeNull();
  expect(moduleIndexFromEvent({ key: "1", metaKey: true, ctrlKey: false, altKey: true, shiftKey: false })).toBeNull();
});

test("a bare digit is not a shortcut", () => {
  expect(moduleIndexFromEvent({ key: "1", metaKey: false, ctrlKey: false, altKey: false, shiftKey: false })).toBeNull();
});
