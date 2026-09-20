package com.devkit.codegraph;

/** One field of a body type: what a request skeleton is built from. */
public class Field {
    public String name;
    public String type;

    public Field() {}

    public Field(String name, String type) {
        this.name = name;
        this.type = type;
    }
}
