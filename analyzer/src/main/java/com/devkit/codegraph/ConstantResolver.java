package com.devkit.codegraph;

import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.ImportDeclaration;
import com.github.javaparser.ast.body.FieldDeclaration;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.body.VariableDeclarator;
import com.github.javaparser.ast.expr.BinaryExpr;
import com.github.javaparser.ast.expr.EnclosedExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.FieldAccessExpr;
import com.github.javaparser.ast.expr.LiteralStringValueExpr;
import com.github.javaparser.ast.expr.NameExpr;
import com.github.javaparser.ast.expr.StringLiteralExpr;
import com.github.javaparser.ast.expr.TextBlockLiteralExpr;

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
            constant = findUnqualified(expr, name.getNameAsString());
        } else if (expr instanceof FieldAccessExpr access) {
            constant = findType(expr, access.getScope().toString())
                    .flatMap(type -> field(type, access.getNameAsString()));
        }
        if (constant.isPresent() && constant.get().getInitializer().isPresent() && guard.add(constant.get())) {
            String value = eval(constant.get().getInitializer().get(), guard);
            guard.remove(constant.get());
            return value;
        }
        return "{" + expr + "}";
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
