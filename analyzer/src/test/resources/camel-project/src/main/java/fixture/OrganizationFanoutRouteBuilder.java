package fixture;
import org.apache.camel.builder.RouteBuilder;
import org.apache.camel.Processor;
public class OrganizationFanoutRouteBuilder extends RouteBuilder {
 private RoutingProperties routing;
 private OrganizationFanoutProcessor processor;
 public void configure() {
  for (RoutingProperties.Domain domain : routing.domains()) { register(domain, processor); }
 }
 private void register(RoutingProperties.Domain domain, Processor handler) {
  from(sourceUri(domain.source())).routeId("fanout-" + domain.name()).process(handler);
 }
 private String sourceUri(RoutingProperties.KafkaEndpoint source) {
  return "kafka:" + source.topic() + "?groupId=" + source.groupId();
 }
}
