package com.devkit.codegraph;

import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.ImportDeclaration;
import com.github.javaparser.ast.body.CallableDeclaration;
import com.github.javaparser.ast.body.ConstructorDeclaration;
import com.github.javaparser.ast.body.FieldDeclaration;
import com.github.javaparser.ast.body.Parameter;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.body.VariableDeclarator;
import com.github.javaparser.ast.expr.AnnotationExpr;
import com.github.javaparser.ast.expr.AssignExpr;
import com.github.javaparser.ast.expr.BinaryExpr;
import com.github.javaparser.ast.expr.EnclosedExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.FieldAccessExpr;
import com.github.javaparser.ast.expr.LambdaExpr;
import com.github.javaparser.ast.expr.LiteralStringValueExpr;
import com.github.javaparser.ast.expr.NameExpr;
import com.github.javaparser.ast.expr.NormalAnnotationExpr;
import com.github.javaparser.ast.expr.SingleMemberAnnotationExpr;
import com.github.javaparser.ast.expr.StringLiteralExpr;
import com.github.javaparser.ast.expr.TextBlockLiteralExpr;
import com.github.javaparser.ast.expr.ThisExpr;
import com.github.javaparser.ast.expr.UnaryExpr;
import com.github.javaparser.ast.expr.VariableDeclarationExpr;
import com.github.javaparser.ast.stmt.BlockStmt;
import com.github.javaparser.ast.stmt.ExpressionStmt;
import com.github.javaparser.ast.stmt.Statement;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;

/**
 * Evaluates the compile-time String constants a mapping annotation is
 * written with — {@code @GetMapping(Paths.USERS)}, {@code API + "/x"} — from
 * the project's own source.
 *
 * Printing the expression instead turned those into routes like
 * "/Paths.USERS", which look real and are wrong. What cannot be followed
 * (a library's constant, a property placeholder built at runtime) becomes
 * "{Paths.USERS}" rather than junk: visibly a hole, and still telling the
 * reader where the value comes from.
 *
 * Outside annotations — an outbound call's URL argument — it also follows
 * what a method body can hold: a local assigned once, a parameter (a hole
 * named after it, "{id}", which is what a route variable is), and a field
 * Spring injects with {@code @Value("${key}")}, which comes back as the
 * placeholder for the service's own config to fold. None of those can
 * appear in an annotation, so endpoint extraction reads exactly as before.
 *
 * AST-only on purpose, like the annotation matching: no symbol solving, so
 * the answer does not depend on which jars the local caches happen to hold.
 */
public final class ConstantResolver {

    /** No project index: literals, and constants of the annotated type and its enclosing types. */
    public static final ConstantResolver NONE = new ConstantResolver(Map.of(), Map.of());

    private final Map<String, TypeDeclaration<?>> typesByQualifiedName;
    private final Map<String, List<TypeDeclaration<?>>> typesBySimpleName;

    private ConstantResolver(Map<String, TypeDeclaration<?>> typesByQualifiedName,
                             Map<String, List<TypeDeclaration<?>>> typesBySimpleName) {
        this.typesByQualifiedName = typesByQualifiedName;
        this.typesBySimpleName = typesBySimpleName;
    }

    public static ConstantResolver of(List<CompilationUnit> units) {
        Map<String, TypeDeclaration<?>> byQualified = new LinkedHashMap<>();
        Map<String, List<TypeDeclaration<?>>> bySimple = new LinkedHashMap<>();
        for (CompilationUnit cu : units) {
            for (TypeDeclaration<?> type : cu.findAll(TypeDeclaration.class)) {
                type.getFullyQualifiedName().ifPresent(fqn -> byQualified.putIfAbsent(fqn, type));
                bySimple.computeIfAbsent(type.getNameAsString(), k -> new ArrayList<>()).add(type);
            }
        }
        return new ConstantResolver(byQualified, bySimple);
    }

    /** The expression's String value, with "{expr}" standing in for any part that cannot be followed. */
    public String resolve(Expression expr) {
        return eval(expr, new HashSet<>());
    }

