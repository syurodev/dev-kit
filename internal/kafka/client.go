package kafka

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

const brokerRequestTimeout = 30 * time.Second

type franzClient struct {
	cl  *kgo.Client
	adm *kadm.Client
	mu  sync.Mutex
}

// DialFranz opens a franz-go backed BrokerClient for cfg.
func DialFranz(ctx context.Context, cfg ConnConfig) (BrokerClient, error) {
	opts, err := franzOpts(cfg)
	if err != nil {
		return nil, err
	}
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &franzClient{
		cl:  cl,
		adm: kadm.NewClient(cl),
	}, nil
}

func franzOpts(cfg ConnConfig) ([]kgo.Opt, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.BootstrapServers...),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		// franz-go disables auto topic creation by default; AllowAutoTopicCreation() is opt-in only.
	}

	switch cfg.SecurityProtocol {
	case "PLAINTEXT":
	case "SSL":
		opts = append(opts, kgo.DialTLS())
	case "SASL_PLAINTEXT":
		mech, err := saslMechanism(cfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, kgo.SASL(mech))
	case "SASL_SSL":
		mech, err := saslMechanism(cfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, kgo.DialTLS(), kgo.SASL(mech))
	default:
		return nil, fmt.Errorf("unsupported security protocol %q", cfg.SecurityProtocol)
	}

	return opts, nil
}

func saslMechanism(cfg ConnConfig) (sasl.Mechanism, error) {
	switch cfg.SASLMechanism {
	case "PLAIN":
		return plain.Auth{
			User: cfg.Username,
			Pass: cfg.Password,
		}.AsMechanism(), nil
	case "SCRAM-SHA-256":
		return scram.Auth{
			User: cfg.Username,
			Pass: cfg.Password,
		}.AsSha256Mechanism(), nil
	case "SCRAM-SHA-512":
		return scram.Auth{
			User: cfg.Username,
			Pass: cfg.Password,
		}.AsSha512Mechanism(), nil
	default:
		return nil, fmt.Errorf("unsupported sasl mechanism %q", cfg.SASLMechanism)
	}
}

func (c *franzClient) Close() error {
	c.cl.Close()
	return nil
}

func (c *franzClient) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, brokerRequestTimeout)
	defer cancel()
	_, err := c.adm.Metadata(ctx)
	return err
}

func (c *franzClient) ListTopics(ctx context.Context) ([]TopicInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, brokerRequestTimeout)
	defer cancel()
	topics, err := c.adm.ListTopics(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TopicInfo, 0, len(topics))
	for _, td := range topics.Sorted() {
		if td.Err != nil {
			continue
		}
		out = append(out, TopicInfo{Name: td.Topic})
	}
	return out, nil
}

func (c *franzClient) DescribeTopic(ctx context.Context, topic string) (TopicDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, brokerRequestTimeout)
	defer cancel()
	topics, err := c.adm.ListTopics(ctx, topic)
	if err != nil {
		return TopicDetail{}, err
	}
	td, ok := topics[topic]
	if !ok {
		return TopicDetail{}, fmt.Errorf("topic %q not found", topic)
	}
	if td.Err != nil {
		return TopicDetail{}, td.Err
	}
	detail := TopicDetail{
		Name:              topic,
		ReplicationFactor: int32(td.Partitions.NumReplicas()),
	}
	for _, p := range td.Partitions.Sorted() {
		if p.Err != nil {
			continue
		}
		detail.Partitions = append(detail.Partitions, PartitionInfo{ID: p.Partition})
	}
	return detail, nil
}

func (c *franzClient) PeekMessages(ctx context.Context, topic string, partition int32, limit int, offset int64) ([]Message, error) {
	limit = clampPeekLimit(limit)

	c.mu.Lock()
	defer c.mu.Unlock()

	startOffset, err := c.peekStartOffset(ctx, topic, partition, limit, offset)
	if err != nil {
		return nil, err
	}

	consume := map[string]map[int32]kgo.Offset{
		topic: {partition: kgo.NewOffset().At(startOffset)},
	}
	c.cl.AddConsumePartitions(consume)
	defer c.cl.RemoveConsumePartitions(map[string][]int32{topic: {partition}})

	ctx, cancel := context.WithTimeout(ctx, brokerRequestTimeout)
	defer cancel()

	messages := make([]Message, 0, limit)
	for len(messages) < limit {
		fetches := c.cl.PollFetches(ctx)
		if err := fetches.Err0(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				break
			}
			return nil, err
		}
		recs := fetches.Records()
		if len(recs) == 0 {
			break
		}
		for _, rec := range recs {
			if rec.Topic != topic || rec.Partition != partition {
				continue
			}
			messages = append(messages, recordToMessage(rec))
			if len(messages) >= limit {
				break
			}
		}
	}
	return messages, nil
}

func (c *franzClient) peekStartOffset(ctx context.Context, topic string, partition int32, limit int, offset int64) (int64, error) {
	if offset >= 0 {
		return offset, nil
	}
	ctx, cancel := context.WithTimeout(ctx, brokerRequestTimeout)
	defer cancel()
	ends, err := c.adm.ListEndOffsets(ctx, topic)
	if err != nil {
		return 0, err
	}
	lo, ok := ends.Lookup(topic, partition)
	if !ok {
		return 0, fmt.Errorf("partition %d not found for topic %q", partition, topic)
	}
	if lo.Err != nil {
		return 0, lo.Err
	}
	start := lo.Offset - int64(limit)
	if start < 0 {
		start = 0
	}
	return start, nil
}

func recordToMessage(rec *kgo.Record) Message {
	return Message{
		Offset:    rec.Offset,
		Key:       bytesAsDisplayString(rec.Key),
		Value:     bytesAsDisplayString(rec.Value),
		Timestamp: rec.Timestamp.UnixMilli(),
	}
}

func bytesAsDisplayString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func (c *franzClient) ListConsumerGroups(ctx context.Context) ([]ConsumerGroupInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, brokerRequestTimeout)
	defer cancel()

	lags, err := c.adm.Lag(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ConsumerGroupInfo, 0, len(lags))
	for _, gl := range lags.Sorted() {
		if gl.Error() != nil {
			continue
		}
		info := ConsumerGroupInfo{GroupID: gl.Group}
		if total := gl.Lag.Total(); total > 0 {
			info.Lag = total
		}
		out = append(out, info)
	}
	return out, nil
}

func (c *franzClient) Publish(ctx context.Context, req PublishRequest) (PublishResult, error) {
	ctx, cancel := context.WithTimeout(ctx, brokerRequestTimeout)
	defer cancel()

	rec := &kgo.Record{
		Topic: req.Topic,
		Key:   []byte(req.Key),
		Value: []byte(req.Value),
	}
	if req.Partition != nil {
		rec.Partition = *req.Partition
	}
	res := c.cl.ProduceSync(ctx, rec)
	if err := res.FirstErr(); err != nil {
		return PublishResult{}, err
	}
	produced := res[0].Record
	return PublishResult{
		Partition: produced.Partition,
		Offset:    produced.Offset,
		Timestamp: produced.Timestamp.UnixMilli(),
	}, nil
}
