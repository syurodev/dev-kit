package com.devkit.codegraph;

/**
 * One call expression in one method body, kept un-deduped and with its line.
 *
 * Deliberately not an Edge. Edges are deduped by (from, to, kind) so the graph
 * stays clean, which throws away both the line and the repetition — and a
 * def-use trace needs exactly those, to interleave calls with field accesses in
 * source order.
 *
 * `to` is empty when the callee could not be turned into a node, and `unresolved`
 * says why. Recording that at all is the point: the old code dropped these calls
 * silently, so a trace could stop dead with no way to tell the reader whether the
 * flow ended or the analyzer simply lost it. "interface" also covers a project
 * interface or abstract method with no implementation in the project, and
 * "lombok" a getter, setter or `log` call to code Lombok generates.
 */
public class CallSite {
    public String from;
    public String to;         // "" when unresolved
    public int line;
    public String unresolved; // "" | "library" | "interface" | "unresolved" | "lombok"

    public CallSite() {}

    public CallSite(String from, String to, int line, String unresolved) {
        this.from = from;
        this.to = to;
        this.line = line;
        this.unresolved = unresolved;
    }
}
