package crossservice

import (
	"testing"

	"devkit/internal/codegraph"
)

func kafka(role, topic, client, group string) codegraph.SnapshotKafka {
	return codegraph.SnapshotKafka{
		KafkaAccess: codegraph.KafkaAccess{From: "node", Topic: topic, Role: role, Client: client, GroupID: group, File: "K.java", Line: 1},
		Label:       "K." + role,
	}
}

func withKafka(s Service, props map[string]string, uses ...codegraph.SnapshotKafka) Service {
	s.Snapshot.Kafka = uses
	for k, v := range props {
		s.Config.Props[k] = v
	}
	return s
}

func topicByName(report KafkaReport, name string) *KafkaTopic {
	for i := range report.Topics {
		if report.Topics[i].Topic == name {
			return &report.Topics[i]
		}
	}
	return nil
}

// Each service names the topic by a property of its own; the join folds each
// against the config of the service that wrote it.
func TestRunKafkaFoldsEachServicesTopicAgainstItsOwnConfig(t *testing.T) {
	access := withKafka(svc("access-service", "8081", nil), nil,
		kafka("produce", "${app.kafka.topic.access:fsap.access.changed}", "kafka-template", ""),
		kafka("produce", "fsap.org.created", "kafka-template", ""))
	billing := withKafka(svc("billing", "8082", nil),
		map[string]string{"billing.topics.access": "fsap.access.changed", "billing.group": "billing-readers"},
		kafka("consume", "${billing.topics.access}", "kafka-listener", "${billing.group}"))
	report, err := RunKafka([]Service{access, billing}, KafkaQuery{Topic: "fsap.access.*"})
	if err != nil {
		t.Fatal(err)
	}
	topic := topicByName(report, "fsap.access.changed")
	if topic == nil || !topic.CrossService || topic.Gap != "" {
		t.Fatalf("topics = %+v", report.Topics)
	}
	if len(topic.Producers) != 1 || topic.Producers[0].Service != "access-service" || topic.Producers[0].Topic != "${app.kafka.topic.access:fsap.access.changed}" {
		t.Fatalf("producers = %+v: the default is folded, the original kept", topic.Producers)
	}
	if len(topic.Consumers) != 1 || topic.Consumers[0].Service != "billing" || topic.Consumers[0].GroupID != "billing-readers" {
		t.Fatalf("consumers = %+v", topic.Consumers)
	}
	if topic.Consumers[0].Resolved != "fsap.access.changed" {
		t.Fatalf("resolved = %q", topic.Consumers[0].Resolved)
	}
}