    private String eval(Expression expr, Set<VariableDeclarator> guard) {
        if (expr instanceof StringLiteralExpr s) {
            return s.asString();
        }
        if (expr instanceof TextBlockLiteralExpr t) {
            return t.asString();
        }
        if (expr instanceof LiteralStringValueExpr literal) {
            return literal.getValue(); // numbers and chars concatenated into a path
        }
        if (expr instanceof EnclosedExpr enclosed) {
            return eval(enclosed.getInner(), guard);
        }
        if (expr instanceof BinaryExpr binary && binary.getOperator() == BinaryExpr.Operator.PLUS) {
            return eval(binary.getLeft(), guard) + eval(binary.getRight(), guard);
        }
        Optional<VariableDeclarator> constant = Optional.empty();
        if (expr instanceof NameExpr name) {
            // Java's own scoping: a local or a parameter shadows any field.
            Optional<String> local = localOrParameter(name, guard);
            if (local.isPresent()) {
                return local.get();
            }
            constant = findUnqualified(expr, name.getNameAsString());
        } else if (expr instanceof FieldAccessExpr access && access.getScope() instanceof ThisExpr) {
            constant = expr.findAncestor(TypeDeclaration.class)
                    .flatMap(type -> field((TypeDeclaration<?>) type, access.getNameAsString()));
        } else if (expr instanceof FieldAccessExpr access) {
            constant = findType(expr, access.getScope().toString())
                    .flatMap(type -> field(type, access.getNameAsString()));
        }
        if (constant.isPresent() && guard.add(constant.get())) {
            try {
                Optional<String> value = fieldValue(constant.get(), guard);
                if (value.isPresent()) {
                    return value.get();
                }
            } finally {
                guard.remove(constant.get());
            }
        }
        return "{" + expr + "}";
    }

    /**
     * A field's value: its initializer, else what Spring injects into it —
     * {@code @Value("${key}")} on the field, or on the constructor parameter
     * the field is assigned from. A placeholder comes back as written
     * ("${billing.url}"), for whoever holds the service's config to fold;
     * this class never reads config. Empty when there is nothing to follow.
     */
    private Optional<String> fieldValue(VariableDeclarator field, Set<VariableDeclarator> guard) {
        if (field.getInitializer().isPresent()) {
            return Optional.of(eval(field.getInitializer().get(), guard));
        }
        Optional<String> injected = field.findAncestor(FieldDeclaration.class).flatMap(ConstantResolver::valueAnnotation);
        if (injected.isPresent()) {
            return injected;
        }
        return assignedInConstructor(field).map(assigned -> eval(assigned, guard));
    }

    /**
     * The one expression every constructor of the field's type assigns to
     * it ({@code this.baseUrl = baseUrl}). Empty when no constructor does, or
     * when two assign different things — then the value depends on which
     * constructor ran, and picking one would be a guess.
     */
    static Optional<Expression> assignedInConstructor(VariableDeclarator field) {
        Optional<TypeDeclaration> owner = field.findAncestor(TypeDeclaration.class);
        if (owner.isEmpty()) {
            return Optional.empty();
        }
        String name = field.getNameAsString();
        Expression found = null;
        for (ConstructorDeclaration ctor : ((TypeDeclaration<?>) owner.get()).getConstructors()) {
            for (AssignExpr assign : ctor.findAll(AssignExpr.class)) {
                if (assign.getOperator() != AssignExpr.Operator.ASSIGN || !assignsField(assign.getTarget(), name, ctor)) {
                    continue;
                }
                if (found != null && !found.toString().equals(assign.getValue().toString())) {
                    return Optional.empty();
                }
                found = assign.getValue();
            }
        }
        return Optional.ofNullable(found);
    }

    private static boolean assignsField(Expression target, String field, ConstructorDeclaration ctor) {
        if (target instanceof FieldAccessExpr access && access.getScope() instanceof ThisExpr) {
            return access.getNameAsString().equals(field);
        }
        return target instanceof NameExpr n && n.getNameAsString().equals(field)
                && ctor.getParameterByName(field).isEmpty();
    }

