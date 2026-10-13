package fixture;
import org.apache.camel.Processor;
import org.apache.camel.Exchange;
public class PerMessageProcessor implements Processor {
 public PerMessageProcessor() { new Business().literal(); }
 public void process(Exchange exchange) { new Business().literal(); }
}
