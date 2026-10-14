import { describe, expect, test } from "bun:test";
import { focusStillWanted } from "./focusWhenInteractive";

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
