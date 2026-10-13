package fixture;
class RouteBuilder { NotCamel from(String uri) { return new NotCamel(); } }
public class NotCamel extends RouteBuilder {
 public void configure() { from("kafka:fake").process("handler").to("kafka:also-fake"); }
 public NotCamel process(String s) { return this; }
 public NotCamel to(String s) { return this; }
}
