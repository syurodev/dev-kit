package com.devkit.codegraph;

/** Non-HTTP entry points; the existing endpoints wire contract stays unchanged. */
public class EntryPoint {
    public String id, verb, target, entryNode, kind, owner, group;
    public String service = "";
    public int order;
    public String routeId = "", topic = "", consumerGroup = "";
}
