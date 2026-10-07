package com.devkit.codegraph;

import com.github.javaparser.ast.body.BodyDeclaration;
import com.github.javaparser.ast.body.CallableDeclaration;
import com.github.javaparser.ast.body.FieldDeclaration;
import com.github.javaparser.ast.body.Parameter;
import com.github.javaparser.ast.body.RecordDeclaration;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.body.VariableDeclarator;
import com.github.javaparser.ast.expr.AnnotationExpr;
import com.github.javaparser.ast.expr.ArrayInitializerExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.expr.NormalAnnotationExpr;
import com.github.javaparser.ast.expr.SingleMemberAnnotationExpr;
import com.github.javaparser.ast.nodeTypes.NodeWithAnnotations;
import com.github.javaparser.ast.type.ClassOrInterfaceType;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;

/**
 * Finds where the code reads configuration, and what it switches on it.
 *
 * Syntactic, like everything here that has no types to lean on: a use is what
 * the source says — an annotation's string, a call's literal argument — never
 * a value looked up. What cannot be read (a key built at runtime, a SpEL
 * expression that is not a plain placeholder) is not guessed at.
 *
 * Eight shapes, as {@link ConfigUsage} describes them: {@code @Value}; a
 * {@code @ConfigurationProperties} class, each of its fields bound to
 * {@code prefix.field-name}; {@code @ConditionalOnProperty} and its siblings,
 * which decide whether a bean EXISTS and so are more dangerous than an
 * {@code if}; {@code @Profile}; the bean conditions that name no key
 * ({@code @ConditionalOnMissingBean}); {@code Environment.getProperty}; and
 * {@code System.getenv}.
 */
final class ConfigExtractor {

    /** A class the analyzer indexed: the node id its uses are recorded against, and its methods' ids. */
    record Owner(TypeDeclaration<?> decl, String id, String file, List<Member> methods) {}

    record Member(CallableDeclaration<?> decl, String id) {}

    /** A body of code whose calls are recorded against ownerIds (a method, or what runs an initializer). */
    record Scope(com.github.javaparser.ast.Node node, List<String> ownerIds, String file) {}

    private static final Set<String> KEYED_CONDITIONS = Set.of(
            "ConditionalOnProperty", "ConditionalOnBooleanProperty");
    private static final List<String> BEAN_CONDITIONS = List.of(
            "ConditionalOnMissingBean", "ConditionalOnBean", "ConditionalOnSingleCandidate",
            "ConditionalOnClass", "ConditionalOnMissingClass", "ConditionalOnResource",
            "ConditionalOnWebApplication", "ConditionalOnNotWebApplication", "ConditionalOnCloudPlatform",
            "ConditionalOnJava", "ConditionalOnWarDeployment");
    private static final Set<String> ENVIRONMENT_READS = Set.of(
            "getProperty", "getRequiredProperty", "containsProperty");
    private static final int MAX_BINDING_DEPTH = 3;

    private final ConstantResolver constants;
    private final Annotations annotations;
    private final Set<String> nodeIds;
    private final Map<String, List<Owner>> ownersBySimpleName = new LinkedHashMap<>();
    private final List<ConfigUsage> out = new ArrayList<>();

    private ConfigExtractor(ConstantResolver constants, Annotations annotations, Set<String> nodeIds, List<Owner> owners) {
        this.constants = constants;
        this.annotations = annotations;
        this.nodeIds = nodeIds;
        for (Owner owner : owners) {
            ownersBySimpleName.computeIfAbsent(owner.decl().getNameAsString(), k -> new ArrayList<>()).add(owner);
        }
    }

    /**
     * Every config use in the project, in a stable order. {@code nodeIds} is the
     * set of nodes the index holds: a use is recorded only against one of them,
     * because the Go side rejects an index that points at a node it lacks.
     */
    static List<ConfigUsage> extract(List<Owner> owners, List<Scope> scopes, Annotations annotations,
                                     ConstantResolver constants, Set<String> nodeIds) {
        ConfigExtractor extractor = new ConfigExtractor(constants, annotations, nodeIds, owners);
        for (Owner owner : owners) {
            extractor.readOwner(owner);
        }
        for (Scope scope : scopes) {
            extractor.readCalls(scope);
        }
        return extractor.deduped();
    }

