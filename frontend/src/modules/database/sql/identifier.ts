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
