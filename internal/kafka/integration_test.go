package kafka

import (
	"context"
	"os"
	"testing"
)

func TestIntegrationKafkaRoundTrip(t *testing.T) {
	if os.Getenv("DEVKIT_KAFKA_TEST") != "1" {
		t.Skip("set DEVKIT_KAFKA_TEST=1 to run integration tests against a real broker")
	}

	bootstrap := os.Getenv("DEVKIT_KAFKA_BOOTSTRAP")
	if bootstrap == "" {
		bootstrap = "127.0.0.1:9092"
	}
	topic := os.Getenv("DEVKIT_KAFKA_TOPIC")
	if topic == "" {
		t.Skip("set DEVKIT_KAFKA_TOPIC to an existing topic for round-trip test")
	}

	ctx := context.Background()
	cfg := ConnConfig{
		BootstrapServers: []string{bootstrap},
		SecurityProtocol: "PLAINTEXT",
	}
	client, err := DialFranz(ctx, cfg)
	if err != nil {
		t.Fatalf("DialFranz: %v", err)
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	result, err := client.Publish(ctx, PublishRequest{
		Topic: topic,
		Value: "devkit-integration-test",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.Offset < 0 {
		t.Fatalf("offset = %d, want >= 0", result.Offset)
	}

	messages, err := client.PeekMessages(ctx, topic, result.Partition, 10, -1)
	if err != nil {
		t.Fatalf("PeekMessages: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("PeekMessages returned no messages")
	}
	last := messages[len(messages)-1]
	if last.Value != "devkit-integration-test" {
		t.Fatalf("last message value = %q, want devkit-integration-test", last.Value)
	}
}
