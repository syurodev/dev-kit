package fixture;
import java.util.List;
public record SourceProperties(String groupId, List<Domain> domains) {
 public record Domain(String name, KafkaEndpoint source) {}
 public record KafkaEndpoint(String topic, String brokers) {}
}
