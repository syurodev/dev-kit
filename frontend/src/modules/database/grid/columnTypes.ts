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
  type LucideIcon,
} from "lucide-react";

/**
 * The glyph a column's datatype gets in the grid header.
 *
 * Buckets, not types. Postgres alone reports int2/int4/int8/serial/bigserial/
 * numeric/decimal/real/double precision for "a number", and a distinct glyph
 * for each would be a legend to memorise rather than something read at a
 * glance. What the glyph answers is "can I compare this, sort it, put quotes
 * round it" — which is the bucket.
 *
 * Kept as a pure name → icon map so it can be tested without rendering: the
 * header is 74 cells wide on a real table and the wrong bucket there is 74
 * wrong answers.
 */
export function typeGlyph(dataType: string | undefined): LucideIcon {
  const t = (dataType ?? "").toLowerCase().trim();
  if (!t) {
    return Columns3;
  }
  // An array of anything is an array first: `text[]` cannot be compared with
  // `=` the way its element type can, which is the thing the glyph is for.
  if (t.endsWith("[]") || t.startsWith("_")) {
    return Brackets;
  }
  if (t.includes("bool")) {
    return ToggleLeft;
  }
  if (t.includes("uuid") || t.includes("uniqueidentifier")) {
    return Fingerprint;
  }
  if (t.includes("json")) {
    return Braces;
  }
  // "binary" also catches varbinary and "binary large object".
  if (t.includes("bytea") || t.includes("blob") || t.includes("binary")) {
    return Binary;
  }
  // date, time, timestamp, timestamptz, datetime, interval, year.
  if (
    t.includes("date") ||
    t.includes("time") ||
    t.includes("interval") ||
    t === "year"
  ) {
    return CalendarClock;
  }
  if (
    t.includes("int") ||
    t.includes("serial") ||
    t.includes("numeric") ||
    t.includes("decimal") ||
    t.includes("float") ||
    t.includes("double") ||
    t.includes("real") ||
    t.includes("money") ||
    t.includes("bit")
  ) {
    return Hash;
  }
  if (
    t.includes("char") ||
    t.includes("text") ||
    t.includes("string") ||
    t.includes("enum") ||
    t.includes("clob")
  ) {
    return Type;
  }
  // A type nothing here recognises — a Postgres domain, a composite, an
  // extension type. Neutral rather than guessed: "column" is true of all of
  // them, and a wrong glyph is worse than no claim.
  return Columns3;
}
