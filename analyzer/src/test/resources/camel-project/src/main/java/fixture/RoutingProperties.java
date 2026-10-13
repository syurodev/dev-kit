package fixture;
import java.util.List;
public record RoutingProperties(List<Domain> domains) {
 public record Domain(String name, KafkaEndpoint source) {}
 public record KafkaEndpoint(String topic, String groupId) {}
}
