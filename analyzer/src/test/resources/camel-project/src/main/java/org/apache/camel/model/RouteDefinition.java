package org.apache.camel.model;
import org.apache.camel.Processor;
public class RouteDefinition {
 public RouteDefinition routeId(String id) { return this; }
 public RouteDefinition process(Processor handler) { return this; }
 public RouteDefinition to(String uri) { return this; }
}
