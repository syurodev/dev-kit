import { describe, expect, it } from "bun:test";
import {
  Binary,
  Braces,
  Brackets,
  CalendarClock,
  Columns3,
  Fingerprint,
  Hash,
  ToggleLeft,
  Type,
} from "lucide-react";
import { typeGlyph } from "./columnTypes";

describe("typeGlyph", () => {
  it.each([
    ["integer", Hash],
    ["int4", Hash],
    ["bigserial", Hash],
    ["numeric(12,2)", Hash],
    ["double precision", Hash],
    ["character varying(255)", Type],
    ["text", Type],
    ["TEXT", Type],
    ["boolean", ToggleLeft],
    ["tinyint(1)", Hash],
    ["timestamp with time zone", CalendarClock],
    ["date", CalendarClock],
    ["interval", CalendarClock],
    ["jsonb", Braces],
    ["uuid", Fingerprint],
    ["bytea", Binary],
    ["varbinary(16)", Binary],
    ["text[]", Brackets],
    ["_int4", Brackets],
  ])("%s", (type, expected) => {
    expect(typeGlyph(type)).toBe(expected);
  });

  // A console result has no catalogue behind it, and a type this does not know
  // is a domain or a composite. Both get the neutral glyph rather than a guess.
  it("claims nothing for a missing or unknown type", () => {
    expect(typeGlyph(undefined)).toBe(Columns3);
    expect(typeGlyph("")).toBe(Columns3);
    expect(typeGlyph("hstore")).toBe(Columns3);
  });

  // "timestamp" contains neither "int" nor "char", but "point" contains "int"
  // and "national character" contains "char" — the order of the tests is what
  // keeps a temporal column out of the number bucket.
  it("puts an array of numbers in the array bucket, not the number one", () => {
    expect(typeGlyph("integer[]")).toBe(Brackets);
  });
});
