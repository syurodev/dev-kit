package com.devkit.codegraph;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Set;

/**
 * The tables a SQL or JPQL statement reads and writes, found by reading its
 * tokens — not by parsing it.
 *
 * A grammar per dialect would be the honest way to do this and the wrong
 * amount of work: the question is only "which names follow FROM, JOIN, UPDATE,
 * INTO", and the tokens answer it for everything an application writes inline.
 * What keeps the shortcut honest is what it refuses: string literals, comments
 * and quoted identifiers are tokens of their own (so {@code 'from x'} names no
 * table), {@code EXTRACT(year FROM d)} and {@code IS DISTINCT FROM} are not a
 * FROM clause, {@code FOR UPDATE} and {@code ON DUPLICATE KEY UPDATE} are not
 * an UPDATE, a CTE's name is not a table, and a subquery in the FROM position
 * names none (its own FROM is found when the scan reaches it).
 *
 * Access follows the clause: FROM, JOIN and USING read; UPDATE, INSERT INTO,
 * DELETE [FROM], MERGE INTO, TRUNCATE and the table DDL write. So
 * {@code INSERT INTO a SELECT .. FROM b} writes a and reads b.
 *
 * In JPQL the names are entities, and a dotted path after JOIN
 * ({@code join o.apis a}) is an association, not a table: it is skipped. The
 * caller maps entity names to tables.
 */
final class SqlTables {

    record Ref(String schema, String name, String access) {}

    private static final Set<String> NOT_ALIAS = Set.of(
            "WHERE", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "NATURAL", "OUTER", "ON", "GROUP", "ORDER",
            "HAVING", "LIMIT", "OFFSET", "UNION", "INTERSECT", "EXCEPT", "SET", "VALUES", "SELECT", "USING", "FOR",
            "RETURNING", "FETCH", "WINDOW", "LATERAL", "STRAIGHT_JOIN", "WITH", "WHEN", "THEN", "ELSE", "AND", "OR");
    /** A FROM inside these is part of the function, not a table clause. */
    private static final Set<String> FROM_IN_FUNCTION = Set.of("EXTRACT", "TRIM", "SUBSTRING", "OVERLAY", "POSITION");
    /** Words that may sit between a clause keyword and the table it names. */
    private static final Set<String> MODIFIERS = Set.of("ONLY", "LATERAL", "FETCH", "TABLE", "IF", "NOT", "EXISTS");

    private SqlTables() {}

