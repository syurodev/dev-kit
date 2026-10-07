package com.devkit.codegraph;

import java.util.ArrayList;
import java.util.List;

public class AnalysisResult {
    public List<Node> nodes = new ArrayList<>();
    public List<Edge> edges = new ArrayList<>();
    public List<Endpoint> endpoints = new ArrayList<>();
    public List<FileDependency> fileDependencies = new ArrayList<>();
    public List<FieldAccess> fieldAccesses = new ArrayList<>();
    public List<CallSite> callSites = new ArrayList<>();
    public List<OutboundCall> outboundCalls = new ArrayList<>();
    public List<ConfigKey> configKeys = new ArrayList<>();
    public List<ConfigUsage> configUsages = new ArrayList<>();
    public List<EntityTable> entityTables = new ArrayList<>();
    public List<TableAccess> tableAccesses = new ArrayList<>();
}
