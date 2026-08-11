package kafka

import (
	"context"
	"errors"
)

var (
	ErrNotConnected            = errors.New("kafka is not connected")
	ErrInsecureRemotePLAINTEXT = errors.New("plaintext kafka to remote host is not allowed")
	ErrBrokerDialNotConfigured = errors.New("kafka broker dial is not configured")
)

// ConnConfig holds decrypted connection parameters passed to DialFunc.
type ConnConfig struct {
	ProfileID        string
	BootstrapServers []string
	SecurityProtocol string
	SASLMechanism    string
	Username         string
	Password         string
}

// BrokerClient is the narrow broker surface the Kafka service uses for cluster
// operations. Real implementations will wrap franz-go; tests inject fakes.
type BrokerClient interface {
	Ping(ctx context.Context) error
	Close() error
	ListTopics(ctx context.Context) ([]TopicInfo, error)
	DescribeTopic(ctx context.Context, topic string) (TopicDetail, error)
	PeekMessages(ctx context.Context, topic string, partition int32, limit int, offset int64) ([]Message, error)
	ListConsumerGroups(ctx context.Context) ([]ConsumerGroupInfo, error)
	Publish(ctx context.Context, req PublishRequest) (PublishResult, error)
}

// DialFunc opens a BrokerClient for the given connection config.
type DialFunc func(ctx context.Context, cfg ConnConfig) (BrokerClient, error)

// ConnectionStatus reports whether a profile is actively connected.
type ConnectionStatus struct {
	Connected bool   `json:"connected"`
	ProfileID string `json:"profile_id,omitempty"`
}

// TopicInfo is a topic listing entry.
type TopicInfo struct {
	Name string `json:"name"`
}

// PartitionInfo describes one partition of a topic.
type PartitionInfo struct {
	ID int32 `json:"id"`
}

// TopicDetail is metadata for a single topic.
type TopicDetail struct {
	Name              string          `json:"name"`
	ReplicationFactor int32           `json:"replication_factor"`
	Partitions        []PartitionInfo `json:"partitions"`
}

// Message is a consumed record (peek / browse).
type Message struct {
	Offset    int64  `json:"offset"`
	Key       string `json:"key,omitempty"`
	Value     string `json:"value"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// ConsumerGroupInfo is a consumer group listing entry.
type ConsumerGroupInfo struct {
	GroupID string `json:"group_id"`
	Lag     int64  `json:"lag,omitempty"`
}

// PublishRequest is a produce request.
type PublishRequest struct {
	Topic     string `json:"topic"`
	Key       string `json:"key,omitempty"`
	Value     string `json:"value"`
	Partition *int32 `json:"partition,omitempty"`
}

// PublishResult is the broker acknowledgement for a produce.
type PublishResult struct {
	Partition int32 `json:"partition"`
	Offset    int64 `json:"offset"`
	Timestamp int64 `json:"timestamp,omitempty"`
}

func clampPeekLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 500 {
		return 500
	}
	return limit
}
