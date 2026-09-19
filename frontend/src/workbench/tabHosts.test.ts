import { describe, expect, it } from "bun:test";
import { rememberScroll, restoreScroll } from "./tabHosts";

// bun:test has no DOM here, and these two functions touch very little of one:
// the stand-ins below are exactly that surface.
type FakeElement = { scrollTop: number; scrollLeft: number };

function fakeHost(children: FakeElement[], onScreen = true) {
  const live = new Set(children);
  const host = {
    isConnected: onScreen,
    parentNode: onScreen ? {} : null,
    querySelectorAll: () => [...live],
    contains: (el: FakeElement) => live.has(el),
  };
  return { host: host as unknown as HTMLElement, unmount: (el: FakeElement) => live.delete(el) };
}

/** What a browser does to every descendant when the host's box goes away. */
function dropOffsets(children: FakeElement[]) {
  for (const el of children) {
    el.scrollTop = 0;
    el.scrollLeft = 0;
  }
}

describe("tab host scroll memory", () => {
  it("puts a view back where it was after the browser dropped its offsets", () => {
    const editor = { scrollTop: 1240, scrollLeft: 0 };
    const grid = { scrollTop: 300, scrollLeft: 80 };
    const still = { scrollTop: 0, scrollLeft: 0 };
    const { host } = fakeHost([editor, grid, still]);

    rememberScroll(host);
    dropOffsets([editor, grid, still]);
    restoreScroll(host);

    expect(editor).toEqual({ scrollTop: 1240, scrollLeft: 0 });
    expect(grid).toEqual({ scrollTop: 300, scrollLeft: 80 });
    expect(still).toEqual({ scrollTop: 0, scrollLeft: 0 });
  });

  it("does not let a parked host overwrite what it remembered", () => {
    const editor = { scrollTop: 900, scrollLeft: 0 };
    const { host } = fakeHost([editor]);
    rememberScroll(host);
    dropOffsets([editor]);

    // Off screen every offset reads zero — recording now would record the loss.
    (host as unknown as { isConnected: boolean }).isConnected = false;
    rememberScroll(host);
    (host as unknown as { isConnected: boolean }).isConnected = true;

    restoreScroll(host);
    expect(editor.scrollTop).toBe(900);
  });

  it("restores once: a later adoption does not drag the view back", () => {
    const editor = { scrollTop: 500, scrollLeft: 0 };
    const { host } = fakeHost([editor]);
    rememberScroll(host);
    restoreScroll(host);

    editor.scrollTop = 40; // the user scrolled since
    restoreScroll(host);
    expect(editor.scrollTop).toBe(40);
  });

  it("skips an element its view has unmounted since", () => {
    const gone = { scrollTop: 700, scrollLeft: 0 };
    const { host, unmount } = fakeHost([gone]);
    rememberScroll(host);
    dropOffsets([gone]);
    unmount(gone);

    restoreScroll(host);
    expect(gone.scrollTop).toBe(0);
  });
});
