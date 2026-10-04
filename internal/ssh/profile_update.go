package ssh

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// UpdateProfileInput edits connection metadata without revealing or replacing
// credentials. Credentials remain owned by Accounts on this device.
type UpdateProfileInput struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Env      string `json:"env"`
}

func (s *Service) UpdateProfile(id string, input UpdateProfileInput) (ProfileSummary, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	input.Name = strings.TrimSpace(input.Name)
	input.Host = strings.TrimSpace(input.Host)
	input.Username = strings.TrimSpace(input.Username)
	if input.Name == "" || input.Username == "" {
		return ProfileSummary{}, errors.New("name and username are required")
	}
	if err := validateTargetHost(input.Host); err != nil {
		return ProfileSummary{}, err
	}
	if input.Port < 1 || input.Port > 65535 {
		return ProfileSummary{}, errors.New("port must be between 1 and 65535")
	}
	if !s.envs.Exists(input.Env) {
		return ProfileSummary{}, fmt.Errorf("unknown env tag %q", input.Env)
	}
	record, err := s.store.Get(id)
	if err != nil {
		return ProfileSummary{}, err
	}
	if record.Type != itemType {
		return ProfileSummary{}, errors.New("not an SSH profile")
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return ProfileSummary{}, err
	}
	if meta.Host != input.Host || meta.Port != input.Port {
		meta.HostKeyFingerprint = ""
	}
	meta.Name, meta.Host, meta.Port, meta.Username, meta.Env = input.Name, input.Host, input.Port, input.Username, input.Env
	raw, err := json.Marshal(meta)
	if err != nil {
		return ProfileSummary{}, err
	}
	updated, err := s.store.UpdateMetadata(id, raw)
	if err != nil {
		return ProfileSummary{}, err
	}
	s.stopForwardsForProfile(id)
	s.closeTerminalsForProfile(id)
	summary, err := summaryFromRecord(updated)
	summary.HasKeyPassphrase = s.hasKeyPassphrase(id)
	return summary, err
}
