package com.devkit.codegraph;

import com.devkit.codegraph.ConfigExtractor.Member;
import com.devkit.codegraph.ConfigExtractor.Owner;
import com.devkit.codegraph.ConfigExtractor.Scope;
import com.github.javaparser.ast.body.BodyDeclaration;
import com.github.javaparser.ast.body.CallableDeclaration;
import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import com.github.javaparser.ast.body.FieldDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.expr.AnnotationExpr;
import com.github.javaparser.ast.expr.ArrayInitializerExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.expr.NormalAnnotationExpr;
import com.github.javaparser.ast.expr.SingleMemberAnnotationExpr;
import com.github.javaparser.ast.nodeTypes.NodeWithAnnotations;
import com.github.javaparser.ast.type.ClassOrInterfaceType;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.function.BiFunction;

/**
 * Finds which tables the code reads and writes: the entity-to-table map, and
 * every access — a repository's {@code @Query}, a derived {@code findByX}, a
 * call to {@code save}/{@code findById} inherited from Spring Data, a SQL string
 * handed to JdbcTemplate, a JPQL or native string handed to the EntityManager.
 *
 * Text, like the rest of this analyzer's reading of data access: a table is
 * what a statement NAMES. A query assembled at runtime from pieces is read as
 * far as its constant parts go, and a table named only by a variable is missed.
 * So is whatever the database does on its own — triggers, cascades, views.
 */
final class TableExtractor {

    private static final Set<String> SPRING_DATA = Set.of(
            "Repository", "CrudRepository", "ListCrudRepository", "PagingAndSortingRepository",
            "ListPagingAndSortingRepository", "JpaRepository", "R2dbcRepository",
            "ReactiveCrudRepository", "ReactiveSortingRepository");
    private static final List<String> READ_PREFIXES = List.of(
            "find", "get", "read", "query", "search", "stream", "count", "exists");
    private static final List<String> WRITE_PREFIXES = List.of("delete", "remove", "save", "insert", "update", "persist");
    /** What a repository inherits from CrudRepository and its relatives, and what each does. */
    private static final Set<String> CRUD_READS = Set.of(
            "findById", "findAll", "findAllById", "existsById", "count", "getById", "getReferenceById", "getOne",
            "findOne", "exists");
    private static final Set<String> CRUD_WRITES = Set.of(
            "save", "saveAll", "saveAndFlush", "saveAllAndFlush", "delete", "deleteById", "deleteAll",
            "deleteAllById", "deleteInBatch", "deleteAllInBatch", "deleteAllByIdInBatch");
    private static final Set<String> SQL_STRING_CALLS = Set.of(
            "query", "queryForObject", "queryForList", "queryForMap", "queryForRowSet", "queryForStream",
            "update", "batchUpdate", "execute", "prepareStatement", "prepareCall");
    private static final Set<String> SQL_STARTS = Set.of(
            "select", "insert", "update", "delete", "with", "merge", "truncate", "replace", "create", "alter", "drop");
    private static final int MAX_DETAIL = 300;

    private final ConstantResolver constants;
    private final Annotations annotations;
    private final Set<String> nodeIds;
    private final BiFunction<Scope, MethodCallExpr, String> receiverType;

    private final Map<String, Owner> ownersById = new LinkedHashMap<>();
    private final Map<String, List<Owner>> ownersBySimpleName = new HashMap<>();
    /** Entity class id -> its primary table row. */
    private final Map<String, EntityTable> primaryByEntity = new LinkedHashMap<>();
    /** JPQL entity name (lower-cased) -> its primary table row. */
    private final Map<String, EntityTable> primaryByName = new HashMap<>();
    /** Repository interface id -> the entity it manages. */
    private final Map<String, EntityTable> entityOfRepository = new HashMap<>();
    /** Repository interface id -> the names of the methods it declares, which are not inherited CRUD. */
    private final Map<String, Set<String>> declared = new HashMap<>();