func TestRunKafkaListsTheSharedTopicsAndNotTheRest(t *testing.T) {
	a := withKafka(svc("a-service", "8081", nil), nil,
		kafka("produce", "shared.events", "kafka-template", ""),
		kafka("produce", "private.events", "kafka-template", ""),
		kafka("consume", "private.events", "kafka-listener", "g"))
	b := withKafka(svc("b-service", "8082", nil), nil, kafka("consume", "shared.events", "kafka-listener", "g"))
	report, err := RunKafka([]Service{a, b}, KafkaQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TopicTotal != 1 || report.Topics[0].Topic != "shared.events" {
		t.Fatalf("topics = %+v: a service talking to itself shares nothing", report.Topics)
	}
}

func TestRunKafkaAnOverlappingTopicWithAHoleIsTheSameTopic(t *testing.T) {
	a := withKafka(svc("a-service", "8081", nil), nil, kafka("produce", "fsap.events.{org}", "kafka-producer", ""))
	b := withKafka(svc("b-service", "8082", nil), nil, kafka("consume", "fsap.events.created", "kafka-listener", "g"))
	report, err := RunKafka([]Service{a, b}, KafkaQuery{Topic: "fsap.events.created"})
	if err != nil {
		t.Fatal(err)
	}
	if report.TopicTotal != 2 {
		t.Fatalf("topics = %+v", report.Topics)
	}
	for _, topic := range report.Topics {
		if !topic.CrossService || len(topic.Services) != 2 {
			t.Fatalf("topic = %+v: the produced pattern and the consumed name are one resource", topic)
		}
		if topic.Gap != "" {
			t.Fatalf("topic %s gap = %q", topic.Topic, topic.Gap)
		}
	}
}

func TestRunKafkaARegexConsumerConsumesEveryTopicItMatches(t *testing.T) {
	a := withKafka(svc("a-service", "8081", nil), nil,
		kafka("produce", "billing.invoice.created", "kafka-template", ""),
		kafka("produce", "orders.created", "kafka-template", ""))
	pattern := kafka("consume", `billing\..*`, "kafka-listener", "readers")
	pattern.Pattern = true
	b := withKafka(svc("b-service", "8082", nil), nil, pattern)
	report, err := RunKafka([]Service{a, b}, KafkaQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TopicTotal != 1 || report.Topics[0].Topic != "billing.invoice.created" {
		t.Fatalf("topics = %+v", report.Topics)
	}
	consumer := report.Topics[0].Consumers[0]
	if consumer.Service != "b-service" || !consumer.Pattern || consumer.Topic != `billing\..*` {
		t.Fatalf("consumer = %+v", consumer)
	}
}

func TestRunKafkaGapsAreProducedAndUnconsumedOrTheReverse(t *testing.T) {
	a := withKafka(svc("a-service", "8081", nil), nil,
		kafka("produce", "nobody.reads.this", "kafka-template", ""),
		kafka("consume", "nobody.writes.this", "kafka-listener", "g"),
		kafka("produce", "healthy.topic", "kafka-template", ""))
	b := withKafka(svc("b-service", "8082", nil), nil, kafka("consume", "healthy.topic", "kafka-listener", "g"))
	report, err := RunKafka([]Service{a, b}, KafkaQuery{Gaps: true})
	if err != nil {
		t.Fatal(err)
	}
	gaps := map[string]string{}
	for _, topic := range report.Topics {
		gaps[topic.Topic] = topic.Gap
	}
	if len(gaps) != 2 || gaps["nobody.reads.this"] != "no-consumer" || gaps["nobody.writes.this"] != "no-producer" {
		t.Fatalf("gaps = %v", gaps)
	}
}

func TestRunKafkaListsAUseWhosePropertyIsNotSetInsteadOfDroppingIt(t *testing.T) {
	a := withKafka(svc("a-service", "8081", nil), nil, kafka("consume", "${not.configured.here}", "kafka-listener", "g"))
	report, err := RunKafka([]Service{a}, KafkaQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unresolved) != 1 || report.Unresolved[0].Topic != "${not.configured.here}" {
		t.Fatalf("unresolved = %+v", report.Unresolved)
	}
	if report.TopicTotal != 0 || report.Topics == nil {
		t.Fatalf("topics = %+v", report.Topics)
	}
}

func TestRunKafkaRefusesAGenericTopicAndNamesUnreadableServices(t *testing.T) {
	a := withKafka(svc("a-service", "8081", nil), nil, kafka("produce", "{topic}", "kafka-template", ""), kafka("produce", "ab", "kafka-template", ""))
	gone := Service{Name: "reports-service", Path: "/ws/reports-service"}
	for _, topic := range []string{"*", "ab", "{x}.events"} {
		if _, err := RunKafka([]Service{a}, KafkaQuery{Topic: topic}); err == nil {
			t.Errorf("topic %q must be refused as too generic", topic)
		}
	}
	report, err := RunKafka([]Service{a, gone}, KafkaQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TopicTotal != 0 {
		t.Fatalf("generic topics must not be linked: %+v", report.Topics)
	}
	said := false
	for _, n := range report.Notes {
		if n == "reports-service is not-indexed: its Kafka topics are not in this answer" {
			said = true
		}
	}
	if !said {
		t.Fatalf("notes = %v", report.Notes)
	}
	if _, err := RunKafka([]Service{a}, KafkaQuery{Limit: -1}); err == nil {
		t.Fatal("a negative limit must be refused")
	}
}
