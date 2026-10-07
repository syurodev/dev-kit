package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.stream.Collectors;

import static org.junit.jupiter.api.Assertions.*;

class TableTest {

    private static void write(Path root, String relative, String content) throws IOException {
        Path file = root.resolve(relative);
        Files.createDirectories(file.getParent());
        Files.writeString(file, content);
    }

    /** "from -> access:table via" for every access, sorted — what the index says in one comparable list. */
    private static List<String> accesses(AnalysisResult r) {
        return r.tableAccesses.stream()
                .map(a -> a.from.replaceAll("^acme\\.", "") + " -> " + a.access + ":" + a.table + " " + a.via)
                .sorted().collect(Collectors.toList());
    }

    private static EntityTable entity(AnalysisResult r, String entityId, String kind) {
        return r.entityTables.stream().filter(e -> e.entity.equals(entityId) && e.kind.equals(kind)).findFirst()
                .orElseThrow(() -> new AssertionError("no " + kind + " table of " + entityId + " in "
                        + r.entityTables.stream().map(e -> e.entity + ":" + e.kind + ":" + e.table).toList()));
    }

    private static final String ENTITIES = """
            package acme;
            import jakarta.persistence.*;
            import java.util.Set;
            @Entity
            class OrganizationApiEnablement { @Id Long id; String org; }
            @Entity
            @Table(name = "orgs", schema = "core")
            class Organization {
                @Id Long id;
                @ManyToMany @JoinTable(name = "org_member") Set<Organization> members;
                @ElementCollection @CollectionTable(name = "org_tags") Set<String> tags;
            }
            @Entity(name = "Api")
            class ApiDefinition { @Id Long id; }
            @Entity
            @SecondaryTable(name = "invoice_extra")
            class HTTPInvoiceV2 { @Id Long id; }
            """;

    @Test
    void entitiesMapToTablesTheWaySpringBootNamesThem(@TempDir Path root) throws IOException {
        write(root, "src/main/java/acme/Entities.java", ENTITIES);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();

        assertEquals("organization_api_enablement", entity(r, "acme.OrganizationApiEnablement", "primary").table);
        EntityTable orgs = entity(r, "acme.Organization", "primary");
        assertEquals("orgs", orgs.table);
        assertEquals("core", orgs.schema);
        assertEquals("Organization", orgs.entityName);
        assertEquals("org_member", entity(r, "acme.Organization", "join").table);
        assertEquals("org_tags", entity(r, "acme.Organization", "collection").table);
        assertEquals("Api", entity(r, "acme.ApiDefinition", "primary").entityName, "@Entity(name) is the JPQL name");
        assertEquals("api_definition", entity(r, "acme.ApiDefinition", "primary").table);
        assertEquals("http_invoice_v2", entity(r, "acme.HTTPInvoiceV2", "primary").table);
        assertEquals("invoice_extra", entity(r, "acme.HTTPInvoiceV2", "secondary").table);
    }

    @Test
    void snakeCase() {
        assertEquals("organization_api_enablement", TableExtractor.snake("OrganizationApiEnablement"));
        assertEquals("http_invoice_v2", TableExtractor.snake("HTTPInvoiceV2"));
        assertEquals("user", TableExtractor.snake("User"));
        assertEquals("a_b", TableExtractor.snake("aB"));
    }

