package org.apache.camel.builder;
import org.apache.camel.model.RouteDefinition;
public abstract class RouteBuilder {
 public abstract void configure();
 public RouteDefinition from(String uri) { return new RouteDefinition(); }
 public RouteDefinition from(org.apache.camel.component.kafka.KafkaEndpoint endpoint) { return new RouteDefinition(); }
 public <T> T endpoint(String uri, Class<T> type) { return null; }
}
