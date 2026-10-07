package com.devkit.codegraph;

/**
 * A JPA entity and the table it is stored in: {@code @Entity class
 * OrganizationApiEnablement} with no {@code @Table} is {@code
 * organization_api_enablement}, which is how Spring Boot names it.
 *
 * What the annotations say, not what the database holds: a column default, a
 * naming strategy the project replaced, or a Liquibase changelog that renamed
 * the table are all invisible here.
 */
public class EntityTable {
    /** The entity class's node id. */
    public String entity;
    /** The name JPQL uses for it: {@code @Entity(name)} or the simple class name. */
    public String entityName;
    public String table;
    public String schema = "";
    /** "primary" | "secondary" (@SecondaryTable) | "join" (@JoinTable) | "collection" (@CollectionTable). */
    public String kind = "primary";
    public String file;
    public int line;

    public EntityTable() {}
}
