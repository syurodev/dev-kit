import { describe, expect, test } from "bun:test";
import { moveHost } from "./hostMove";

/** Just enough of an element: a parent, scroll offsets, and the two moves. */
type Fake = {
  parentNode: Fake | null;
  isConnected: boolean;
  scrollTop: number;
  scrollLeft: number;
  children: Fake[];
  querySelectorAll: () => Fake[];
  appendChild: (child: Fake) => void;
  moveBefore?: (child: Fake, ref: null) => void;
};

function el(opts: { connected?: boolean; scrollTop?: number; children?: Fake[] } = {}): Fake {
  const node: Fake = {
    parentNode: null,
    isConnected: opts.connected ?? true,
    scrollTop: opts.scrollTop ?? 0,
    scrollLeft: 0,
    children: opts.children ?? [],
    querySelectorAll: () => node.children,
    appendChild(child) {
      // A real removal from the document resets every scroll box under it.
      child.scrollTop = 0;
      for (const c of child.children) c.scrollTop = 0;
      child.parentNode = node;
    },
  };
  return node;
}

const move = (parent: Fake, host: Fake) => moveHost(parent as unknown as Element, host as unknown as Element);

describe("moveHost", () => {
  test("puts a scrolled transcript back where it was after a plain re-parent", () => {
    const transcript = el({ scrollTop: 840 });
    const host = el({ children: [transcript] });
    const park = el();
    move(park, host);
    expect(host.parentNode).toBe(park);
    expect(transcript.scrollTop).toBe(840);
  });

  test("uses moveBefore where the engine has it, and touches no offsets", () => {
    const transcript = el({ scrollTop: 840 });
    const host = el({ children: [transcript] });
    const card = el();
    const calls: Fake[] = [];
    card.moveBefore = (child) => {
      calls.push(child);
      child.parentNode = card;
    };
    move(card, host);
    expect(calls).toEqual([host]);
    expect(transcript.scrollTop).toBe(840);
  });

  test("does nothing when the host is already there", () => {
    const card = el();
    const host = el();
    host.parentNode = card;
    let appended = false;
    card.appendChild = () => void (appended = true);
    move(card, host);
    expect(appended).toBe(false);
  });

  test("a host never attached has nothing to remember", () => {
    const host = el({ connected: false });
    const card = el();
    move(card, host);
    expect(host.parentNode).toBe(card);
  });
});
