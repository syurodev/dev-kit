package com.devkit.codegraph;

public class Node {
    public String id;
    public String label;
    public String kind;   // "class" | "method"
    public String layer;  // "controller" | "service" | "repository" | "unknown"
    public String filePath;
    public int line;
    public int endLine;
}
