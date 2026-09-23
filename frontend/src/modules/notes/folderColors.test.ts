import { describe, expect, test } from "bun:test";
import { folderDotVar, isHexColor, noteColorValue } from "./folderColors";
import { noteCardSurface } from "./noteColor";

describe("note colours", () => {
  test("a preset maps to its theme token", () => {
    expect(noteColorValue("blue")).toBe("var(--note-folder-blue)");
  });

  test("a stored hex colour is used as is", () => {
    expect(noteColorValue("#3b82f6")).toBe("#3b82f6");
    expect(noteCardSurface("#3b82f6").backgroundColor).toBe("color-mix(in srgb, #3b82f6 26%, transparent)");
  });

  test("anything else never reaches a style", () => {
    for (const bad of ["#3B82F6", "#abc", "red;background:url(x)", "rgb(1,2,3)", ""]) {
      expect(isHexColor(bad)).toBe(false);
      expect(noteColorValue(bad)).toBeUndefined();
    }
    expect(folderDotVar("red;x")).toBe("var(--note-folder-gray)");
  });
});