    /** The tables the statement touches, in order of appearance, each access once. */
    static List<Ref> parse(String sql, boolean jpql) {
        List<String> t = tokens(sql);
        Set<String> ctes = cteNames(t);
        Set<Ref> found = new LinkedHashSet<>();
        List<String> parens = new ArrayList<>(); // for each open paren, the word before it
        String previous = "";
        for (int i = 0; i < t.size(); i++) {
            String tok = t.get(i);
            String kw = isWord(tok) ? tok.toUpperCase(Locale.ROOT) : "";
            switch (tok) {
                case "(" -> parens.add(isWord(previous) ? previous.toUpperCase(Locale.ROOT) : "");
                case ")" -> {
                    if (!parens.isEmpty()) {
                        parens.remove(parens.size() - 1);
                    }
                }
                default -> { }
            }
            switch (kw) {
                case "FROM" -> {
                    boolean inFunction = !parens.isEmpty() && FROM_IN_FUNCTION.contains(parens.get(parens.size() - 1));
                    if (!inFunction && !previous.equalsIgnoreCase("DISTINCT")) {
                        refs(t, i + 1, previous.equalsIgnoreCase("DELETE") ? "write" : "read", true, jpql, ctes, found);
                    }
                }
                case "JOIN", "USING" -> {
                    // `JOIN t USING (col)` is a column list; `DELETE .. USING t` is a table.
                    if (!(kw.equals("USING") && "(".equals(next(t, i)))) {
                        refs(t, i + 1, "read", false, jpql, ctes, found);
                    }
                }
                case "UPDATE" -> {
                    String after = next(t, i);
                    // FOR UPDATE, ON DUPLICATE KEY UPDATE and DO UPDATE SET are not a table clause.
                    boolean notAClause = previous.equalsIgnoreCase("FOR") || previous.equalsIgnoreCase("KEY")
                            || previous.equalsIgnoreCase("DO") || after.equalsIgnoreCase("SET");
                    if (!notAClause) {
                        refs(t, i + 1, "write", false, jpql, ctes, found);
                    }
                }
                case "INTO" -> {
                    String before = previous.toUpperCase(Locale.ROOT);
                    if (before.equals("INSERT") || before.equals("MERGE") || before.equals("REPLACE")) {
                        refs(t, i + 1, "write", false, jpql, ctes, found);
                    }
                }
                case "DELETE" -> {
                    // JPQL allows `DELETE Entity e` without FROM.
                    String after = next(t, i);
                    if (jpql && isWord(after) && !after.equalsIgnoreCase("FROM")) {
                        refs(t, i + 1, "write", false, jpql, ctes, found);
                    }
                }
                case "TABLE" -> {
                    String before = previous.toUpperCase(Locale.ROOT);
                    if (before.equals("TRUNCATE") || before.equals("CREATE") || before.equals("ALTER") || before.equals("DROP")) {
                        refs(t, i + 1, "write", false, jpql, ctes, found);
                    }
                }
                case "TRUNCATE" -> {
                    if (!next(t, i).equalsIgnoreCase("TABLE")) {
                        refs(t, i + 1, "write", false, jpql, ctes, found);
                    }
                }
                default -> { }
            }
            previous = tok;
        }
        return new ArrayList<>(found);
    }

    /**
     * Reads the table reference(s) at {@code at}: {@code [schema.]name [[AS] alias]}, and for a FROM
     * list the comma-joined ones after it.
     */
    private static void refs(List<String> t, int at, String access, boolean list, boolean jpql,
                             Set<String> ctes, Set<Ref> out) {
        int i = at;
        while (true) {
            while (i < t.size() && isWord(t.get(i)) && MODIFIERS.contains(t.get(i).toUpperCase(Locale.ROOT))) {
                i++;
            }
            if (i >= t.size() || !isWord(t.get(i))) {
                return; // a subquery, a VALUES list, a parameter: no table here
            }
            List<String> parts = new ArrayList<>();
            parts.add(t.get(i++));
            while (i + 1 < t.size() && t.get(i).equals(".") && isWord(t.get(i + 1))) {
                parts.add(t.get(i + 1));
                i += 2;
            }
            String name = parts.get(parts.size() - 1);
            String schema = parts.size() > 1 ? parts.get(parts.size() - 2) : "";
            boolean cte = parts.size() == 1 && ctes.contains(name.toLowerCase(Locale.ROOT));
            // JPQL: the first word of a dotted join is an alias — an association path, not an entity.
            boolean associationPath = jpql && parts.size() > 1 && !list && access.equals("read");
            if (!associationPath && !cte && !isKeyword(name)) {
                out.add(new Ref(jpql ? "" : schema, name, access));
            }
            // An alias: `AS x`, or a bare word that is not the next clause.
            if (i < t.size() && t.get(i).equalsIgnoreCase("AS")) {
                i += 2;
            } else if (i < t.size() && isWord(t.get(i)) && !NOT_ALIAS.contains(t.get(i).toUpperCase(Locale.ROOT))) {
                i++;
            }
            if (list && i < t.size() && t.get(i).equals(",")) {
                i++;
                continue;
            }
            return;
        }
    }

    private static boolean isKeyword(String word) {
        return NOT_ALIAS.contains(word.toUpperCase(Locale.ROOT));
    }

    private static String next(List<String> t, int i) {
        return i + 1 < t.size() ? t.get(i + 1) : "";
    }

