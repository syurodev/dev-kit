import { describe, expect, it } from "bun:test";
import { installNativeZoomGuard, isZoomGesture } from "./nativeZoom";

describe("isZoomGesture", () => {
  it("treats ctrl+wheel as a pinch", () => {
    // Not because anyone held ctrl: every engine sets ctrlKey on the synthetic
    // wheel event a trackpad pinch produces.
    expect(isZoomGesture({ ctrlKey: true })).toBe(true);
  });

  it("leaves an ordinary scroll alone", () => {
    expect(isZoomGesture({ ctrlKey: false })).toBe(false);
  });
});

describe("installNativeZoomGuard", () => {
  // A minimal stand-in for Window: bun test has no DOM, and what matters is
  // the registration options and which events get prevented.
  function fakeWindow() {
    let handler: ((event: WheelEvent) => void) | undefined;
    let options: AddEventListenerOptions | undefined;
    let removed = false;
    return {
      target: {
        addEventListener(_type: string, fn: EventListener, opts: AddEventListenerOptions) {
          handler = fn as (event: WheelEvent) => void;
          options = opts;
        },
        removeEventListener() {
          removed = true;
        },
      } as unknown as Window,
      dispatch(ctrlKey: boolean) {
        let prevented = false;
        handler?.({
          ctrlKey,
          preventDefault: () => {
            prevented = true;
          },
        } as unknown as WheelEvent);
        return prevented;
      },
      get options() {
        return options;
      },
      get removed() {
        return removed;
      },
    };
  }

  it("prevents the host's zoom on a pinch and nothing else", () => {
    const win = fakeWindow();
    installNativeZoomGuard(win.target);

    expect(win.dispatch(true)).toBe(true);
    expect(win.dispatch(false)).toBe(false);
  });

  it("registers non-passive, in the capture phase", () => {
    // A passive listener may not call preventDefault, so a passive
    // registration here would silently do nothing at all.
    const win = fakeWindow();
    installNativeZoomGuard(win.target);

    expect(win.options?.passive).toBe(false);
    expect(win.options?.capture).toBe(true);
  });

  it("returns a teardown that removes the listener", () => {
    const win = fakeWindow();
    installNativeZoomGuard(win.target)();
    expect(win.removed).toBe(true);
  });
});
