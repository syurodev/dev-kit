import { expect, test } from "bun:test";
import { parseNoteResult } from "./noteResult";

test("parses notes.create's Summary shape (id + title; other fields pass through unread)", () => {
  const result = parseNoteResult({ id: "n1", title: "Meeting notes", preview: "decrypted body text…" });
  expect(result?.id).toBe("n1");
  expect(result?.title).toBe("Meeting notes");
});

test("parses notes.update's full Note shape the same way", () => {
  const result = parseNoteResult({ id: "n1", title: "Meeting notes", body: "the whole decrypted note" });
  expect(result?.id).toBe("n1");
  expect(result?.title).toBe("Meeting notes");
});

test("returns null when id or title is missing", () => {
  expect(parseNoteResult({ title: "x" })).toBeNull();
  expect(parseNoteResult({ id: "n1" })).toBeNull();
});
