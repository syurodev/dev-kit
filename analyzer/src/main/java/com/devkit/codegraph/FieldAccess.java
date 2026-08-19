package com.devkit.codegraph;

public class FieldAccess {
    public String classId;
    public String nodeId;
    public String fieldName;
    public String kind; // "read" | "write"
    public int line;

    public FieldAccess() {}

    public FieldAccess(String classId, String nodeId, String fieldName, String kind, int line) {
        this.classId = classId;
        this.nodeId = nodeId;
        this.fieldName = fieldName;
        this.kind = kind;
        this.line = line;
    }
}
