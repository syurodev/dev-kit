package fixture;
import org.apache.camel.builder.RouteBuilder;
import org.apache.camel.Processor;
import org.apache.camel.Exchange;
import org.apache.camel.component.kafka.KafkaEndpoint;
import org.apache.camel.component.kafka.KafkaConfiguration;
public class AdditionalRoutes extends RouteBuilder {
 private Business business;
 private boolean runtime;
 public void configure() {
  KafkaEndpoint local = endpoint("kafka:uri-is-not-topic", KafkaEndpoint.class);
  KafkaConfiguration config = local.getConfiguration();
  from(local).routeId("local-endpoint").process(factory());
  configureEndpoint(config, "local-topic", "local-group");
  register("kafka:helper-reference", this::handle);
  register("kafka:helper-lambda", e -> business.literal());
  String uri = "kafka:before";
  if (runtime) { uri = "kafka:after"; }
  from(uri).routeId("runtime-uri").process(this::handle);
  from("kafka:unresolved-handler").routeId("unresolved").process(externalProcessor());
  from("kafka:no-id?groupId=standalone").process(this::handle);
  from("kafka:construction").routeId("construction").process(e -> new PerMessageProcessor().process(e));
 }
 private Processor factory() { return new OrganizationFanoutProcessor(); }
 private void configureEndpoint(KafkaConfiguration config, String topic, String group) {
  KafkaConfiguration alias = config;
  alias.setTopic(topic); alias.setGroupId(group);
 }
 private Processor externalProcessor() { return null; }
 private void register(String uri, Processor handler) { from(uri).routeId(uri).process(handler); }
 private void handle(Exchange e) { business.reference(); }
}
