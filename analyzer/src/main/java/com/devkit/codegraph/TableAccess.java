package com.devkit.codegraph;

/**
 * One place that reads or writes a database table, as far as the source says.
 *
 * {@code from} is the node that does it: the repository method that declares
 * the query, or the method that calls JdbcTemplate or the EntityManager. A
 * method that calls a repository is not named here — the call edge already
 * says that, and a walk up the call graph from {@code from} finds it.
 */
public class TableAccess {
    public String from;
    public String table;
    public String schema = "";
    /** "read" | "write". */
    public String access;
    /**
     * "repository-query" (a @Query), "repository-derived" (a findByX method),
     * "repository-crud" (save/findById… inherited from Spring Data, recorded at the
     * call), "jdbc" (a SQL string passed to query/update/execute), "entity-manager"
     * (createQuery/createNativeQuery).
     */
    public String via;
    /** The statement, whitespace folded and clipped; or the repository method for the inherited ones. */
    public String detail = "";
    public String file;
    public int line;
    /** The column of the call's name when the row is read off a call (see Steps.col); 0 when it is read off a declaration. */
    public int col;

    public TableAccess() {}
}
