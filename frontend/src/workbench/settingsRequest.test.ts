import { beforeAll, describe, expect, test } from "bun:test";
import { closeSettings, onSettingsCloseRequested, onSettingsRequested, requestSettings } from "./settingsRequest";

// The Workbench owns the Settings modal; a module asks it to open or close.
describe("settings requests", () => {
  // The bus is a window event; bun has no window, and an EventTarget is all it uses.
  beforeAll(() => {
    (globalThis as { window?: unknown }).window ??= new EventTarget();
  });

  test("a close request reaches whoever listens, until they stop", () => {
    let closes = 0;
    const off = onSettingsCloseRequested(() => {
      closes += 1;
    });
    closeSettings();
    closeSettings();
    expect(closes).toBe(2);
    off();
    closeSettings();
    expect(closes).toBe(2);
  });

  test("opening and closing are different requests", () => {
    const opened: unknown[] = [];
    let closes = 0;
    const offOpen = onSettingsRequested((tab) => opened.push(tab));
    const offClose = onSettingsCloseRequested(() => {
      closes += 1;
    });
    requestSettings("agents-runtimes");
    expect(opened).toEqual(["agents-runtimes"]);
    expect(closes).toBe(0);
    closeSettings();
    expect(opened).toHaveLength(1);
    expect(closes).toBe(1);
    offOpen();
    offClose();
  });
});
