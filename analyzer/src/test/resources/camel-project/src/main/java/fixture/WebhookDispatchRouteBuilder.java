package fixture;
import org.apache.camel.builder.RouteBuilder;
import org.apache.camel.component.kafka.KafkaEndpoint;
import org.apache.camel.component.kafka.KafkaConfiguration;
public class WebhookDispatchRouteBuilder extends RouteBuilder {
 private SourceProperties source;
 private WebhookDispatchProcessor processor;
 public void configure() {
  for (SourceProperties.Domain domain : source.domains()) {
   String routeId = "webhook-dispatch-" + domain.name();
   String groupId = source.groupId() + "." + domain.name();
   from(sourceEndpoint(routeId, domain.source(), groupId))
    .routeId(routeId)
    .process(exchange -> processor.dispatch(domain.name(), exchange));
  }
 }
 private KafkaEndpoint sourceEndpoint(String routeId, SourceProperties.KafkaEndpoint source, String groupId) {
  KafkaEndpoint endpoint = endpoint("kafka:" + routeId, KafkaEndpoint.class);
  KafkaConfiguration configuration = endpoint.getConfiguration();
  configuration.setTopic(source.topic());
  configuration.setBrokers(source.brokers());
  configuration.setGroupId(groupId);
  return endpoint;
 }
}