    private List<ConfigUsage> deduped() {
        Map<String, ConfigUsage> byKey = new LinkedHashMap<>();
        for (ConfigUsage u : out) {
            byKey.putIfAbsent(String.join("\n", u.kind, u.key, u.from, u.field, String.valueOf(u.line), u.detail), u);
        }
        return new ArrayList<>(byKey.values());
    }

    // --- annotations --------------------------------------------------------------------------

    private void readOwner(Owner owner) {
        TypeDeclaration<?> type = owner.decl();
        readConditions(type, owner.id(), owner.file(), "");
        Optional<AnnotationExpr> properties = annotations.find(type, "ConfigurationProperties");
        properties.ifPresent(a -> bind(owner, a, strings(a, "prefix", "value").stream().findFirst().orElse(""), 0, new LinkedHashSet<>()));

        for (BodyDeclaration<?> member : type.getMembers()) {
            if (member instanceof FieldDeclaration field) {
                readValueAnnotation(field, owner, fieldNames(field), "");
            }
        }
        for (Member method : owner.methods()) {
            CallableDeclaration<?> decl = method.decl();
            readConditions(decl, method.id(), owner.file(), "");
            if (annotations.has(decl, "Value")) {
                readValueAnnotation(decl, owner.id(), method.id(), "", owner.file(), "setter");
            }
            for (Parameter p : decl.getParameters()) {
                if (annotations.has(p, "Value")) {
                    readValueAnnotation(p, owner.id(), owner.id(), p.getNameAsString(), owner.file(),
                            "parameter of " + decl.getNameAsString());
                }
            }
        }
        // Constructors are not in `methods`; their @Value parameters still land in the class's fields.
        for (BodyDeclaration<?> member : type.getMembers()) {
            if (member instanceof com.github.javaparser.ast.body.ConstructorDeclaration ctor) {
                for (Parameter p : ctor.getParameters()) {
                    if (annotations.has(p, "Value")) {
                        readValueAnnotation(p, owner.id(), owner.id(), p.getNameAsString(), owner.file(),
                                "parameter of " + ctor.getNameAsString());
                    }
                }
            }
        }
        // A record's components are its constructor parameters.
        if (type instanceof RecordDeclaration record) {
            for (Parameter p : record.getParameters()) {
                if (annotations.has(p, "Value")) {
                    readValueAnnotation(p, owner.id(), owner.id(), p.getNameAsString(), owner.file(),
                            "component of " + record.getNameAsString());
                }
            }
        }
    }

    private void readValueAnnotation(FieldDeclaration field, Owner owner, List<String> names, String detail) {
        if (!annotations.has(field, "Value")) {
            return;
        }
        for (String name : names) {
            readValueAnnotation(field, owner.id(), owner.id(), name, owner.file(), detail);
        }
    }

    private void readValueAnnotation(NodeWithAnnotations<?> element, String ownerId, String fromId, String field,
                                     String file, String detail) {
        Optional<AnnotationExpr> value = annotations.find(element, "Value");
        if (value.isEmpty() || !nodeIds.contains(fromId)) {
            return;
        }
        Optional<Expression> expression = attribute(value.get(), "value");
        if (expression.isEmpty()) {
            return;
        }
        String text = constants.resolve(expression.get());
        int line = line(value.get());
        for (Placeholder p : placeholders(text)) {
            ConfigUsage usage = new ConfigUsage("value", p.key(), fromId, field, file, line);
            usage.defaultValue = p.defaultValue();
            usage.detail = detail;
            out.add(usage);
        }
    }

    /** The class's fields bound to {@code prefix.field-name}, nested project types followed a few levels down. */
    private void bind(Owner owner, AnnotationExpr written, String prefix, int depth, Set<String> visiting) {
        if (prefix.isEmpty() || !nodeIds.contains(owner.id()) || !visiting.add(owner.id())) {
            return;
        }
        TypeDeclaration<?> type = owner.decl();
        List<Parameter> components = type instanceof RecordDeclaration r ? r.getParameters() : List.of();
        for (Parameter p : components) {
            bindMember(owner, written, prefix, p.getNameAsString(), p.getType().isClassOrInterfaceType()
                    ? p.getType().asClassOrInterfaceType() : null, depth, visiting);
        }
        for (BodyDeclaration<?> member : type.getMembers()) {
            if (!(member instanceof FieldDeclaration field) || field.isStatic()) {
                continue;
            }
            for (VariableDeclarator v : field.getVariables()) {
                bindMember(owner, written, prefix, v.getNameAsString(),
                        v.getType().isClassOrInterfaceType() ? v.getType().asClassOrInterfaceType() : null, depth, visiting);
            }
        }
        visiting.remove(owner.id());
    }

