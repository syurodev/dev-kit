package fixture;
import org.apache.camel.builder.RouteBuilder;
abstract class ProjectRouteBuilder extends RouteBuilder {}
public class InheritedRoutes extends ProjectRouteBuilder {
 private OrganizationFanoutProcessor processor;
 public void configure() { from("kafka:inherited").routeId("inherited").process(processor); }
}
