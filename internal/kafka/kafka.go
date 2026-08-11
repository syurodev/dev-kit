package kafka

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"devkit/internal/vaultitem"
)

const (
	itemType    = "kafka-profile"
	secretKeyID = "kafka-secret"
)

// EnvLookup is the narrow view of the environments registry the Kafka service
// needs to validate a profile's env tag. Declared here (the consumer), not by
// internal/environments — README.md "Accept interfaces, return structs".
type EnvLookup interface {
	Exists(name string) bool
}

// Store is the shared vault-item substrate the Kafka service needs.
type Store interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	Delete(id string) error
}

// ProfileSummary is a Kafka connection profile without the (encrypted) secret.
type ProfileSummary struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	BootstrapServers []string `json:"bootstrap_servers"`
	SecurityProtocol string   `json:"security_protocol"`
	SASLMechanism    string   `json:"sasl_mechanism"`
	Username         string   `json:"username"`
	Env              string   `json:"env"`
}

type CreateProfileInput struct {
	Name             string   `json:"name"`
	BootstrapServers []string `json:"bootstrap_servers"`
	SecurityProtocol string   `json:"security_protocol"`
	SASLMechanism    string   `json:"sasl_mechanism"`
	Username         string   `json:"username"`
	Secret           string   `json:"secret"`
	Env              string   `json:"env"`
}

// metadata is the plaintext, non-sensitive connection metadata for a profile.
type metadata struct {
	Name             string   `json:"name"`
	BootstrapServers []string `json:"bootstrap_servers"`
	SecurityProtocol string   `json:"security_protocol"`
	SASLMechanism    string   `json:"sasl_mechanism"`
	Username         string   `json:"username"`
	Env              string   `json:"env"`
}

type Service struct {
	store Store
	envs  EnvLookup
	dial  DialFunc

	mu            sync.Mutex
	active        BrokerClient
	activeProfile string
}

func NewService(store Store, envs EnvLookup, dial DialFunc) *Service {
	if dial == nil {
		dial = DialFranz
	}
	return &Service{store: store, envs: envs, dial: dial}
}

func (s *Service) CreateProfile(input CreateProfileInput) (ProfileSummary, error) {
	if input.Name == "" {
		return ProfileSummary{}, errors.New("name is required")
	}
	if len(input.BootstrapServers) == 0 {
		return ProfileSummary{}, errors.New("bootstrap_servers is required")
	}
	if input.Env == "" {
		return ProfileSummary{}, errors.New("env is required")
	}
	if !s.envs.Exists(input.Env) {
		return ProfileSummary{}, fmt.Errorf("unknown env tag %q", input.Env)
	}
	if err := normalizeSecurity(&input); err != nil {
		return ProfileSummary{}, err
	}

	meta, err := json.Marshal(metadata{
		Name:             input.Name,
		BootstrapServers: input.BootstrapServers,
		SecurityProtocol: input.SecurityProtocol,
		SASLMechanism:    input.SASLMechanism,
		Username:         input.Username,
		Env:              input.Env,
	})
	if err != nil {
		return ProfileSummary{}, err
	}
	record, err := s.store.Create(itemType, secretKeyID, meta, []byte(input.Secret))
	if err != nil {
		return ProfileSummary{}, err
	}
	return summaryFromRecord(record)
}

func (s *Service) ListProfiles() ([]ProfileSummary, error) {
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	summaries := make([]ProfileSummary, 0, len(records))
	for _, record := range records {
		summary, err := summaryFromRecord(record)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func (s *Service) DeleteProfile(id string) error {
	s.mu.Lock()
	if s.activeProfile == id {
		if s.active != nil {
			s.active.Close()
		}
		s.active = nil
		s.activeProfile = ""
	}
	s.mu.Unlock()
	return s.store.Delete(id)
}

func normalizeSecurity(input *CreateProfileInput) error {
	if input.SecurityProtocol == "" {
		input.SecurityProtocol = "PLAINTEXT"
	}
	switch input.SecurityProtocol {
	case "PLAINTEXT", "SSL":
		return nil
	case "SASL_PLAINTEXT", "SASL_SSL":
		switch input.SASLMechanism {
		case "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512":
			if input.Username == "" {
				return errors.New("username is required for SASL authentication")
			}
			return nil
		default:
			if input.SASLMechanism == "" {
				return errors.New("sasl_mechanism is required for SASL security protocols")
			}
			return errors.New("unsupported sasl mechanism")
		}
	default:
		return errors.New("unsupported security protocol")
	}
}

func decodeMetadata(record vaultitem.Record) (metadata, error) {
	var meta metadata
	if err := json.Unmarshal(record.Metadata, &meta); err != nil {
		return metadata{}, err
	}
	return meta, nil
}

func summaryFromRecord(record vaultitem.Record) (ProfileSummary, error) {
	meta, err := decodeMetadata(record)
	if err != nil {
		return ProfileSummary{}, err
	}
	return ProfileSummary{
		ID:               record.ID,
		Name:             meta.Name,
		BootstrapServers: meta.BootstrapServers,
		SecurityProtocol: meta.SecurityProtocol,
		SASLMechanism:    meta.SASLMechanism,
		Username:         meta.Username,
		Env:              meta.Env,
	}, nil
}