    private final List<EntityTable> entities = new ArrayList<>();
    private final List<TableAccess> accesses = new ArrayList<>();

    private TableExtractor(ConstantResolver constants, Annotations annotations, Set<String> nodeIds, List<Owner> owners,
                           BiFunction<Scope, MethodCallExpr, String> receiverType) {
        this.constants = constants;
        this.annotations = annotations;
        this.nodeIds = nodeIds;
        this.receiverType = receiverType;
        for (Owner owner : owners) {
            ownersById.put(owner.id(), owner);
            ownersBySimpleName.computeIfAbsent(owner.decl().getNameAsString(), k -> new ArrayList<>()).add(owner);
        }
    }

    record Result(List<EntityTable> entities, List<TableAccess> accesses) {}

    /**
     * {@code receiverType} says what class a call's receiver is (its node id), or null when it cannot be
     * told — the one thing here that needs the symbol solver.
     */
    static Result extract(List<Owner> owners, List<Scope> scopes, Annotations annotations, ConstantResolver constants,
                          Set<String> nodeIds, BiFunction<Scope, MethodCallExpr, String> receiverType) {
        TableExtractor x = new TableExtractor(constants, annotations, nodeIds, owners, receiverType);
        for (Owner owner : owners) {
            x.readEntity(owner);
        }
        for (Owner owner : owners) {
            x.readRepository(owner);
        }
        for (Scope scope : scopes) {
            x.readCalls(scope);
        }
        return new Result(x.entities, x.deduped());
    }

    private List<TableAccess> deduped() {
        Map<String, TableAccess> byKey = new LinkedHashMap<>();
        for (TableAccess a : accesses) {
            byKey.putIfAbsent(String.join("\n", a.from, a.schema, a.table, a.access, a.via, String.valueOf(a.line), String.valueOf(a.col), a.detail), a);
        }
        return new ArrayList<>(byKey.values());
    }

    // --- entities -----------------------------------------------------------------------------

    private void readEntity(Owner owner) {
        TypeDeclaration<?> type = owner.decl();
        if (!annotations.has(type, "Entity") || !nodeIds.contains(owner.id())) {
            return;
        }
        String entityName = annotations.find(type, "Entity").flatMap(a -> string(a, "name")).orElse(type.getNameAsString());
        Optional<AnnotationExpr> table = annotations.find(type, "Table");
        String name = table.flatMap(a -> string(a, "name")).orElse(snake(type.getNameAsString()));
        String schema = table.flatMap(a -> string(a, "schema")).orElse("");

        EntityTable primary = row(owner, entityName, name, schema, "primary", table.map(this::line).orElse(line(type)));
        entities.add(primary);
        primaryByEntity.put(owner.id(), primary);
        primaryByName.putIfAbsent(entityName.toLowerCase(Locale.ROOT), primary);
        primaryByName.putIfAbsent(type.getNameAsString().toLowerCase(Locale.ROOT), primary);

        annotations.find(type, "SecondaryTable").ifPresent(a ->
                tableRow(owner, entityName, a, "secondary"));
        annotations.find(type, "SecondaryTables").ifPresent(a ->
                nested(a, "value").forEach(inner -> tableRow(owner, entityName, inner, "secondary")));
        for (BodyDeclaration<?> member : type.getMembers()) {
            if (member instanceof FieldDeclaration || member instanceof MethodDeclaration) {
                NodeWithAnnotations<?> element = (NodeWithAnnotations<?>) member;
                annotations.find(element, "JoinTable").ifPresent(a -> tableRow(owner, entityName, a, "join"));
                annotations.find(element, "CollectionTable").ifPresent(a -> tableRow(owner, entityName, a, "collection"));
            }
        }
    }

    private void tableRow(Owner owner, String entityName, AnnotationExpr a, String kind) {
        Optional<String> name = string(a, "name");
        if (name.isPresent()) {
            entities.add(row(owner, entityName, name.get(), string(a, "schema").orElse(""), kind, line(a)));
        }
    }