    /** The names of the CTEs a WITH clause defines: {@code WITH a AS (..), b AS (..)}. */
    private static Set<String> cteNames(List<String> t) {
        Set<String> names = new HashSet<>();
        if (t.isEmpty() || !t.get(0).equalsIgnoreCase("WITH")) {
            return names;
        }
        for (int i = 1; i + 2 < t.size(); i++) {
            if (isWord(t.get(i)) && t.get(i + 1).equalsIgnoreCase("AS")) {
                int j = i + 2;
                while (j < t.size() && (t.get(j).equalsIgnoreCase("NOT") || t.get(j).equalsIgnoreCase("MATERIALIZED"))) {
                    j++;
                }
                if (j < t.size() && t.get(j).equals("(")) {
                    names.add(t.get(i).toLowerCase(Locale.ROOT));
                }
            }
        }
        return names;
    }

    // --- tokens ---------------------------------------------------------------------------------

    /** Words, quoted identifiers (unquoted), and single-character punctuation; strings, comments and parameters dropped. */
    static List<String> tokens(String sql) {
        List<String> out = new ArrayList<>();
        int n = sql.length();
        int i = 0;
        while (i < n) {
            char c = sql.charAt(i);
            if (Character.isWhitespace(c)) {
                i++;
            } else if (c == '-' && i + 1 < n && sql.charAt(i + 1) == '-') {
                while (i < n && sql.charAt(i) != '\n') {
                    i++;
                }
            } else if (c == '/' && i + 1 < n && sql.charAt(i + 1) == '*') {
                int end = sql.indexOf("*/", i + 2);
                i = end < 0 ? n : end + 2;
            } else if (c == '\'') {
                i++;
                while (i < n) {
                    if (sql.charAt(i) == '\'') {
                        if (i + 1 < n && sql.charAt(i + 1) == '\'') {
                            i += 2;
                            continue;
                        }
                        break;
                    }
                    i++;
                }
                i++;
                out.add("?"); // a literal stands where a value goes, never a name
            } else if (c == '"' || c == '`' || c == '[') {
                char close = c == '[' ? ']' : c;
                int end = sql.indexOf(close, i + 1);
                if (end < 0) {
                    break;
                }
                out.add(sql.substring(i + 1, end));
                i = end + 1;
            } else if (c == ':' && i + 1 < n && (Character.isLetter(sql.charAt(i + 1)) || sql.charAt(i + 1) == '_')) {
                i++; // a named parameter, not a name — but `::` casts keep their type token out too
                while (i < n && (Character.isLetterOrDigit(sql.charAt(i)) || sql.charAt(i) == '_')) {
                    i++;
                }
                out.add("?");
            } else if (c == ':') {
                i++; // part of a `::` cast: the type after it is not a table
                if (i < n && sql.charAt(i) == ':') {
                    i++;
                    while (i < n && (Character.isLetterOrDigit(sql.charAt(i)) || sql.charAt(i) == '_')) {
                        i++;
                    }
                }
            } else if (Character.isLetter(c) || c == '_') {
                int start = i;
                while (i < n && (Character.isLetterOrDigit(sql.charAt(i)) || sql.charAt(i) == '_' || sql.charAt(i) == '$')) {
                    i++;
                }
                out.add(sql.substring(start, i));
            } else if (c == '?') {
                i++; // `?` and the positional `?1`
                while (i < n && Character.isDigit(sql.charAt(i))) {
                    i++;
                }
                out.add("?");
            } else if (Character.isDigit(c)) {
                while (i < n && (Character.isLetterOrDigit(sql.charAt(i)) || sql.charAt(i) == '.')) {
                    i++;
                }
                out.add("?");
            } else {
                out.add(String.valueOf(c));
                i++;
            }
        }
        return out;
    }

    private static boolean isWord(String token) {
        if (token.isEmpty()) {
            return false;
        }
        char c = token.charAt(0);
        return Character.isLetter(c) || c == '_' || token.length() > 1;
    }
}
