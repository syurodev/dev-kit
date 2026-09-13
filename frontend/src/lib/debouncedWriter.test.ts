import { expect, test } from "bun:test";
import { createDebouncedWriter } from "./debouncedWriter";

const tick = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

// #50. Two stores writing inside one window both land.
test("a write to one key does not cancel a write to another", async () => {
  const written: [string, number][] = [];
  const writer = createDebouncedWriter<number>((key, value) => written.push([key, value]), 10);

  writer.schedule("databaseFolders", 1);
  writer.schedule("workbenchTabs", 2);
  await tick(30);

  expect(written).toEqual([
    ["databaseFolders", 1],
    ["workbenchTabs", 2],
  ]);
});

test("the last write to a key inside the window wins", async () => {
  const written: [string, number][] = [];
  const writer = createDebouncedWriter<number>((key, value) => written.push([key, value]), 10);

  writer.schedule("appearance", 1);
  writer.schedule("appearance", 2);
  await tick(30);

  expect(written).toEqual([["appearance", 2]]);
});

test("flush writes what is waiting, once", async () => {
  const written: [string, number][] = [];
  const writer = createDebouncedWriter<number>((key, value) => written.push([key, value]), 1000);

  writer.schedule("a", 1);
  writer.flush();
  await tick(20);

  expect(written).toEqual([["a", 1]]);
});
