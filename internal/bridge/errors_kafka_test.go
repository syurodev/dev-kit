package bridge

import (
	"testing"

	"devkit/internal/kafka"
)

func TestPublicErrorMapsKafkaNotConnected(t *testing.T) {
	err := publicError("builtin.kafka", kafka.ErrNotConnected)
	if err == nil || err.Error() != "connection_failed: Kafka is not connected." {
		t.Fatalf("public error = %v", err)
	}
}

func TestPublicErrorMapsKafkaInsecureRemotePLAINTEXT(t *testing.T) {
	err := publicError("builtin.kafka", kafka.ErrInsecureRemotePLAINTEXT)
	if err == nil || err.Error() != "connection_failed: Plaintext Kafka to a remote host is not allowed." {
		t.Fatalf("public error = %v", err)
	}
}

func TestPublicErrorMapsKafkaUnavailable(t *testing.T) {
	err := publicError("builtin.kafka", errKafkaUnavailable)
	if err == nil || err.Error() != "internal_error: Kafka is not available." {
		t.Fatalf("public error = %v", err)
	}
}