    /** {@code @Value("...")}'s string, when the element carries one with a literal. SpEL ("#{...}") is not followed. */
    static Optional<String> valueAnnotation(com.github.javaparser.ast.nodeTypes.NodeWithAnnotations<?> element) {
        for (AnnotationExpr a : element.getAnnotations()) {
            if (!Annotations.simpleName(a).equals("Value")) {
                continue;
            }
            Expression v = null;
            if (a instanceof SingleMemberAnnotationExpr single) {
                v = single.getMemberValue();
            } else if (a instanceof NormalAnnotationExpr normal) {
                for (var pair : normal.getPairs()) {
                    if (pair.getNameAsString().equals("value")) {
                        v = pair.getValue();
                    }
                }
            }
            if (v instanceof StringLiteralExpr literal && !literal.asString().startsWith("#{")) {
                return Optional.of(literal.asString());
            }
        }
        return Optional.empty();
    }

    /**
     * A bare name that is a local variable or a parameter of the code it is
     * used in. A parameter is a hole named after it ("{id}") — the value
     * comes from the caller — unless Spring injects it with @Value. A local
     * is followed to its initializer when it is never reassigned; one that
     * is, or that has no initializer, is a hole too. Empty when the name is
     * neither, so the caller goes on to fields.
     */
    private Optional<String> localOrParameter(NameExpr name, Set<VariableDeclarator> guard) {
        String id = name.getNameAsString();
        com.github.javaparser.ast.Node child = name;
        Optional<com.github.javaparser.ast.Node> current = name.getParentNode();
        while (current.isPresent()) {
            com.github.javaparser.ast.Node node = current.get();
            if (node instanceof BlockStmt block) {
                Optional<VariableDeclarator> local = declaredBefore(block, child, id);
                if (local.isPresent()) {
                    VariableDeclarator v = local.get();
                    if (v.getInitializer().isEmpty() || reassigned(block, id) || !guard.add(v)) {
                        return Optional.of("{" + id + "}");
                    }
                    try {
                        return Optional.of(eval(v.getInitializer().get(), guard));
                    } finally {
                        guard.remove(v);
                    }
                }
            } else if (node instanceof LambdaExpr lambda) {
                for (Parameter p : lambda.getParameters()) {
                    if (p.getNameAsString().equals(id)) {
                        return Optional.of("{" + id + "}");
                    }
                }
            } else if (node instanceof CallableDeclaration<?> callable) {
                for (Parameter p : callable.getParameters()) {
                    if (p.getNameAsString().equals(id)) {
                        return Optional.of(valueAnnotation(p).orElse("{" + id + "}"));
                    }
                }
                return Optional.empty(); // past the method: fields from here on
            } else if (node instanceof TypeDeclaration<?>) {
                return Optional.empty();
            }
            child = node;
            current = node.getParentNode();
        }
        return Optional.empty();
    }

    /** The declarator of {@code id} in a statement of {@code block} before the one holding {@code use}. */
    private static Optional<VariableDeclarator> declaredBefore(BlockStmt block, com.github.javaparser.ast.Node use, String id) {
        for (Statement stmt : block.getStatements()) {
            if (stmt == use) {
                break;
            }
            if (stmt instanceof ExpressionStmt es && es.getExpression() instanceof VariableDeclarationExpr decl) {
                for (VariableDeclarator v : decl.getVariables()) {
                    if (v.getNameAsString().equals(id)) {
                        return Optional.of(v);
                    }
                }
            }
        }
        return Optional.empty();
    }

    private static boolean reassigned(BlockStmt block, String id) {
        for (AssignExpr assign : block.findAll(AssignExpr.class)) {
            if (assign.getTarget() instanceof NameExpr n && n.getNameAsString().equals(id)) {
                return true;
            }
        }
        for (UnaryExpr unary : block.findAll(UnaryExpr.class)) {
            if (unary.getExpression() instanceof NameExpr n && n.getNameAsString().equals(id)
                    && (unary.isPrefix() || unary.isPostfix())
                    && unary.getOperator() != UnaryExpr.Operator.MINUS && unary.getOperator() != UnaryExpr.Operator.PLUS
                    && unary.getOperator() != UnaryExpr.Operator.LOGICAL_COMPLEMENT
                    && unary.getOperator() != UnaryExpr.Operator.BITWISE_COMPLEMENT) {
                return true;
            }
        }
        return false;
    }

