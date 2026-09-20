package com.devkit.codegraph;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.List;

/**
 * One method parameter.
 *
 * {@code source}, {@code wireName} and {@code fields} are only ever set for a
 * handler's parameters, so they are left out of the JSON when empty rather
 * than writing three nulls for every parameter of every method in the project.
 */
@JsonInclude(JsonInclude.Include.NON_EMPTY)
public class Param {
    public String name;
    public String type;
    /** "body" | "path" | "query" | "header" | "other"; null when not a handler parameter. */
    public String source;
    /** The name on the wire when the annotation renames it. Null means {@code name}. */
    public String wireName;
    /** The body type's own fields, one level deep. Null unless source is "body". */
    public List<Field> fields;

    public Param() {}

    public Param(String name, String type) {
        this.name = name;
        this.type = type;
    }
}
