import { expect, test } from "bun:test";
import { isPaletteShortcut, moduleIndexFromEvent, peekIndexFromEvent } from "./moduleShortcuts";

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

const key = (overrides: Partial<Parameters<typeof isPaletteShortcut>[0]>) => ({
  key: "k",
  metaKey: false,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  ...overrides,
});

test("⌘K opens the palette wherever the focus is", () => {
  expect(isPaletteShortcut(key({ metaKey: true }), false)).toBe(true);
  expect(isPaletteShortcut(key({ metaKey: true }), true)).toBe(true);
  expect(isPaletteShortcut(key({ metaKey: true, key: "K" }), false)).toBe(true); // caps lock
});

test("Ctrl+K opens it only where Ctrl+K does not already edit text", () => {
  // In a field, an editor or a terminal it is "kill to end of line", and the
  // platform cannot be asked whether this machine means that.
  expect(isPaletteShortcut(key({ ctrlKey: true }), false)).toBe(true);
  expect(isPaletteShortcut(key({ ctrlKey: true }), true)).toBe(false);
});

test("other chords on K are left alone", () => {
  expect(isPaletteShortcut(key({ metaKey: true, shiftKey: true }), false)).toBe(false);
  expect(isPaletteShortcut(key({ metaKey: true, altKey: true }), false)).toBe(false);
  expect(isPaletteShortcut(key({ metaKey: true, ctrlKey: true }), false)).toBe(false);
  expect(isPaletteShortcut(key({}), false)).toBe(false);
  expect(isPaletteShortcut(key({ metaKey: true, key: "j" }), false)).toBe(false);
});

const peekKey = (over: Partial<Parameters<typeof peekIndexFromEvent>[0]> = {}) => ({
  key: "¡",
  code: "Digit1",
  metaKey: true,
  ctrlKey: false,
  altKey: true,
  shiftKey: false,
  ...over,
});
const anywhere = { editsText: false, richText: false };

test("⌘⌥digit names a slot from the physical key, since ⌥ changes what the key says", () => {
  expect(peekIndexFromEvent(peekKey(), anywhere)).toBe(0);
  expect(peekIndexFromEvent(peekKey({ key: "™", code: "Digit2" }), anywhere)).toBe(1);
  expect(peekIndexFromEvent(peekKey({ code: "Numpad9" }), anywhere)).toBe(8);
});

test("Ctrl+Alt+digit does the same off macOS, when nothing is being typed", () => {
  expect(peekIndexFromEvent(peekKey({ metaKey: false, ctrlKey: true }), anywhere)).toBe(0);
});

test("Ctrl+Alt is AltGr on Windows, so it leaves a text field — and an AltGraph key — alone", () => {
  const ctrlAlt = peekKey({ metaKey: false, ctrlKey: true });
  expect(peekIndexFromEvent(ctrlAlt, { editsText: true, richText: false })).toBeNull();
  expect(peekIndexFromEvent({ ...ctrlAlt, getModifierState: (key) => key === "AltGraph" }, anywhere)).toBeNull();
  // ⌘⌥ types nothing, so a field does not stop it.
  expect(peekIndexFromEvent(peekKey(), { editsText: true, richText: false })).toBe(0);
});

test("the Notes editor keeps ⌘⌥1–⌘⌥6 for headings", () => {
  expect(peekIndexFromEvent(peekKey(), { editsText: true, richText: true })).toBeNull();
});

test("neither ⌘digit nor a chord with Shift or without ⌥ opens a peek", () => {
  expect(peekIndexFromEvent(peekKey({ altKey: false }), anywhere)).toBeNull();
  expect(peekIndexFromEvent(peekKey({ shiftKey: true }), anywhere)).toBeNull();
  expect(peekIndexFromEvent(peekKey({ metaKey: false }), anywhere)).toBeNull();
  expect(peekIndexFromEvent(peekKey({ code: "Digit0" }), anywhere)).toBeNull();
  expect(peekIndexFromEvent(peekKey({ code: "KeyA" }), anywhere)).toBeNull();
});

test("the module shortcut still ignores the peek chord", () => {
  expect(moduleIndexFromEvent({ key: "1", metaKey: true, ctrlKey: false, altKey: true, shiftKey: false })).toBeNull();
});
