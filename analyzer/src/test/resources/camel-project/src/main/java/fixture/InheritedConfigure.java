package fixture;
import org.apache.camel.builder.RouteBuilder;
import org.apache.camel.Exchange;
abstract class BaseConfiguredRoute extends RouteBuilder {
 public void configure() { from("kafka:inherited-configure").routeId("inherited-configure").process(this::handle); }
 public abstract void handle(Exchange e);
}
public class InheritedConfigure extends BaseConfiguredRoute {
 private Business business;
 public void handle(Exchange e) { business.constant(); }
}
