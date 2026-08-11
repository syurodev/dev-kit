package bridge

import (
	"context"
	"errors"

	"devkit/internal/kafka"
)

var errKafkaUnavailable = errors.New("kafka is not available")

func (s *AppService) kafkaService() (*kafka.Service, error) {
	if s.container.Kafka == nil {
		return nil, errKafkaUnavailable
	}
	return s.container.Kafka, nil
}

func (s *AppService) KafkaListProfiles() ([]kafka.ProfileSummary, error) {
	result, err := s.call(context.Background(), "builtin.kafka", "kafka:connect", "profile:list", map[string]any{
		"action": "list_profiles",
	}, func(context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return svc.ListProfiles()
	})
	if err != nil {
		return nil, err
	}
	return result.([]kafka.ProfileSummary), nil
}

func (s *AppService) KafkaCreateProfile(input kafka.CreateProfileInput) (kafka.ProfileSummary, error) {
	result, err := s.call(context.Background(), "builtin.kafka", "kafka:connect", "profile:new", map[string]any{
		"action": "create_profile",
	}, func(context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return svc.CreateProfile(input)
	})
	if err != nil {
		return kafka.ProfileSummary{}, err
	}
	return result.(kafka.ProfileSummary), nil
}

func (s *AppService) KafkaDeleteProfile(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.kafka", "kafka:connect", "profile:"+opaqueID, map[string]any{
		"action":     "delete_profile",
		"profile_id": opaqueID,
	}, func(context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return nil, svc.DeleteProfile(id)
	})
	return err
}

func (s *AppService) KafkaTestConnection(ctx context.Context, id string) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.kafka", "kafka:connect", "profile:"+opaqueID, map[string]any{
		"action":     "test_connection",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return nil, svc.TestConnection(callCtx, id)
	})
	return err
}

func (s *AppService) KafkaConnect(ctx context.Context, id string) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.kafka", "kafka:connect", "profile:"+opaqueID, map[string]any{
		"action":     "connect",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return nil, svc.Connect(callCtx, id)
	})
	return err
}

func (s *AppService) KafkaDisconnect() error {
	_, err := s.call(context.Background(), "builtin.kafka", "kafka:connect", "connection", map[string]any{
		"action": "disconnect",
	}, func(context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return nil, svc.Disconnect()
	})
	return err
}

func (s *AppService) KafkaConnectionStatus() (kafka.ConnectionStatus, error) {
	result, err := s.call(context.Background(), "builtin.kafka", "kafka:connect", "connection", map[string]any{
		"action": "connection_status",
	}, func(context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return svc.Status(), nil
	})
	if err != nil {
		return kafka.ConnectionStatus{}, err
	}
	return result.(kafka.ConnectionStatus), nil
}

func (s *AppService) KafkaListTopics(ctx context.Context) ([]kafka.TopicInfo, error) {
	result, err := s.call(ctx, "builtin.kafka", "kafka:connect", "connection", map[string]any{
		"action": "list_topics",
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return svc.ListTopics(callCtx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]kafka.TopicInfo), nil
}

func (s *AppService) KafkaDescribeTopic(ctx context.Context, topic string) (kafka.TopicDetail, error) {
	result, err := s.call(ctx, "builtin.kafka", "kafka:connect", "connection", map[string]any{
		"action": "describe_topic",
		"topic":  topic,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return svc.DescribeTopic(callCtx, topic)
	})
	if err != nil {
		return kafka.TopicDetail{}, err
	}
	return result.(kafka.TopicDetail), nil
}

func (s *AppService) KafkaPeekMessages(ctx context.Context, topic string, partition int32, limit int, offset int64) ([]kafka.Message, error) {
	result, err := s.call(ctx, "builtin.kafka", "kafka:connect", "connection", map[string]any{
		"action": "peek_messages",
		"topic":  topic,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return svc.PeekMessages(callCtx, topic, partition, limit, offset)
	})
	if err != nil {
		return nil, err
	}
	return result.([]kafka.Message), nil
}

func (s *AppService) KafkaListConsumerGroups(ctx context.Context) ([]kafka.ConsumerGroupInfo, error) {
	result, err := s.call(ctx, "builtin.kafka", "kafka:connect", "connection", map[string]any{
		"action": "list_consumer_groups",
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return svc.ListConsumerGroups(callCtx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]kafka.ConsumerGroupInfo), nil
}

func (s *AppService) KafkaPublishMessage(ctx context.Context, req kafka.PublishRequest) (kafka.PublishResult, error) {
	result, err := s.call(ctx, "builtin.kafka", "kafka:produce", "connection", map[string]any{
		"action": "publish_message",
		"topic":  req.Topic,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.kafkaService()
		if err != nil {
			return nil, err
		}
		return svc.PublishMessage(callCtx, req)
	})
	if err != nil {
		return kafka.PublishResult{}, err
	}
	return result.(kafka.PublishResult), nil
}
