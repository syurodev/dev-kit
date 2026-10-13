package fixture;
import org.apache.camel.builder.RouteBuilder;
import org.apache.camel.Processor;
import org.apache.camel.Exchange;
public class SimpleRoutes extends RouteBuilder {
 static final String SOURCE = "kafka:constants?groupId=constant-group";
 private Business business;
 public void configure() {
  from("kafka:literal?groupId=literal-group").routeId("literal").process(e -> business.literal());
  Processor callback = e -> business.constant();
  from(SOURCE).routeId("constant").process(callback);
  from("kafka:reference").routeId("reference").process(this::handle);
  from("kafka:object").routeId("object").process(new OrganizationFanoutProcessor());
  from("direct:local").routeId("local").process(e -> business.internal());
  from("seda:queued").routeId("queued").process(new OrganizationFanoutProcessor());
  from("kafka:sender").routeId("sender").to("direct:local");
  from("kafka:producer").routeId("producer").process(this::handle).to("kafka:outbound");
  register("kafka:helper-one", new OrganizationFanoutProcessor());
  register("kafka:helper-two", new OrganizationFanoutProcessor());
 }
 private void register(String uri, Processor handler) { from(uri).routeId("duplicate").process(handler); }
 public void handle(Exchange e) { business.reference(); }
}
