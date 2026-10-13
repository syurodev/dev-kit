package fixture;
import org.apache.camel.Exchange;
import org.apache.camel.Processor;
public class OrganizationFanoutProcessor implements Processor {
 private Business business;
 public void process(Exchange exchange) { business.fanout(); }
}
