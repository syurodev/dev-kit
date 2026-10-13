package fixture;
import org.apache.camel.Exchange;
import org.apache.camel.Processor;
public class WebhookDispatchProcessor implements Processor {
 private Business business;
 public void dispatch(String domain, Exchange exchange) { business.webhook(); }
 public void process(Exchange exchange) { dispatch("unused", exchange); }
}
