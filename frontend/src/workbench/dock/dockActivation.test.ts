import { expect, test } from "bun:test";
import {
  ACTIVATION_MARGIN,
  EXPAND_LIFT,
  isPointerNear,
  shortcutLabel,
  moduleIndexFromEvent,
} from "./dockActivation";

// A 900px-tall window: the resting pill is 126x15, centred, 7px off the bottom.
const WINDOW_HEIGHT = 900;
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

test("the margin applies on every side, including below the pill", () => {
  // The pill sits a few px off the window bottom; those px must still count,
  // or approaching from the very edge never expands it.
  expect(isPointerNear({ x: 700, y: 899 }, rect, ACTIVATION_MARGIN)).toBe(true);
});

test("opening the dock never moves it out of its own trigger zone", () => {
  // The pill lifts as it opens, which moves it AWAY from the pointer that just
  // opened it. Left unchecked that oscillates: a pointer resting at the edge
  // of the zone opens the dock, falls outside the moved zone, closes it, and
  // the dock flickers under a motionless mouse. It is safe here only because
  // the pill grows upward (15px tall -> 46px) by more than it lifts.
  //
  // Points below the window are excluded on purpose, not for convenience —
  // that is where the invariant genuinely fails, and it holds in practice
  // because pointermove never reports a clientY past the viewport.
  const open = {
    left: 592,
    right: 854,
    bottom: rect.bottom - EXPAND_LIFT,
    top: rect.bottom - EXPAND_LIFT - 46,
  };
  let checked = 0;
  for (let x = 400; x <= 1040; x += 1) {
    for (let y = WINDOW_HEIGHT - 220; y <= WINDOW_HEIGHT; y += 1) {
      const pointer = { x, y };
      if (!isPointerNear(pointer, rect, ACTIVATION_MARGIN)) continue;
      checked += 1;
      expect(isPointerNear(pointer, open, ACTIVATION_MARGIN)).toBe(true);
    }
  }
  // Guards the guard: a typo in the sweep bounds would pass vacuously.
  expect(checked).toBeGreaterThan(5000);
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