    private EntityTable row(Owner owner, String entityName, String table, String schema, String kind, int line) {
        EntityTable row = new EntityTable();
        row.entity = owner.id();
        row.entityName = entityName;
        row.table = table;
        row.schema = schema;
        row.kind = kind;
        row.file = owner.file();
        row.line = line;
        return row;
    }

    // --- repositories ---------------------------------------------------------------------------

    private void readRepository(Owner owner) {
        if (!(owner.decl() instanceof ClassOrInterfaceDeclaration type) || !type.isInterface()) {
            return;
        }
        Set<String> typeArguments = new LinkedHashSet<>();
        if (!springData(owner, typeArguments, new LinkedHashSet<>())) {
            return;
        }
        EntityTable entity = null;
        for (String argument : typeArguments) {
            List<Owner> candidates = ownersBySimpleName.getOrDefault(argument, List.of());
            if (candidates.size() == 1 && primaryByEntity.containsKey(candidates.get(0).id())) {
                entity = primaryByEntity.get(candidates.get(0).id());
                break;
            }
        }
        declared.put(owner.id(), declared(owner, new LinkedHashSet<>()));
        if (entity == null) {
            return; // a repository of something that is not an entity of this project has no table to name
        }
        entityOfRepository.put(owner.id(), entity);
        for (Member method : owner.methods()) {
            readRepositoryMethod(owner, entity, method);
        }
    }

    /** True when the interface is a Spring Data repository, directly or through project interfaces; collects the type arguments met on the way. */
    private boolean springData(Owner owner, Set<String> typeArguments, Set<String> visited) {
        if (!visited.add(owner.id()) || !(owner.decl() instanceof ClassOrInterfaceDeclaration type)) {
            return false;
        }
        boolean found = false;
        for (ClassOrInterfaceType extended : type.getExtendedTypes()) {
            if (SPRING_DATA.contains(extended.getNameAsString())) {
                found = true;
            }
            extended.getTypeArguments().ifPresent(args -> args.forEach(a -> {
                if (a.isClassOrInterfaceType()) {
                    typeArguments.add(a.asClassOrInterfaceType().getNameAsString());
                }
            }));
            List<Owner> parents = ownersBySimpleName.getOrDefault(extended.getNameAsString(), List.of());
            if (parents.size() == 1 && springData(parents.get(0), typeArguments, visited)) {
                found = true;
            }
        }
        return found;
    }

    /** Names declared by the interface and by the project interfaces it extends: those are not inherited CRUD. */
    private Set<String> declared(Owner owner, Set<String> visited) {
        Set<String> names = new LinkedHashSet<>();
        if (!visited.add(owner.id())) {
            return names;
        }
        for (Member m : owner.methods()) {
            names.add(m.decl().getNameAsString());
        }
        if (owner.decl() instanceof ClassOrInterfaceDeclaration type) {
            for (ClassOrInterfaceType extended : type.getExtendedTypes()) {
                List<Owner> parents = ownersBySimpleName.getOrDefault(extended.getNameAsString(), List.of());
                if (parents.size() == 1) {
                    names.addAll(declared(parents.get(0), visited));
                }
            }
        }
        return names;
    }

    private void readRepositoryMethod(Owner owner, EntityTable entity, Member method) {
        CallableDeclaration<?> decl = method.decl();
        if (!nodeIds.contains(method.id()) || (decl instanceof MethodDeclaration m && m.getBody().isPresent())) {
            return; // a default method has a body; what it calls is read at its calls
        }
        Optional<AnnotationExpr> query = annotations.find(decl, "Query");
        if (query.isPresent()) {
            readQueryAnnotation(owner, entity, method, query.get());
            return;
        }
        String name = decl.getNameAsString();
        String access = prefixed(name, READ_PREFIXES) ? "read" : prefixed(name, WRITE_PREFIXES) ? "write" : null;
        if (access != null) {
            add(method.id(), entity.table, entity.schema, access, "repository-derived", name, owner.file(), line(decl));
        }
    }

