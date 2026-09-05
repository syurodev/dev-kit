package mcp

import (
	"context"

	"devkit/internal/interaction"
	"devkit/internal/kafka"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerKafka(ms *sdk.Server) {
	sdk.AddTool(ms, &sdk.Tool{
		Name:        "kafka.listProfiles",
		Description: "List Kafka connection profiles (metadata only)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolKafkaListProfiles)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "kafka.connectionStatus",
		Description: "Report whether Kafka is connected and which profile is live",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolKafkaConnectionStatus)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "kafka.listTopics",
		Description: "List topics on the live Kafka connection (gated)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolKafkaListTopics)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "kafka.describeTopic",
		Description: "Describe a Kafka topic on the live connection (gated)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolKafkaDescribeTopic)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "kafka.peekMessages",
		Description: "Peek messages from a Kafka topic partition (gated)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolKafkaPeekMessages)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "kafka.listConsumerGroups",
		Description: "List Kafka consumer groups on the live connection (gated)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolKafkaListConsumerGroups)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "kafka.publishMessage",
		Description: "Publish a message on the live Kafka connection (gated)",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
	}, s.toolKafkaPublishMessage)
}

func (s *Server) toolKafkaListProfiles(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.kafka", "kafka:connect", "profile:list", meta("list_profiles", nil), func(context.Context) (any, error) {
		return s.deps.Kafka.ListProfiles()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolKafkaConnectionStatus(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.kafka", "kafka:connect", "connection", meta("connection_status", nil), func(context.Context) (any, error) {
		return s.deps.Kafka.Status(), nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) gateKafka(ctx context.Context, tool, summary string) error {
	target := s.kafkaTarget()
	id, err := GateActiveMCP(ctx, s.deps.Hub, s.deps.Envs, target, target, tool, summary, interaction.KindKafka)
	if err != nil {
		return err
	}
	// Connect only after the confirmation covered this profile id: sealed SASL
	// credentials must not reach the broker before the user approves.
	if st := s.deps.Kafka.Status(); !st.Connected || st.ProfileID != id {
		if err := s.deps.Kafka.Connect(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) toolKafkaListTopics(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	if err := s.gateKafka(ctx, "kafka.listTopics", "list topics"); err != nil {
		return nil, nil, mapInteractErr(err)
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.kafka", "kafka:connect", "connection", meta("list_topics", nil), func(callCtx context.Context) (any, error) {
		return s.deps.Kafka.ListTopics(callCtx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type kafkaDescribeTopicInput struct {
	Topic string `json:"topic"`
}

func (s *Server) toolKafkaDescribeTopic(ctx context.Context, _ *sdk.CallToolRequest, in kafkaDescribeTopicInput) (*sdk.CallToolResult, any, error) {
	if err := s.gateKafka(ctx, "kafka.describeTopic", in.Topic); err != nil {
		return nil, nil, mapInteractErr(err)
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.kafka", "kafka:connect", "connection", meta("describe_topic", map[string]any{
		"topic": in.Topic,
	}), func(callCtx context.Context) (any, error) {
		return s.deps.Kafka.DescribeTopic(callCtx, in.Topic)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type kafkaPeekMessagesInput struct {
	Topic     string `json:"topic"`
	Partition int32  `json:"partition"`
	Limit     *int   `json:"limit"`
	Offset    *int64 `json:"offset"`
}

func (s *Server) toolKafkaPeekMessages(ctx context.Context, _ *sdk.CallToolRequest, in kafkaPeekMessagesInput) (*sdk.CallToolResult, any, error) {
	if err := s.gateKafka(ctx, "kafka.peekMessages", in.Topic); err != nil {
		return nil, nil, mapInteractErr(err)
	}
	limit := 0
	if in.Limit != nil {
		limit = *in.Limit
	}
	offset := int64(0)
	if in.Offset != nil {
		offset = *in.Offset
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.kafka", "kafka:connect", "connection", meta("peek_messages", map[string]any{
		"topic": in.Topic,
	}), func(callCtx context.Context) (any, error) {
		return s.deps.Kafka.PeekMessages(callCtx, in.Topic, in.Partition, limit, offset)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolKafkaListConsumerGroups(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	if err := s.gateKafka(ctx, "kafka.listConsumerGroups", "list consumer groups"); err != nil {
		return nil, nil, mapInteractErr(err)
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.kafka", "kafka:connect", "connection", meta("list_consumer_groups", nil), func(callCtx context.Context) (any, error) {
		return s.deps.Kafka.ListConsumerGroups(callCtx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type kafkaPublishMessageInput struct {
	Topic     string `json:"topic"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Payload   string `json:"payload"` // alias for value
	Partition *int32 `json:"partition"`
}

func (in kafkaPublishMessageInput) messageValue() string {
	if in.Value != "" {
		return in.Value
	}
	return in.Payload
}

func (s *Server) toolKafkaPublishMessage(ctx context.Context, _ *sdk.CallToolRequest, in kafkaPublishMessageInput) (*sdk.CallToolResult, any, error) {
	value := in.messageValue()
	if err := s.gateKafka(ctx, "kafka.publishMessage", value); err != nil {
		return nil, nil, mapInteractErr(err)
	}
	req := kafka.PublishRequest{
		Topic:     in.Topic,
		Key:       in.Key,
		Value:     value,
		Partition: in.Partition,
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.kafka", "kafka:produce", "connection", meta("publish_message", map[string]any{
		"topic": in.Topic,
	}), func(callCtx context.Context) (any, error) {
		return s.deps.Kafka.PublishMessage(callCtx, req)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}
