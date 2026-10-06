import type { CellValue } from "./updateCell";

export type CellKind =
  | "text"
  | "boolean"
  | "integer"
  | "number"
  | "date"
  | "time"
  | "datetime"
  | "datetime-zone"
  | "time-zone"
  | "json"
  | "uuid";

/** Catalogue types, never guesses from one cell's contents. Domains/arrays stay text. */
export function cellKind(type?: string): CellKind {
  const t = (type ?? "").trim().toLowerCase();
  if (t.endsWith("[]") || t.startsWith("_")) return "text";
  if (/^(bool|boolean|tinyint\(1\)(?: unsigned)?)$/.test(t)) return "boolean";
  if (/^(json|jsonb)$/.test(t)) return "json";
  if (/^(uuid|uniqueidentifier)$/.test(t)) return "uuid";
  if (t === "date") return "date";
  if (/^(timestamptz|timestamp(?:\(\d+\))? with time zone)$/.test(t))
    return "datetime-zone";
  if (/^(timetz|time(?:\(\d+\))? with time zone)$/.test(t)) return "time-zone";
  if (/^(timestamp|datetime|datetime2)(\(\d+\))?( without time zone)?$/.test(t))
    return "datetime";
  if (/^time(\(\d+\))?( without time zone)?$/.test(t)) return "time";
  if (
    /^(smallint|integer|bigint|int[248]?|tinyint|mediumint|serial|bigserial|smallserial)( unsigned)?$/.test(
      t,
    )
  )
    return "integer";
  if (
    /^(numeric|decimal|float|float[48]|double(?: precision)?|real)(\([\d, ]+\))?( unsigned)?$/.test(
      t,
    )
  )
    return "number";
  return "text";
}

function validDate(value: string): boolean {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!m) return false;
  const [, y, mo, d] = m.map(Number);
  const days = [
    31,
    y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0) ? 29 : 28,
    31,
    30,
    31,
    30,
    31,
    31,
    30,
    31,
    30,
    31,
  ];
  return y > 0 && mo > 0 && mo <= 12 && d > 0 && d <= days[mo - 1];
}
function validTime(value: string, duration = false, endOfDay = false): boolean {
  if (endOfDay && /^24:00(?::00(?:\.0{1,6})?)?$/.test(value)) return true;
  const m = /^(-?)(\d{2,3}):(\d{2})(?::(\d{2})(\.\d{1,6})?)?$/.exec(value);
  return Boolean(
    m &&
    (duration || !m[1]) &&
    Number(m[2]) <= (duration ? 838 : 23) &&
    Number(m[3]) < 60 &&
    Number(m[4] ?? 0) < 60,
  );
}
export function splitOffset(value: string): { local: string; offset: string } {
  const m = /^(.*\d)(Z|[+-]\d{2}(?::?\d{2})?)$/i.exec(value.trim());
  return m && m[1].includes(":")
    ? { local: m[1].trim().replace(" ", "T"), offset: m[2] }
    : { local: value.replace(" ", "T"), offset: "" };
}
function validOffset(offset: string): boolean {
  if (/^z$/i.test(offset)) return true;
  const m = /^[+-](\d{2})(?::?(\d{2}))?$/.exec(offset);
  return Boolean(m && Number(m[1]) <= 15 && Number(m[2] ?? 0) < 60);
}
export function booleanValue(value: string): string {
  const t = value.trim().toLowerCase();
  if (["true", "t", "1"].includes(t)) return "true";
  if (["false", "f", "0"].includes(t)) return "false";
  return value;
}
/** Preserve decimal precision, offset, fractional seconds and JSON source text. No JS Date/Number conversion. */
export function validateCellValue(
  value: CellValue,
  type?: string,
  nullable = true,
  engine?: string,
): string {
  if (value === null) return nullable ? "" : "This column does not allow NULL.";
  const kind = cellKind(type);
  if (kind === "boolean")
    return ["true", "false"].includes(booleanValue(value))
      ? ""
      : "Choose true or false.";
  if (kind === "integer")
    return /^[+-]?\d+$/.test(value) ? "" : "Enter a whole number.";
  if (kind === "number")
    return /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(value)
      ? ""
      : "Enter a number; precision is kept as typed.";
  if (kind === "json") {
    try {
      JSON.parse(value);
      return "";
    } catch {
      return "Enter valid JSON.";
    }
  }
  if (kind === "uuid")
    return /^[\da-f]{8}-(?:[\da-f]{4}-){3}[\da-f]{12}$/i.test(value)
      ? ""
      : "Enter a UUID (8-4-4-4-12).";
  // PostgreSQL special temporal values have no calendar representation.
  if (
    ["date", "datetime", "datetime-zone"].includes(kind) &&
    engine !== "mysql" &&
    engine !== "mariadb" &&
    /^-?infinity$/.test(value)
  )
    return "";
  if (kind === "date")
    return validDate(value) ? "" : "Use a valid YYYY-MM-DD date.";
  if (kind === "time")
    return validTime(
      value,
      engine === "mysql" || engine === "mariadb",
      engine !== "mysql" && engine !== "mariadb",
    )
      ? ""
      : "Use HH:mm[:ss[.ffffff]].";
  if (kind === "time-zone") {
    const { local, offset } = splitOffset(value);
    return validTime(local, false, true) && validOffset(offset)
      ? ""
      : "Include a time and UTC offset, e.g. 14:30:00+07:00.";
  }
  if (kind === "datetime" || kind === "datetime-zone") {
    const { local, offset } =
      kind === "datetime-zone"
        ? splitOffset(value)
        : { local: value.replace(" ", "T"), offset: "" };
    const [date, time, ...extra] = local.split("T");
    return !extra.length &&
      validDate(date) &&
      validTime(time ?? "") &&
      (kind !== "datetime-zone" || validOffset(offset))
      ? ""
      : `Use a valid date and time${kind === "datetime-zone" ? " with an explicit UTC offset" : ""}.`;
  }
  return "";
}

/** Skip semantic boolean/temporal no-ops without rounding or timezone conversion. */
export function sameCellValue(
  a: CellValue,
  b: CellValue,
  type?: string,
): boolean {
  if (a === null || b === null) return a === b;
  const kind = cellKind(type);
  if (kind === "boolean") return booleanValue(a) === booleanValue(b);
  if (
    ["date", "time", "datetime", "datetime-zone", "time-zone"].includes(kind)
  ) {
    const canonical = (v: string) =>
      v
        .replace("T", " ")
        .replace(/(\d{2}:\d{2})(?=$|Z|[+-]\d{2})/, "$1:00")
        .replace(/(\.\d*?)0+(?=$|Z|[+-]\d{2})/, "$1")
        .replace(/\.(?=$|Z|[+-]\d{2})/, "");
    return canonical(a) === canonical(b);
  }
  return a === b;
}
