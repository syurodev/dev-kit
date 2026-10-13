package codegraph

// The clients a KafkaAccess is made with.
const (
	KafkaTemplate = "kafka-template" // Spring Kafka's KafkaTemplate
	KafkaProducer = "kafka-producer" // the plain client's ProducerRecord
	KafkaListener = "kafka-listener" // @KafkaListener
	KafkaConsumer = "kafka-consumer" // the plain client's subscribe
	KafkaStreams  = "kafka-streams"  // Kafka Streams
	KafkaResty    = "resty-kafka"    // OpenResty's resty.kafka.producer, as a Kong plugin uses it
	KafkaCamel    = "camel-kafka"    // Apache Camel Kafka routes
)

// The roles of a KafkaAccess.
const (
	KafkaProduce = "produce"
	KafkaConsume = "consume"
)

// KafkaAccess is one place that produces to or consumes from a Kafka topic, as
// far as the source says. Topic is what the code wrote: a literal or constant,
// a property placeholder ("${app.kafka.topic.access}") that only the owning
// service's config can fold — so it is folded by the join, against that
// service's config — or a name with {holes}.
type KafkaAccess struct {
	// From is the method that does it; for a class-level @KafkaListener, the class.
	From  string `json:"from"`
	Topic string `json:"topic"`
	// Role is "produce" or "consume".
	Role   string `json:"role"`
	Client string `json:"client"`
	// GroupID is a consumer's group, as written; empty when none is visible.
	GroupID string `json:"groupId,omitempty"`
	// Pattern is true when Topic is a regular expression (a topicPattern).
	Pattern bool   `json:"pattern,omitempty"`
	File    string `json:"file"`
	Line    int    `json:"line"`
}