    /** A bare name: this type's constant, an enclosing type's, or one statically imported. */
    private Optional<VariableDeclarator> findUnqualified(Expression at, String name) {
        Optional<com.github.javaparser.ast.Node> current = at.getParentNode();
        while (current.isPresent()) {
            if (current.get() instanceof TypeDeclaration<?> type) {
                Optional<VariableDeclarator> own = field(type, name);
                if (own.isPresent()) {
                    return own;
                }
            }
            current = current.get().getParentNode();
        }
        Optional<CompilationUnit> cu = at.findCompilationUnit();
        if (cu.isEmpty()) {
            return Optional.empty();
        }
        for (ImportDeclaration imp : cu.get().getImports()) {
            if (!imp.isStatic()) {
                continue;
            }
            String imported = imp.getNameAsString();
            if (imp.isAsterisk()) {
                Optional<VariableDeclarator> viaStar = Optional.ofNullable(typesByQualifiedName.get(imported))
                        .flatMap(type -> field(type, name));
                if (viaStar.isPresent()) {
                    return viaStar;
                }
            } else if (imported.endsWith("." + name)) {
                String owner = imported.substring(0, imported.length() - name.length() - 1);
                Optional<VariableDeclarator> single = Optional.ofNullable(typesByQualifiedName.get(owner))
                        .flatMap(type -> field(type, name));
                if (single.isPresent()) {
                    return single;
                }
            }
        }
        return Optional.empty();
    }

    /**
     * The type a qualifier names, the way the compiler would look for it:
     * a type nested in this one's enclosing types, an import, this package,
     * a fully-qualified name, and finally any project type of that simple
     * name when there is exactly one.
     */
    private Optional<TypeDeclaration<?>> findType(Expression at, String qualifier) {
        String first = qualifier.contains(".") ? qualifier.substring(0, qualifier.indexOf('.')) : qualifier;
        String rest = qualifier.substring(first.length());

        Optional<com.github.javaparser.ast.Node> current = at.getParentNode();
        while (current.isPresent()) {
            if (current.get() instanceof TypeDeclaration<?> type) {
                if (type.getNameAsString().equals(first)) {
                    return nested(type, rest);
                }
                for (var member : type.getMembers()) {
                    if (member instanceof TypeDeclaration<?> inner && inner.getNameAsString().equals(first)) {
                        return nested(inner, rest);
                    }
                }
            }
            current = current.get().getParentNode();
        }

        Optional<CompilationUnit> cu = at.findCompilationUnit();
        if (cu.isPresent()) {
            for (ImportDeclaration imp : cu.get().getImports()) {
                if (!imp.isStatic() && !imp.isAsterisk() && imp.getName().getIdentifier().equals(first)) {
                    return Optional.ofNullable(typesByQualifiedName.get(imp.getNameAsString() + rest));
                }
            }
            String pkg = cu.get().getPackageDeclaration().map(p -> p.getNameAsString() + ".").orElse("");
            TypeDeclaration<?> samePackage = typesByQualifiedName.get(pkg + qualifier);
            if (samePackage != null) {
                return Optional.of(samePackage);
            }
        }
        TypeDeclaration<?> qualified = typesByQualifiedName.get(qualifier);
        if (qualified != null) {
            return Optional.of(qualified);
        }
        List<TypeDeclaration<?>> bySimple = typesBySimpleName.getOrDefault(first, List.of());
        if (bySimple.size() == 1) {
            return nested(bySimple.get(0), rest);
        }
        return Optional.empty();
    }

    private static Optional<TypeDeclaration<?>> nested(TypeDeclaration<?> type, String rest) {
        TypeDeclaration<?> current = type;
        for (String segment : rest.split("\\.")) {
            if (segment.isEmpty()) {
                continue;
            }
            TypeDeclaration<?> next = null;
            for (var member : current.getMembers()) {
                if (member instanceof TypeDeclaration<?> inner && inner.getNameAsString().equals(segment)) {
                    next = inner;
                    break;
                }
            }
            if (next == null) {
                return Optional.empty();
            }
            current = next;
        }
        return Optional.of(current);
    }

    private static Optional<VariableDeclarator> field(TypeDeclaration<?> type, String name) {
        for (var member : type.getMembers()) {
            if (member instanceof FieldDeclaration field) {
                for (VariableDeclarator v : field.getVariables()) {
                    if (v.getNameAsString().equals(name)) {
                        return Optional.of(v);
                    }
                }
            }
        }
        return Optional.empty();
    }
}
