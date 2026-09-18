package com.devkit.codegraph;

import java.util.ArrayList;
import java.util.List;

public class Node {
    public String id;
    public String label;
    public String kind;   // "class" | "method" (constructors are "method" too)
    public String layer;  // "controller" | "service" | "repository" | "unknown"
    public String filePath;
    public int line;
    public int endLine;
    public String summary = "";
    public List<Param> params = new ArrayList<>();
}
