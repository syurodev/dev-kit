/**
 * Quoting a table or column name into a statement.
 *
 * Every name here comes from the schema the server reported, so this is not
 * guarding against a hostile user — it is guarding against ordinary names the
 * SQL grammar cannot take bare: a reserved word, a space, a hyphen, a non-ASCII
 * letter. The doubling rule also means a name containing the quote character
 * cannot end the identifier early, which is the one case where an ordinary name
 * would otherwise become a second statement.
 */
export function quoteIdentifier(name: string, engine: string | undefined): string {
  if (!name) {
    throw new Error("cannot quote an empty identifier");
  }
  const quote = quoteCharFor(engine);
  return quote + name.split(quote).join(quote + quote) + quote;
}

/**
 * MySQL and MariaDB take backticks; everything else in this app is Postgres,
 * which takes the SQL-standard double quote.
 */
function quoteCharFor(engine: string | undefined): string {
  const normalised = (engine ?? "postgres").toLowerCase();
  return normalised === "mysql" || normalised === "mariadb" ? "`" : '"';
}

/**
 * Quoting a VALUE into a statement.
 *
 * Written for the grid's "Filter by this value": the value comes back from the
 * server as text and is put straight into a WHERE clause, so a name holding an
 * apostrophe — O'Brien, and every other ordinary one — would end the literal
 * early and leave the rest of it as SQL.
 *
 * MySQL and MariaDB also treat backslash as an escape inside a string unless
 * NO_BACKSLASH_ESCAPES is set, so a value ending in one would swallow the
 * closing quote there. Postgres does not, and doubling backslashes for it
 * would change the value, so the two engines are quoted differently.
 */
export function quoteLiteral(value: string, engine: string | undefined): string {
  const normalised = (engine ?? "postgres").toLowerCase();
  const escaped =
    normalised === "mysql" || normalised === "mariadb"
      ? value.split("\\").join("\\\\")
      : value;
  return "'" + escaped.split("'").join("''") + "'";
}
