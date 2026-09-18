package com.devkit.codegraph;

public class Edge {
    public String from;
    public String to;
    public String kind; // "call" — also interface/abstract method -> its project implementation

    public Edge() {}

    public Edge(String from, String to, String kind) {
        this.from = from;
        this.to = to;
        this.kind = kind;
    }
}
