package com.devkit.codegraph;

/**
 * One control structure around a call — an {@code if} branch, a {@code catch},
 * a loop, a closure — as {@link CallSite#frames} lists them, outermost first.
 *
 * Language-neutral on purpose (the Go side reads the same shape from every
 * analyzer): {@code kind} is one of if, else, case, catch, finally, try, loop,
 * closure, and, or, ternary, await; {@code id} is the structure's
 * {@code line:col}, so calls in one branch share it; {@code exits} is true when
 * the branch ends by returning, throwing, breaking or continuing.
 */
public class StepFrame {
    public String id;
    public String kind;
    public String text;
    public boolean exits;

    public StepFrame() {}

    public StepFrame(String id, String kind, String text, boolean exits) {
        this.id = id;
        this.kind = kind;
        this.text = Steps.clip(text);
        this.exits = exits;
    }
}
