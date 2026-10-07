package com.devkit.codegraph;

public class Edge {
    public String from;
    public String to;
    public String kind; // "call" — also interface/abstract method -> its project implementation
    /**
     * The condition under which the call runs, as written in the source
     * ({@code "!moduleBasedEnabled"}); "" when it runs whenever the caller does.
     * See {@link Guards}.
     */
    public String guard = "";

    public Edge() {}

    public Edge(String from, String to, String kind) {
        this.from = from;
        this.to = to;
        this.kind = kind;
    }

    public Edge(String from, String to, String kind, String guard) {
        this(from, to, kind);
        this.guard = guard;
    }
}
