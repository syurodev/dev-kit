package kafka

import (
	"context"
	"fmt"
	"net"
)

func (s *Service) TestConnection(ctx context.Context, id string) error {
	if s.dial == nil {
		return ErrBrokerDialNotConfigured
	}
	cfg, err := s.buildConnConfig(id)
	if err != nil {
		return err
	}
	client, err := s.dial(ctx, cfg)
	zeroizeString(&cfg.Password)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.Ping(ctx)
}

func (s *Service) Connect(ctx context.Context, id string) error {
	if s.dial == nil {
		return ErrBrokerDialNotConfigured
	}
	cfg, err := s.buildConnConfig(id)
	if err != nil {
		return err
	}
	client, err := s.dial(ctx, cfg)
	zeroizeString(&cfg.Password)
	if err != nil {
		return err
	}
	if err := client.Ping(ctx); err != nil {
		client.Close()
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		s.active.Close()
	}
	s.active = client
	s.activeProfile = id
	return nil
}

func (s *Service) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return nil
	}
	err := s.active.Close()
	s.active = nil
	s.activeProfile = ""
	return err
}

func (s *Service) Status() ConnectionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return ConnectionStatus{Connected: false}
	}
	return ConnectionStatus{Connected: true, ProfileID: s.activeProfile}
}

func (s *Service) ListTopics(ctx context.Context) ([]TopicInfo, error) {
	client, err := s.activeClient()
	if err != nil {
		return nil, err
	}
	return client.ListTopics(ctx)
}

func (s *Service) DescribeTopic(ctx context.Context, topic string) (TopicDetail, error) {
	client, err := s.activeClient()
	if err != nil {
		return TopicDetail{}, err
	}
	return client.DescribeTopic(ctx, topic)
}

func (s *Service) PeekMessages(ctx context.Context, topic string, partition int32, limit int, offset int64) ([]Message, error) {
	client, err := s.activeClient()
	if err != nil {
		return nil, err
	}
	limit = clampPeekLimit(limit)
	return client.PeekMessages(ctx, topic, partition, limit, offset)
}

func (s *Service) ListConsumerGroups(ctx context.Context) ([]ConsumerGroupInfo, error) {
	client, err := s.activeClient()
	if err != nil {
		return nil, err
	}
	return client.ListConsumerGroups(ctx)
}

func (s *Service) PublishMessage(ctx context.Context, req PublishRequest) (PublishResult, error) {
	client, err := s.activeClient()
	if err != nil {
		return PublishResult{}, err
	}
	return client.Publish(ctx, req)
}

func (s *Service) Close() {
	_ = s.Disconnect()
}

func (s *Service) activeClient() (BrokerClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return nil, ErrNotConnected
	}
	return s.active, nil
}

func (s *Service) buildConnConfig(id string) (ConnConfig, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return ConnConfig{}, err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return ConnConfig{}, err
	}
	if err := validateCleartextTransportSecurity(meta.BootstrapServers, meta.SecurityProtocol); err != nil {
		return ConnConfig{}, err
	}
	secret, err := s.store.OpenPayload(record, secretKeyID)
	if err != nil {
		return ConnConfig{}, err
	}
	password := string(secret)
	zeroizeBytes(secret)
	return ConnConfig{
		ProfileID:        id,
		BootstrapServers: meta.BootstrapServers,
		SecurityProtocol: meta.SecurityProtocol,
		SASLMechanism:    meta.SASLMechanism,
		Username:         meta.Username,
		Password:         password,
	}, nil
}

func validateCleartextTransportSecurity(servers []string, securityProtocol string) error {
	switch securityProtocol {
	case "PLAINTEXT", "SASL_PLAINTEXT":
	default:
		return nil
	}
	for _, server := range servers {
		host, err := bootstrapHost(server)
		if err != nil {
			return fmt.Errorf("invalid bootstrap server %q: %w", server, err)
		}
		if !isLoopbackHost(host) {
			return ErrInsecureRemotePLAINTEXT
		}
	}
	return nil
}

func bootstrapHost(server string) (string, error) {
	host, _, err := net.SplitHostPort(server)
	if err != nil {
		return "", err
	}
	return host, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func zeroizeString(s *string) {
	b := []byte(*s)
	for i := range b {
		b[i] = 0
	}
	*s = ""
}

func zeroizeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
