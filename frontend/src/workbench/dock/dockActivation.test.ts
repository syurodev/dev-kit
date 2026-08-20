import { expect, test } from "bun:test";
import {
  ACTIVATION_MARGIN,
  isPointerNear,
  shortcutLabel,
  moduleIndexFromEvent,
} from "./dockActivation";

// A 900px-tall window: the resting pill is 126x15, centred, 7px off the bottom.
const rect = { left: 660, top: 878, right: 786, bottom: 893 };

test("a pointer inside the pill is near", () => {
  expect(isPointerNear({ x: 700, y: 885 }, rect, ACTIVATION_MARGIN)).toBe(true);
});

test("a pointer within the activation margin is near", () => {
  expect(
    isPointerNear({ x: 700, y: rect.top - ACTIVATION_MARGIN + 1 }, rect, ACTIVATION_MARGIN),
  ).toBe(true);
  expect(
    isPointerNear({ x: rect.left - ACTIVATION_MARGIN + 1, y: 885 }, rect, ACTIVATION_MARGIN),
  ).toBe(true);
});

test("a pointer beyond the margin is not near", () => {
  expect(
    isPointerNear({ x: 700, y: rect.top - ACTIVATION_MARGIN - 1 }, rect, ACTIVATION_MARGIN),
  ).toBe(false);
  expect(
    isPointerNear({ x: rect.left - ACTIVATION_MARGIN - 1, y: 885 }, rect, ACTIVATION_MARGIN),
  ).toBe(false);
});

test("there is no bottom bound at all", () => {
  // The few px between the pill and the window edge must count, or flicking
  // the mouse to the bottom of the screen never opens the dock. They are
  // covered by there being no lower bound rather than by a margin — see
  // isPointerNear's own comment for why a small one would flicker.
  expect(isPointerNear({ x: 700, y: 899 }, rect, ACTIVATION_MARGIN)).toBe(true);
  // Far below the pill, past anything a margin would have reached: still near,
  // because nothing can be down there but the window edge.
  expect(isPointerNear({ x: 700, y: 5000 }, rect, ACTIVATION_MARGIN)).toBe(true);
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
