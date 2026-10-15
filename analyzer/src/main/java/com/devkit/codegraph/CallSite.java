package com.devkit.codegraph;

import java.util.ArrayList;
import java.util.List;

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

    // What the flow outline reads (see Steps). Empty on a site the analyzer
    // has nothing to say about, which is how every older index reads too.
    public int col;
    public String text = "";
    public String calleeKey = "";  // "<declaring type>#<member>"
    public String receiver = "";   // param | field | local-from-param | local | static | this | unknown
    public String resultUse = "";  // discarded | assigned | returned | argument | condition | unknown
    public List<StepFrame> frames = new ArrayList<>();
    public String role = "";       // "" for a call, "override" for a dispatch link to an override
    // The project node the call names, even when `to` stays "" because the
    // node has no body to follow (a repository or client interface method):
    // the flow outline still shows it as the project's own call.
    public String target = "";

    public CallSite() {}

    public CallSite(String from, String to, int line, String unresolved) {
        this.from = from;
        this.to = to;
        this.line = line;
        this.unresolved = unresolved;
    }

    CallSite describe(Steps.Info info, String calleeKey) {
        this.col = info.col();
        this.text = info.text();
        this.receiver = info.receiver();
        this.resultUse = info.resultUse();
        this.frames = info.frames();
        this.calleeKey = calleeKey == null ? "" : calleeKey;
        return this;
    }
}