    private void bindMember(Owner owner, AnnotationExpr written, String prefix, String name,
                            ClassOrInterfaceType type, int depth, Set<String> visiting) {
        String key = prefix + "." + kebab(name);
        ConfigUsage usage = new ConfigUsage("properties", key, owner.id(), name, owner.file(), line(written));
        usage.detail = depth == 0 ? "@ConfigurationProperties(\"" + prefix + "\")" : "nested under " + prefix;
        out.add(usage);
        // A field typed as another class of this project binds its own fields beneath the key.
        if (type != null && depth < MAX_BINDING_DEPTH) {
            List<Owner> candidates = ownersBySimpleName.getOrDefault(type.getNameAsString(), List.of());
            if (candidates.size() == 1) {
                bind(candidates.get(0), written, key, depth + 1, visiting);
            }
        }
    }

    /** @Profile and the conditions, on a class or on a method. {@code fieldName} is what a method-level use is recorded under. */
    private void readConditions(NodeWithAnnotations<?> element, String fromId, String file, String fieldName) {
        if (!nodeIds.contains(fromId)) {
            return;
        }
        for (String name : KEYED_CONDITIONS) {
            annotations.find(element, name).ifPresent(a -> readKeyedCondition(a, name, fromId, file, fieldName));
        }
        annotations.find(element, "Profile").ifPresent(a -> {
            ConfigUsage usage = new ConfigUsage("profile", "", fromId, fieldName, file, line(a));
            usage.detail = String.join(", ", strings(a, "value"));
            out.add(usage);
        });
        annotations.find(element, "ConditionalOnExpression").ifPresent(a -> {
            Optional<Expression> expression = attribute(a, "value");
            if (expression.isEmpty()) {
                return;
            }
            String text = constants.resolve(expression.get());
            for (Placeholder p : placeholders(text)) {
                ConfigUsage usage = new ConfigUsage("condition", p.key(), fromId, fieldName, file, line(a));
                usage.defaultValue = p.defaultValue();
                usage.detail = "@ConditionalOnExpression(" + text + ")";
                out.add(usage);
            }
        });
        for (String name : BEAN_CONDITIONS) {
            annotations.find(element, name).ifPresent(a -> {
                ConfigUsage usage = new ConfigUsage("bean-condition", "", fromId, fieldName, file, line(a));
                usage.detail = "@" + name + attributesText(a);
                out.add(usage);
            });
        }
    }

    private void readKeyedCondition(AnnotationExpr a, String name, String fromId, String file, String fieldName) {
        String prefix = strings(a, "prefix").stream().findFirst().orElse("");
        List<String> names = strings(a, "name", "value");
        String havingValue = strings(a, "havingValue").stream().findFirst().orElse("");
        boolean matchIfMissing = attribute(a, "matchIfMissing").map(e -> e.toString().equals("true")).orElse(false);
        StringBuilder detail = new StringBuilder("@" + name);
        if (!havingValue.isEmpty()) {
            detail.append(" havingValue=").append(havingValue);
        }
        if (matchIfMissing) {
            detail.append(" matchIfMissing");
        }
        for (String n : names) {
            String key = prefix.isEmpty() ? n : prefix.endsWith(".") ? prefix + n : prefix + "." + n;
            ConfigUsage usage = new ConfigUsage("condition", key, fromId, fieldName, file, line(a));
            usage.detail = detail.toString();
            out.add(usage);
        }
    }

    // --- calls --------------------------------------------------------------------------------

