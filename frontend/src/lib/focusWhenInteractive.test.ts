import { describe, expect, test } from "bun:test";
import { focusStillWanted, focusWhenInteractive, type FocusEnv } from "./focusWhenInteractive";

describe("focusWhenInteractive: focus deferred until an inert panel opens", () => {
  const body = { id: "body" };
  const editor = { id: "editor" };
  const search = { id: "search" };

  test("taken when focus is still where it was when asked, or nowhere", () => {
    // Shift+F12 from the editor: the references panel is still inert while it
    // slides in; when it settles, focus moves into it.
    expect(focusStillWanted({ requestedFrom: editor, now: editor, body })).toBe(true);
    expect(focusStillWanted({ requestedFrom: editor, now: body, body })).toBe(true);
    expect(focusStillWanted({ requestedFrom: editor, now: null, body })).toBe(true);
  });

  test("not taken from somewhere the person went in the meantime", () => {
    expect(focusStillWanted({ requestedFrom: editor, now: search, body })).toBe(false);
  });
});

describe("focusWhenInteractive", () => {
  type El = { id: string; inert: { blocked: boolean } | null };
  function env(active: unknown = "editor") {
    let lift: (() => void) | null = null;
    let disconnected = 0;
    let timer: (() => void) | null = null;
    const focused: string[] = [];
    const e: FocusEnv<El> = {
      inertAncestor: (el) => (el.inert && el.inert.blocked ? el.inert : null),
      watchInert: (_blocker, onChange) => {
        lift = onChange;
        return () => {
          disconnected++;
          lift = null;
        };
      },
      focus: (el) => void focused.push(el.id),
      active: () => active,
      body: "body",
      connected: () => true,
      setTimer: (fn) => {
        timer = fn;
        return 1;
      },
      clearTimer: () => {
        timer = null;
      },
    };
    return {
      e,
      focused,
      lift: (el: El) => {
        el.inert!.blocked = false;
        lift?.();
      },
      fireTimer: () => timer?.(),
      disconnected: () => disconnected,
      setActive: (a: unknown) => (active = a),
    };
  }

  test("focuses at once when nothing inert holds it", () => {
    const t = env();
    focusWhenInteractive({ id: "panel", inert: null }, t.e);
    expect(t.focused).toEqual(["panel"]);
  });

  test("waits for the inert column to lift, then focuses", () => {
    const t = env();
    const el = { id: "panel", inert: { blocked: true } };
    focusWhenInteractive(el, t.e);
    expect(t.focused).toEqual([]);
    t.lift(el);
    expect(t.focused).toEqual(["panel"]);
  });

  test("cancelled — the pane closed again before it settled — it never focuses and stops watching", () => {
    const t = env();
    const el = { id: "panel", inert: { blocked: true } };
    const cancel = focusWhenInteractive(el, t.e);
    cancel();
    expect(t.disconnected()).toBe(1);
    t.lift(el);
    expect(t.focused).toEqual([]);
  });

  test("gives up after the motion's watchdog, and stops watching", () => {
    const t = env();
    const el = { id: "panel", inert: { blocked: true } };
    focusWhenInteractive(el, t.e);
    t.fireTimer();
    expect(t.disconnected()).toBe(1);
    t.lift(el);
    expect(t.focused).toEqual([]);
  });

  test("does not take focus from where the person went meanwhile", () => {
    const t = env();
    const el = { id: "panel", inert: { blocked: true } };
    focusWhenInteractive(el, t.e);
    t.setActive("search");
    t.lift(el);
    expect(t.focused).toEqual([]);
  });
});
