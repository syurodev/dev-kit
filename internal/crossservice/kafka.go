package crossservice

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"devkit/internal/codegraph"
)

// A Kafka topic connects the service that writes to it and the one that reads
// it without either naming the other, like a Redis key — with two differences.
// A topic name usually lives in config (@KafkaListener(topics = "${app.topic}")),
// so each use is folded against the config of the SERVICE THAT WROTE IT: two
// services can spell the same topic by different properties. And a topic has a
// direction, so the join can say what a Redis key cannot: this topic is produced
// and nobody in the workspace consumes it.

// KafkaUse is one produce or consume of a topic by one method of one service.
type KafkaUse struct {
	Service string `json:"service"`
	Path    string `json:"path"`
	NodeID  string `json:"nodeId"`
	Label   string `json:"label"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	// Topic is the topic as the code wrote it; Resolved is it with the service's
	// own config folded in, and holes as "*".
	Topic    string `json:"topic"`
	Resolved string `json:"resolved,omitempty"`
	Client   string `json:"client"`
	// GroupID is a consumer's group, folded the same way where it could be.
	GroupID string `json:"groupId,omitempty"`
	// Pattern is true for a consumer subscribed by regular expression.
	Pattern bool `json:"pattern,omitempty"`
}

// KafkaTopic is one topic and everything that produces to it or consumes from it.
type KafkaTopic struct {
	Topic     string     `json:"topic"`
	Producers []KafkaUse `json:"producers"`
	Consumers []KafkaUse `json:"consumers"`
	// Services touch this topic or one that overlaps it.
	Services []string `json:"services"`
	// CrossService is true when one service produces what a different one consumes.
	CrossService bool `json:"crossService"`
	// Gap names what the workspace lacks for this topic: "no-consumer" (produced, and
	// no service of the workspace consumes it — it may be read outside the
	// workspace) or "no-producer" (consumed, and nothing in the workspace writes it).
	Gap string `json:"gap,omitempty"`
}

// KafkaQuery asks who produces to and consumes from a topic. Topic is a name or
// a pattern with a * or {hole} (at least 3 fixed leading characters); empty
// lists every topic two or more services touch.
type KafkaQuery struct {
	Topic string
	// Gaps lists the topics with a gap instead: produced and unconsumed, or consumed and unproduced.
	Gaps  bool
	Limit int
}

// KafkaReport is the topics, and what was read to find them.
type KafkaReport struct {
	Query      string       `json:"query"`
	Topics     []KafkaTopic `json:"topics"`
	TopicTotal int          `json:"topicTotal"`
	// Unresolved are uses whose topic names a property the service's config does not set: they
	// cannot be matched and are listed rather than dropped.
	Unresolved []KafkaUse    `json:"unresolved,omitempty"`
	Services   []ServiceInfo `json:"services"`
	Notes      []string      `json:"notes,omitempty"`
}

// RunKafka answers who produces to and consumes from a topic across the workspace's services.
func RunKafka(services []Service, q KafkaQuery) (KafkaReport, error) {
	if q.Limit < 0 {
		return KafkaReport{}, fmt.Errorf("limit must be positive (got %d)", q.Limit)
	}
	limit := q.Limit
	if limit == 0 {
		limit = DefaultLimit / 2
	}
	limit = min(limit, MaxLimit)
	wanted := normalizeKey(strings.TrimSpace(q.Topic))
	if wanted != "" && !specific(wanted) {
		return KafkaReport{}, errors.New("topic is too generic: it needs at least 3 fixed characters before the first wildcard, or it matches every topic")
	}

	report := KafkaReport{Query: q.Topic, Topics: []KafkaTopic{}}
	byTopic := map[string]*KafkaTopic{}
	var regexConsumers []KafkaUse
	for i := range services {
		s := &services[i]
		report.Services = append(report.Services, infoOf(s))
		for _, a := range s.Snapshot.Kafka {
			use := KafkaUse{
				Service: s.Name, Path: s.Path, NodeID: a.From, Label: a.Label, File: a.File, Line: a.Line,
				Topic: a.Topic, Client: a.Client, Pattern: a.Pattern,
			}
			if a.GroupID != "" {
				use.GroupID, _ = s.Config.Resolve(a.GroupID)
			}
			folded, complete := s.Config.Resolve(a.Topic)
			if !complete {
				use.Resolved = folded
				report.Unresolved = append(report.Unresolved, use)
				continue
			}
			if a.Client == codegraph.KafkaCamel && strings.Contains(folded, "{") {
				report.Unresolved = append(report.Unresolved, use)
				continue
			}
			if a.Pattern {
				use.Resolved = folded
				regexConsumers = append(regexConsumers, use)
				continue
			}
			resolved := normalizeKey(folded)
			if !specific(resolved) {
				if a.Client == codegraph.KafkaCamel {
					// A runtime Camel source reference names no concrete topic;
					// show it as unresolved rather than joining it to everything.
					report.Unresolved = append(report.Unresolved, use)
				}
				continue
			}
			use.Resolved = resolved
			t := byTopic[resolved]
			if t == nil {
				t = &KafkaTopic{Topic: resolved, Producers: []KafkaUse{}, Consumers: []KafkaUse{}}
				byTopic[resolved] = t
			}
			if a.Role == codegraph.KafkaProduce {
				t.Producers = append(t.Producers, use)
			} else {
				t.Consumers = append(t.Consumers, use)
			}
		}
	}

	topics := make([]string, 0, len(byTopic))
	for p := range byTopic {
		topics = append(topics, p)
	}
	sort.Strings(topics)

	// A consumer subscribed by regular expression consumes every topic whose name it matches.
	for _, use := range regexConsumers {
		re, err := regexp.Compile("^(?:" + use.Resolved + ")$")
		if err != nil {
			report.Unresolved = append(report.Unresolved, use)
			continue
		}
		for _, name := range topics {
			if !strings.Contains(name, "*") && re.MatchString(name) {
				byTopic[name].Consumers = append(byTopic[name].Consumers, use)
			}
		}
	}

	for _, name := range topics {
		t := byTopic[name]
		touching, producing, consuming := map[string]bool{}, map[string]bool{}, map[string]bool{}
		clusterProducers, clusterConsumers := 0, 0
		for _, other := range topics {
			if other != name && !overlaps(name, other) {
				continue
			}
			for _, u := range byTopic[other].Producers {
				touching[u.Service], producing[u.Service] = true, true
				clusterProducers++
			}
			for _, u := range byTopic[other].Consumers {
				touching[u.Service], consuming[u.Service] = true, true
				clusterConsumers++
			}
		}
		for s := range touching {
			t.Services = append(t.Services, s)
		}
		sort.Strings(t.Services)
		for p := range producing {
			for c := range consuming {
				if p != c {
					t.CrossService = true
				}
			}
		}
		switch {
		case clusterProducers > 0 && clusterConsumers == 0:
			t.Gap = "no-consumer"
		case clusterConsumers > 0 && clusterProducers == 0:
			t.Gap = "no-producer"
		}
	}

	var out []KafkaTopic
	for _, name := range topics {
		t := byTopic[name]
		switch {
		case wanted != "":
			if !overlaps(wanted, name) {
				continue
			}
		case q.Gaps:
			if t.Gap == "" {
				continue
			}
		case len(t.Services) < 2:
			continue
		}
		out = append(out, *t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CrossService != out[j].CrossService {
			return out[i].CrossService
		}
		return out[i].Topic < out[j].Topic
	})
	report.TopicTotal = len(out)
	if len(out) > limit {
		out = out[:limit]
	}
	if out != nil {
		report.Topics = out
	}
	if len(report.Unresolved) > 50 {
		report.Unresolved = report.Unresolved[:50]
	}

	for _, s := range report.Services {
		if s.State != codegraph.SnapshotIndexed {
			report.Notes = append(report.Notes, fmt.Sprintf("%s is %s: its Kafka topics are not in this answer", s.Name, s.State))
		}
	}
	report.Notes = append(report.Notes,
		"read from each service's index and its own config: a topic named by a property is folded against the config of the service that wrote it (default profile only), and one whose property is not set there is listed under unresolved",
		"seen: @KafkaListener, KafkaTemplate.send, ProducerRecord, consumer.subscribe, Kafka Streams, resty.kafka.producer; not seen: Spring Cloud Stream bindings, @SendTo, sendDefault, a topic built elsewhere and passed in",
		"a gap is a gap in THIS workspace: a topic produced and unconsumed here may be consumed by a service that is not in it")
	return report, nil
}
