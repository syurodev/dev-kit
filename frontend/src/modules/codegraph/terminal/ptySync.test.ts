import { describe, expect, test } from "bun:test";
import { ptySizeSync } from "./ptySync";

function setup() {
  const grid = { id: "sess-1" as string | null, rows: 24, cols: 80 };
  const sent: [string, number, number][] = [];
  let fail = false;
  const sync = ptySizeSync(
    () => (grid.id ? { id: grid.id, rows: grid.rows, cols: grid.cols } : null),
    (id, rows, cols) => {
      sent.push([id, rows, cols]);
      return fail ? Promise.reject(new Error("bridge down")) : Promise.resolve();
    },
  );
  return { grid, sent, sync, failNext: (v: boolean) => (fail = v) };
}

const settle = () => new Promise((r) => setTimeout(r, 0));

describe("ptySizeSync", () => {
  test("tells the PTY only when the grid changed", () => {
    const { grid, sent, sync } = setup();
    sync.started(24, 80);
    sync();
    expect(sent).toEqual([]);
    grid.cols = 120;
    sync();
    sync();
    expect(sent).toEqual([["sess-1", 24, 120]]);
  });

  test("sends nothing before there is a session", () => {
    const { grid, sent, sync } = setup();
    grid.id = null;
    sync();
    expect(sent).toEqual([]);
  });

  test("the size the shell started with counts as told; a fit that landed meanwhile catches up", () => {
    const { grid, sent, sync } = setup();
    grid.cols = 100; // the webfont re-measure, while start was in flight
    sync.started(24, 80);
    sync();
    expect(sent).toEqual([["sess-1", 24, 100]]);
  });

  test("a resize the bridge refused is sent again on the next fit", async () => {
    const { grid, sent, sync, failNext } = setup();
    sync.started(24, 80);
    failNext(true);
    grid.cols = 120;
    sync();
    await settle();
    failNext(false);
    sync();
    expect(sent).toEqual([
      ["sess-1", 24, 120],
      ["sess-1", 24, 120],
    ]);
  });
});