    @Test
    void repositoryQueriesDerivedMethodsAndInheritedCrud(@TempDir Path root) throws IOException {
        write(root, "src/main/java/acme/Entities.java", ENTITIES);
        write(root, "src/main/java/acme/OrgRepo.java", """
                package acme;
                import org.springframework.data.jpa.repository.*;
                import org.springframework.data.repository.query.Param;
                interface BaseRepo<T, ID> extends JpaRepository<T, ID> {}
                interface OrgRepo extends BaseRepo<Organization, Long> {
                    @Query("select o from Organization o join o.members m where m.id = :id")
                    java.util.List<Organization> byMember(@Param("id") Long id);

                    @Modifying
                    @Query("update Organization o set o.id = :id where o.id = :old")
                    int renumber(Long id, Long old);

                    @Query(value = "select * from core.orgs o join org_member m on m.org = o.id where o.id = ?1", nativeQuery = true)
                    java.util.List<Organization> nativeFind(Long id);

                    @Query("select count(o) from #{#entityName} o")
                    long howMany();

                    java.util.Optional<Organization> findByName(String name);
                    boolean existsByName(String name);
                    void deleteByName(String name);
                    default int twice() { return 2; }
                }
                """);
        write(root, "src/main/java/acme/EnablementRepo.java", """
                package acme;
                import org.springframework.data.repository.CrudRepository;
                interface EnablementRepo extends CrudRepository<OrganizationApiEnablement, Long> {
                    @org.springframework.data.jpa.repository.Query(value = "delete from organization_api_enablement where org = ?1", nativeQuery = true)
                    void purge(String org);
                }
                """);
        write(root, "src/main/java/acme/EnablementService.java", """
                package acme;
                class EnablementService {
                    private final EnablementRepo repo = null;
                    private final OrgRepo orgs = null;
                    void enable(OrganizationApiEnablement e) { repo.save(e); }
                    boolean has(Long id) { return repo.existsById(id) && orgs.count() > 0; }
                    void drop(String org) { repo.purge(org); }
                    void remove(Long id) { repo.deleteById(id); }
                }
                """);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();
        List<String> got = accesses(r);

        // @Query, JPQL: entity names go through the entity map; an association path names no table.
        assertTrue(got.contains("OrgRepo.byMember(java.lang.Long) -> read:orgs repository-query"), got.toString());
        assertFalse(got.stream().anyMatch(s -> s.contains("members")), "an association path is not a table");
        assertTrue(got.contains("OrgRepo.renumber(java.lang.Long, java.lang.Long) -> write:orgs repository-query"), got.toString());
        // @Query, native: the names as written.
        assertTrue(got.contains("OrgRepo.nativeFind(java.lang.Long) -> read:orgs repository-query"), got.toString());
        assertTrue(got.contains("OrgRepo.nativeFind(java.lang.Long) -> read:org_member repository-query"), got.toString());
        assertTrue(got.contains("OrgRepo.howMany() -> read:orgs repository-query"), "#{#entityName} is the managed entity");
        assertTrue(got.contains("EnablementRepo.purge(java.lang.String) -> write:organization_api_enablement repository-query"), got.toString());
        // Derived queries: the verb says read or write; the table is the repository's entity.
        assertTrue(got.contains("OrgRepo.findByName(java.lang.String) -> read:orgs repository-derived"), got.toString());
        assertTrue(got.contains("OrgRepo.existsByName(java.lang.String) -> read:orgs repository-derived"), got.toString());
        assertTrue(got.contains("OrgRepo.deleteByName(java.lang.String) -> write:orgs repository-derived"), got.toString());
        assertFalse(got.stream().anyMatch(s -> s.startsWith("OrgRepo.twice")), "a default method has a body, not a query");
        // Inherited CRUD is recorded where it is called; a declared method is not CRUD.
        assertTrue(got.contains("EnablementService.enable(acme.OrganizationApiEnablement) -> write:organization_api_enablement repository-crud"), got.toString());
        assertTrue(got.contains("EnablementService.has(java.lang.Long) -> read:organization_api_enablement repository-crud"), got.toString());
        assertTrue(got.contains("EnablementService.has(java.lang.Long) -> read:orgs repository-crud"), got.toString());
        assertTrue(got.contains("EnablementService.remove(java.lang.Long) -> write:organization_api_enablement repository-crud"), got.toString());
        assertFalse(got.stream().anyMatch(s -> s.startsWith("EnablementService.drop") ),
                "purge is declared on the repository; its table access belongs to purge, which drop calls");
        // The caller of a declared repository method is found by the call edge, not recorded twice.
        assertTrue(r.edges.stream().anyMatch(e -> e.from.endsWith("EnablementService.drop(java.lang.String)")
                && e.to.endsWith("EnablementRepo.purge(java.lang.String)")));
    }

    @Test
    void jdbcAndEntityManagerStrings(@TempDir Path root) throws IOException {
        write(root, "src/main/java/acme/Entities.java", ENTITIES);
        write(root, "src/main/java/acme/Dao.java", """
                package acme;
                class Dao {
                    static final String TABLE = "organization_api_enablement";
                    static final String COUNT = "select count(*) from " + TABLE + " where org = ?";
                    private final Jdbc jdbc = null;
                    private final Em em = null;
                    interface Jdbc { int update(String sql, Object... args); Object queryForObject(String sql, Class<?> t, Object... args); }
                    interface Em { Object createQuery(String q); Object createNativeQuery(String q); }

                    int disable(String org) { return jdbc.update("update organization_api_enablement set enabled = false where org = ?", org); }
                    Object count(String org) { return jdbc.queryForObject(COUNT, Long.class, org); }
                    Object jpql() { return em.createQuery("select o from Organization o where o.id = :id"); }
                    Object nativeSql() { return em.createNativeQuery("insert into audit_log (msg) values (?)"); }
                    Object dynamic(String t) { return jdbc.queryForObject("select * from " + t, Object.class); }
                    Object notSql(String s) { return jdbc.update("hello world, this is not a statement", s); }
                }
                """);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();
        List<String> got = accesses(r);

        assertTrue(got.contains("Dao.disable(java.lang.String) -> write:organization_api_enablement jdbc"), got.toString());
        assertTrue(got.contains("Dao.count(java.lang.String) -> read:organization_api_enablement jdbc"), "a constant built from constants: " + got);
        assertTrue(got.contains("Dao.jpql() -> read:orgs entity-manager"), got.toString());
        assertTrue(got.contains("Dao.nativeSql() -> write:audit_log entity-manager"), got.toString());
        assertFalse(got.stream().anyMatch(s -> s.startsWith("Dao.dynamic")), "a table named only by a variable is not guessed");
        assertFalse(got.stream().anyMatch(s -> s.startsWith("Dao.notSql")), "a string that is not a statement is not one");
    }

    @Test
    void aRepositoryOfSomethingThatIsNotAnEntityNamesNoTable(@TempDir Path root) throws IOException {
        write(root, "src/main/java/acme/Things.java", """
                package acme;
                import org.springframework.data.repository.CrudRepository;
                class Thing { Long id; }
                interface ThingRepo extends CrudRepository<Thing, Long> { Thing findByName(String n); }
                class Svc { private final ThingRepo repo = null; void go(Thing t) { repo.save(t); } }
                """);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();
        assertTrue(r.tableAccesses.isEmpty(), accesses(r).toString());
        assertTrue(r.entityTables.isEmpty());
    }
}