    private void readQueryAnnotation(Owner owner, EntityTable entity, Member method, AnnotationExpr query) {
        Optional<Expression> value = attribute(query, "value");
        if (value.isEmpty()) {
            return;
        }
        boolean nativeQuery = attribute(query, "nativeQuery").map(e -> e.toString().equals("true")).orElse(false);
        String text = constants.resolve(value.get());
        // Spring Data's own placeholder for the managed entity.
        text = text.replace("#{#entityName}", nativeQuery ? entity.table : entity.entityName);
        text = holes(text);
        int line = line(query);
        for (SqlTables.Ref ref : SqlTables.parse(text, !nativeQuery)) {
            record(method.id(), ref, nativeQuery, "repository-query", text, owner.file(), line);
        }
    }

    // --- calls --------------------------------------------------------------------------------

    private void readCalls(Scope scope) {
        for (MethodCallExpr call : scope.node().findAll(MethodCallExpr.class)) {
            String name = call.getNameAsString();
            readInheritedCrud(scope, call, name);
            readSqlString(scope, call, name);
        }
    }

    /** {@code repo.save(x)}: not declared on the repository, so no node and no edge — recorded where it is called. */
    private void readInheritedCrud(Scope scope, MethodCallExpr call, String name) {
        boolean read = CRUD_READS.contains(name) || name.startsWith("findFirst") || name.startsWith("findTop");
        boolean write = CRUD_WRITES.contains(name);
        if (call.getScope().isEmpty() || (!read && !write)) {
            return;
        }
        String repositoryId = receiverType.apply(scope, call);
        EntityTable entity = repositoryId == null ? null : entityOfRepository.get(repositoryId);
        if (entity == null || declared.getOrDefault(repositoryId, Set.of()).contains(name)) {
            return;
        }
        Owner repository = ownersById.get(repositoryId);
        String detail = repository.decl().getNameAsString() + "." + name;
        for (String from : scope.ownerIds()) {
            add(from, entity.table, entity.schema, write ? "write" : "read", "repository-crud", detail, scope.file(), Steps.line(call), Steps.col(call));
        }
    }

    /** {@code jdbc.update("insert into t ..")}, {@code em.createQuery("select ..")}: a string that is plainly a statement. */
    private void readSqlString(Scope scope, MethodCallExpr call, String name) {
        boolean entityManager = name.equals("createQuery") || name.equals("createNativeQuery");
        if ((!entityManager && !SQL_STRING_CALLS.contains(name)) || call.getArguments().isEmpty()) {
            return;
        }
        String text = holes(constants.resolve(call.getArgument(0)));
        if (!looksLikeSql(text)) {
            return;
        }
        boolean jpql = name.equals("createQuery");
        for (SqlTables.Ref ref : SqlTables.parse(text, jpql)) {
            for (String from : scope.ownerIds()) {
                record(from, ref, !jpql, entityManager ? "entity-manager" : "jdbc", text, scope.file(), Steps.line(call), Steps.col(call));
            }
        }
    }

    // --- helpers ------------------------------------------------------------------------------

    /** Turns a parsed reference into an access: a JPQL entity name through the entity map, a native name as it is. */
    private void record(String from, SqlTables.Ref ref, boolean nativeSql, String via, String statement, String file, int line) {
        record(from, ref, nativeSql, via, statement, file, line, 0);
    }

    private void record(String from, SqlTables.Ref ref, boolean nativeSql, String via, String statement, String file, int line, int col) {
        if (nativeSql) {
            add(from, ref.name(), ref.schema(), ref.access(), via, statement, file, line, col);
            return;
        }
        EntityTable entity = primaryByName.get(ref.name().toLowerCase(Locale.ROOT));
        if (entity != null) {
            add(from, entity.table, entity.schema, ref.access(), via, statement, file, line, col);
        }
    }

