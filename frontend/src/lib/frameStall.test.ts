import { expect, test } from "bun:test";
import { createStallDetector, describeStall, type Stall } from "./frameStall";

function run(feed: (d: ReturnType<typeof createStallDetector>) => void): Stall[] {
  const out: Stall[] = [];
  feed(createStallDetector((s) => out.push(s)));
  return out;
}

test("frames at 60 Hz, or a little over, are not stalls", () => {
  expect(
    run((d) => {
      for (const t of [0, 17, 34, 52, 100]) d.frame(t, 0);
    }),
  ).toEqual([]);
});

test("a gap past the threshold is a stall carrying how long the main thread was held", () => {
  const stalls = run((d) => {
    d.frame(0, 0);
    d.late(4);
    d.late(140);
    d.frame(180, 0);
  });
  expect(stalls).toEqual([{ gapMs: 180, blockedMs: 140, reactMs: 0 }]);
});

test("the late timer is counted per gap, not for ever", () => {
  const stalls = run((d) => {
    d.frame(0, 0);
    d.late(140);
    d.frame(20, 0);
    d.frame(100, 0);
  });
  expect(stalls).toEqual([{ gapMs: 80, blockedMs: 0, reactMs: 0 }]);
});

test("React time is the render clock's movement inside the gap", () => {
  const stalls = run((d) => {
    d.frame(0, 100);
    d.frame(90, 130);
  });
  expect(stalls[0]?.reactMs).toBe(30);
});

test("the input just before the gap is named; an older one is not", () => {
  const near = run((d) => {
    d.frame(1000, 0);
    d.input("wheel", "scroll-area", 1020);
    d.frame(1100, 0);
  });
  expect(near[0]?.input).toEqual({ type: "wheel", target: "scroll-area", agoMs: 0 });

  // Just ahead of the previous frame still counts: the frame it caused is the one that ran long.
  const before = run((d) => {
    d.input("keydown", "textarea", 950);
    d.frame(1000, 0);
    d.frame(1100, 0);
  });
  expect(before[0]?.input).toEqual({ type: "keydown", target: "textarea", agoMs: 50 });

  const old = run((d) => {
    d.input("keydown", "textarea", 100);
    d.frame(1000, 0);
    d.frame(1100, 0);
  });
  expect(old[0]?.input).toBeUndefined();
});

test("a hidden window's return is not a stall", () => {
  expect(
    run((d) => {
      d.frame(0, 0);
      d.reset();
      d.frame(60_000, 0);
    }),
  ).toEqual([]);
});

test("the line says whether the main thread or the compositor was late", () => {
  const held = describeStall({ gapMs: 200, blockedMs: 180, reactMs: 40 }, "agenthub › Session");
  expect(held).toContain("main thread held 180ms");
  expect(held).toContain('"agenthub › Session"');

  const free = describeStall(
    { gapMs: 200, blockedMs: 5, reactMs: 0, input: { type: "wheel", target: "scroll-area", agoMs: 3 } },
    "notes",
  );
  expect(free).toContain("paint or compositor");
  expect(free).toContain("after wheel on scroll-area");
});
