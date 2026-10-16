import { beforeEach, describe, expect, test } from "bun:test";
import { cachedPage, forgetPages, pageKey, rememberPage } from "./pdfPageCache";

const image = (page: number) => ({ page, src: `data:image/png;base64,${page}`, width: 1, height: 1 });

describe("pdf page cache", () => {
  beforeEach(forgetPages);

  test("a page comes back for the same revision and resolution only", () => {
    rememberPage(pageKey("f", "a.pdf", "r1", 3, 96), image(3));
    expect(cachedPage(pageKey("f", "a.pdf", "r1", 3, 96))?.page).toBe(3);
    expect(cachedPage(pageKey("f", "a.pdf", "r2", 3, 96))).toBeUndefined();
    expect(cachedPage(pageKey("f", "a.pdf", "r1", 3, 120))).toBeUndefined();
    expect(cachedPage(pageKey("g", "a.pdf", "r1", 3, 96))).toBeUndefined();
  });

  test("keeps the most recently used pages when it is full", () => {
    for (let p = 1; p <= 24; p++) rememberPage(pageKey("f", "a.pdf", "r", p, 96), image(p));
    cachedPage(pageKey("f", "a.pdf", "r", 1, 96)); // page 1 is read again, so page 2 is the oldest
    rememberPage(pageKey("f", "a.pdf", "r", 25, 96), image(25));
    expect(cachedPage(pageKey("f", "a.pdf", "r", 2, 96))).toBeUndefined();
    expect(cachedPage(pageKey("f", "a.pdf", "r", 1, 96))?.page).toBe(1);
    expect(cachedPage(pageKey("f", "a.pdf", "r", 25, 96))?.page).toBe(25);
  });
});