    private void add(String from, String table, String schema, String access, String via, String detail, String file, int line) {
        add(from, table, schema, access, via, detail, file, line, 0);
    }

    /** col is the call's name column for a row read off a call (Steps.col), 0 for one read off a declaration. */
    private void add(String from, String table, String schema, String access, String via, String detail, String file, int line, int col) {
        if (!nodeIds.contains(from) || table == null || table.isEmpty()) {
            return;
        }
        TableAccess a = new TableAccess();
        a.from = from;
        a.table = table;
        a.schema = schema == null ? "" : schema;
        a.access = access;
        a.via = via;
        String folded = detail.replaceAll("\\s+", " ").trim();
        a.detail = folded.length() <= MAX_DETAIL ? folded : folded.substring(0, MAX_DETAIL - 1) + "…";
        a.file = file;
        a.line = line;
        a.col = col;
        accesses.add(a);
    }

    private static boolean looksLikeSql(String text) {
        String t = text.stripLeading().toLowerCase(Locale.ROOT);
        int end = 0;
        while (end < t.length() && Character.isLetter(t.charAt(end))) {
            end++;
        }
        return text.length() > 12 && SQL_STARTS.contains(t.substring(0, end));
    }

    /** What ConstantResolver leaves for the part it cannot read — {@code {expr}} — as a placeholder the SQL scan skips. */
    private static String holes(String text) {
        return text.replaceAll("\\{[^{}]*}", "?");
    }

    private static boolean prefixed(String name, List<String> prefixes) {
        for (String prefix : prefixes) {
            if (name.startsWith(prefix) && (name.length() == prefix.length() || Character.isUpperCase(name.charAt(prefix.length())))) {
                return true;
            }
        }
        return false;
    }

    /** Spring Boot's default naming: {@code OrganizationApiEnablement} is {@code organization_api_enablement}. */
    static String snake(String name) {
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < name.length(); i++) {
            char c = name.charAt(i);
            if (Character.isUpperCase(c) && i > 0
                    && (Character.isLowerCase(name.charAt(i - 1)) || Character.isDigit(name.charAt(i - 1))
                    || (i + 1 < name.length() && Character.isLowerCase(name.charAt(i + 1))))) {
                sb.append('_');
            }
            sb.append(Character.toLowerCase(c));
        }
        return sb.toString();
    }

    private Optional<String> string(AnnotationExpr a, String name) {
        return attribute(a, name).map(constants::resolve).filter(s -> !s.isEmpty() && !s.contains("{"));
    }

    /** The annotations nested in an array attribute: {@code @SecondaryTables({@SecondaryTable(..), ..})}. */
    private static List<AnnotationExpr> nested(AnnotationExpr a, String name) {
        List<AnnotationExpr> out = new ArrayList<>();
        attribute(a, name).ifPresent(e -> {
            if (e instanceof ArrayInitializerExpr array) {
                array.getValues().forEach(v -> {
                    if (v instanceof AnnotationExpr inner) {
                        out.add(inner);
                    }
                });
            } else if (e instanceof AnnotationExpr inner) {
                out.add(inner);
            }
        });
        return out;
    }

    private static Optional<Expression> attribute(AnnotationExpr a, String name) {
        if (a instanceof NormalAnnotationExpr normal) {
            return normal.getPairs().stream().filter(p -> p.getNameAsString().equals(name)).map(p -> p.getValue()).findFirst();
        }
        if (a instanceof SingleMemberAnnotationExpr single && name.equals("value")) {
            return Optional.of(single.getMemberValue());
        }
        return Optional.empty();
    }

    private int line(com.github.javaparser.ast.Node node) {
        return node.getRange().map(r -> r.begin.line).orElse(0);
    }
}