    private void readCalls(Scope scope) {
        for (MethodCallExpr call : scope.node().findAll(MethodCallExpr.class)) {
            String name = call.getNameAsString();
            boolean environment = ENVIRONMENT_READS.contains(name) && !call.getArguments().isEmpty();
            boolean getenv = name.equals("getenv") && call.getArguments().size() == 1
                    && call.getScope().map(s -> s.toString().equals("System")).orElse(false);
            if (!environment && !getenv) {
                continue;
            }
            String key = constants.resolve(call.getArgument(0));
            // A key the code builds at runtime comes back as a hole: not a key.
            if (key.isEmpty() || key.contains("{") || key.contains(" ")) {
                continue;
            }
            for (String owner : scope.ownerIds()) {
                if (!nodeIds.contains(owner)) {
                    continue;
                }
                ConfigUsage usage = new ConfigUsage(getenv ? "env-var" : "environment", key, owner, "", scope.file(), line(call));
                if (environment && call.getArguments().size() == 2) {
                    usage.defaultValue = constants.resolve(call.getArgument(1));
                }
                out.add(usage);
            }
        }
    }

    // --- helpers ------------------------------------------------------------------------------

    record Placeholder(String key, String defaultValue) {}

    /**
     * The {@code ${key:default}} placeholders in a string, nested defaults
     * included ({@code ${a:${b}}} is both a and b). SpEL around them is left alone.
     */
    static List<Placeholder> placeholders(String text) {
        List<Placeholder> found = new ArrayList<>();
        scan(text, found);
        return found;
    }

    private static void scan(String text, List<Placeholder> found) {
        int from = 0;
        while (true) {
            int start = text.indexOf("${", from);
            if (start < 0) {
                return;
            }
            int depth = 1;
            int i = start + 2;
            int colon = -1;
            for (; i < text.length() && depth > 0; i++) {
                char c = text.charAt(i);
                if (c == '{') {
                    depth++;
                } else if (c == '}') {
                    depth--;
                } else if (c == ':' && depth == 1 && colon < 0) {
                    colon = i;
                }
            }
            if (depth != 0) {
                return; // unbalanced: not a placeholder
            }
            int end = i - 1;
            String key = text.substring(start + 2, colon < 0 ? end : colon).strip();
            String defaultValue = colon < 0 ? "" : text.substring(colon + 1, end);
            if (!key.isEmpty() && !key.contains("${")) {
                found.add(new Placeholder(key, defaultValue));
            }
            if (colon >= 0) {
                scan(defaultValue, found);
            }
            from = end + 1;
        }
    }

    /** An annotation attribute: written as {@code name = ...}, or as the bare value for {@code value}. */
    private static Optional<Expression> attribute(AnnotationExpr a, String name) {
        if (a instanceof NormalAnnotationExpr normal) {
            return normal.getPairs().stream().filter(p -> p.getNameAsString().equals(name))
                    .map(p -> p.getValue()).findFirst();
        }
        if (a instanceof SingleMemberAnnotationExpr single && name.equals("value")) {
            return Optional.of(single.getMemberValue());
        }
        return Optional.empty();
    }

    /** The strings an attribute holds — one, or an array — constants folded; first attribute name present wins. */
    private List<String> strings(AnnotationExpr a, String... names) {
        for (String name : names) {
            Optional<Expression> expression = attribute(a, name);
            if (expression.isEmpty()) {
                continue;
            }
            List<String> values = new ArrayList<>();
            if (expression.get() instanceof ArrayInitializerExpr array) {
                for (Expression e : array.getValues()) {
                    values.add(constants.resolve(e));
                }
            } else {
                values.add(constants.resolve(expression.get()));
            }
            values.removeIf(v -> v.isEmpty() || v.contains("{"));
            return values;
        }
        return List.of();
    }

    private static String attributesText(AnnotationExpr a) {
        String text = a.toString().replaceAll("\\s+", " ");
        int paren = text.indexOf('(');
        return paren < 0 ? "" : text.substring(paren);
    }

    private static List<String> fieldNames(FieldDeclaration field) {
        List<String> names = new ArrayList<>();
        field.getVariables().forEach(v -> names.add(v.getNameAsString()));
        return names;
    }

    private static int line(com.github.javaparser.ast.Node node) {
        return node.getRange().map(r -> r.begin.line).orElse(0);
    }

    /** {@code moduleBasedEnabled} → {@code module-based-enabled}: how Spring's relaxed binding spells a field. */
    static String kebab(String name) {
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < name.length(); i++) {
            char c = name.charAt(i);
            if (Character.isUpperCase(c) && i > 0) {
                sb.append('-');
            }
            sb.append(Character.toLowerCase(c));
        }
        return sb.toString();
    }
}
