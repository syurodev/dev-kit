import { describe, expect, test } from "bun:test";
import { quoteIdentifier, quoteLiteral } from "./identifier";

describe("quoteIdentifier", () => {
  test("MySQL uses backticks, Postgres double quotes", () => {
    expect(quoteIdentifier("users", "mysql")).toBe("`users`");
    expect(quoteIdentifier("users", "postgres")).toBe('"users"');
  });

  test("MariaDB quotes as MySQL", () => {
    expect(quoteIdentifier("users", "mariadb")).toBe("`users`");
  });

  // A reserved word is the everyday case, not an exotic one.
  test("a reserved word survives", () => {
    expect(quoteIdentifier("order", "postgres")).toBe('"order"');
  });

  // The quote character inside a name is the injection this exists to stop.
  // Both dialects escape it by doubling.
  test("an embedded quote is doubled, not dropped", () => {
    expect(quoteIdentifier('a"b', "postgres")).toBe('"a""b"');
    expect(quoteIdentifier("a`b", "mysql")).toBe("`a``b`");
  });

  // A name carrying a statement terminator must come out as one identifier,
  // never as two statements.
  //
  // A10: what makes that true is that every quote in the interior is part of a
  // doubled PAIR — an unpaired one would close the identifier where the name
  // did not intend it to, handing the rest of the name to the parser as SQL.
  // The original assertion looked for the byte pair `";` instead, which
  // doubling puts there by construction (`x";` becomes `x"";`), so it
  // contradicted the doubling test above and nothing could pass both.
  test("a name that looks like SQL stays a name", () => {
    const quoted = quoteIdentifier('x"; DROP TABLE users; --', "postgres");
    expect(quoted).toBe('"x""; DROP TABLE users; --"');
    expect(quoted.startsWith('"')).toBe(true);
    expect(quoted.endsWith('"')).toBe(true);
    // Strip the doubled pairs; a quote still standing would be one that ends
    // the identifier early.
    expect(quoted.slice(1, -1).split('""').join("")).not.toContain('"');
  });

  test("an empty name is refused rather than quoted into nothing", () => {
    expect(() => quoteIdentifier("", "postgres")).toThrow();
  });
});

describe("quoteLiteral", () => {
  test("an ordinary value is wrapped, nothing else", () => {
    expect(quoteLiteral("open", "postgres")).toBe("'open'");
  });

  // The everyday one, not an exotic one: this is what "Filter by this value"
  // on a customer named O'Brien produces.
  test("an apostrophe cannot end the literal early", () => {
    expect(quoteLiteral("O'Brien", "postgres")).toBe("'O''Brien'");
  });

  // Postgres reads a backslash as a backslash; MySQL reads it as an escape
  // unless NO_BACKSLASH_ESCAPES is set, so a trailing one would eat the
  // closing quote there and only there.
  test("a backslash is doubled for MySQL and left alone for Postgres", () => {
    expect(quoteLiteral("C:\\", "mysql")).toBe("'C:\\\\'");
    expect(quoteLiteral("C:\\", "postgres")).toBe("'C:\\'");
  });

  test("an empty value is an empty string, not a bare pair of quotes round nothing", () => {
    expect(quoteLiteral("", "postgres")).toBe("''");
  });
});
