package com.devkit.codegraph;

import com.github.javaparser.ast.body.Parameter;
import com.github.javaparser.ast.body.RecordDeclaration;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.expr.AnnotationExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.NormalAnnotationExpr;
import com.github.javaparser.ast.expr.SingleMemberAnnotationExpr;
import com.github.javaparser.ast.expr.StringLiteralExpr;

import java.util.ArrayList;
import java.util.List;
import java.util.Optional;

/**
 * Where a handler parameter's value comes from on the wire, and what a body
 * type's fields are.
 *
 * Only Spring's own bindings are read. The analyzer matches annotations by
 * simple name everywhere else for the reason LayerClassifier gives, and this
 * keeps that: a project's own {@code @JsonBody} wrapper is seen through by
 * {@link Annotations} exactly like a stereotype is.
 *
 * A parameter with none of these is {@code other} rather than a guess. An
 * unannotated parameter in a Spring handler is usually the request, the
 * principal or a model — not a value to put in a draft — and inventing a query
 * parameter for it would put something in the URL that was never there.
 */
public final class ParamBinding {

    private static final List<String> BODY = List.of("RequestBody");
    private static final List<String> PATH = List.of("PathVariable");
    private static final List<String> QUERY = List.of("RequestParam");
    private static final List<String> HEADER = List.of("RequestHeader");

    private ParamBinding() {}

    /** Sets source and wireName on param from the parameter's annotations. */
    public static void describe(Param param, Parameter declared, Annotations annotations) {
        Optional<Annotations.Match> match = annotations.findAny(declared, BODY);
        if (match.isPresent()) {
            param.source = "body";
            return;
        }
        param.source = "other";
        for (var candidate : List.of(
                new Binding("path", PATH), new Binding("query", QUERY), new Binding("header", HEADER))) {
            Optional<Annotations.Match> found = annotations.findAny(declared, candidate.names());
            if (found.isEmpty()) {
                continue;
            }
            param.source = candidate.source();
            String named = nameAttribute(found.get().written());
            if (named != null && !named.equals(param.name)) {
                param.wireName = named;
            }
            return;
        }
    }

    private record Binding(String source, List<String> names) {}

    /**
     * The name the annotation gives, from {@code @RequestParam("page_size")} or
     * {@code @RequestParam(name = "page_size")}.
     *
     * Only a string literal counts. A constant would have to be resolved, and a
     * wrong wire name is worse than none: the draft would send a parameter the
     * server ignores while the real one is missing.
     */
    private static String nameAttribute(AnnotationExpr annotation) {
        if (annotation instanceof SingleMemberAnnotationExpr single) {
            return literal(single.getMemberValue());
        }
        if (annotation instanceof NormalAnnotationExpr normal) {
            for (var pair : normal.getPairs()) {
                String key = pair.getNameAsString();
                if (key.equals("value") || key.equals("name")) {
                    return literal(pair.getValue());
                }
            }
        }
        return null;
    }

    private static String literal(Expression expr) {
        return expr instanceof StringLiteralExpr string ? string.asString() : null;
    }

    /**
     * A body type's own fields, one level deep.
     *
     * A record's components ARE its fields, and a DTO written as a record is
     * the common modern shape. Static fields are left out: a constant on the
     * DTO is not part of the request.
     */
    public static List<Field> fieldsOf(TypeDeclaration<?> type) {
        List<Field> fields = new ArrayList<>();
        if (type instanceof RecordDeclaration record) {
            for (var component : record.getParameters()) {
                fields.add(new Field(component.getNameAsString(), component.getType().asString()));
            }
            return fields;
        }
        for (var field : type.getFields()) {
            if (field.isStatic()) {
                continue;
            }
            for (var variable : field.getVariables()) {
                fields.add(new Field(variable.getNameAsString(), field.getElementType().asString()));
            }
        }
        return fields;
    }
}
