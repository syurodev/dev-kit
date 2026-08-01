package ssh

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"changeme/internal/services/vaultitem"

	gossh "golang.org/x/crypto/ssh"
)

const (
	AuthPassword = "password"
	AuthKey      = "key"

	secretKeyID = "ssh-secret"
	itemType    = "ssh-profile"
)

// Store is the shared vault-item substrate the SSH service needs.
type Store interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	UpdateMetadata(id string, metadata json.RawMessage) (vaultitem.Record, error)
	Delete(id string) error
}

// metadata is the plaintext, non-sensitive SSH connection metadata.
type metadata struct {
	Name               string `json:"name"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	AuthMethod         string `json:"auth_method"`
	HostKeyFingerprint string `json:"host_key_fingerprint"`
}

// ProfileSummary is an SSH profile without the (encrypted) secret.
type ProfileSummary struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	AuthMethod         string `json:"auth_method"`
	HostKeyFingerprint string `json:"host_key_fingerprint"`
}

type CreateProfileInput struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthMethod string `json:"auth_method"`
	// Secret is the password (auth_method "password") or the private key PEM
	// (auth_method "key").
	Secret string `json:"secret"`
}

type CommandResult struct {
	Output string `json:"output"`
}

type Service struct {
	store       Store
	forwardMu   sync.Mutex
	forwards    map[string]*forwardSession
	forwardDial func(context.Context, string, string) (forwardingClient, error)
}

func NewService(store Store) *Service {
	service := &Service{
		store:    store,
		forwards: make(map[string]*forwardSession),
	}
	service.forwardDial = func(ctx context.Context, id string, passphrase string) (forwardingClient, error) {
		return service.dial(ctx, id, passphrase)
	}
	return service
}

func (s *Service) CreateProfile(input CreateProfileInput) (ProfileSummary, error) {
	if input.Name == "" {
		return ProfileSummary{}, errors.New("name is required")
	}
	if input.Host == "" {
		return ProfileSummary{}, errors.New("host is required")
	}
	if input.Username == "" {
		return ProfileSummary{}, errors.New("username is required")
	}
	if input.Port == 0 {
		input.Port = 22
	}
	if input.AuthMethod == "" {
		input.AuthMethod = AuthPassword
	}
	if input.AuthMethod != AuthPassword && input.AuthMethod != AuthKey {
		return ProfileSummary{}, errors.New("unsupported auth method")
	}

	meta, err := json.Marshal(metadata{
		Name:       input.Name,
		Host:       input.Host,
		Port:       input.Port,
		Username:   input.Username,
		AuthMethod: input.AuthMethod,
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
	// A profile owns the SSH client used by its forwards. Stop those runtime
	// sessions before deleting the profile so a deleted credential cannot leave
	// an orphaned network path alive in the process.
	s.stopForwardsForProfile(id)
	return s.store.Delete(id)
}

// TestConnection dials, authenticates, and opens a session to verify the profile.
func (s *Service) TestConnection(ctx context.Context, id string, passphrase string) error {
	client, err := s.dial(ctx, id, passphrase)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	return session.Close()
}

// RunCommand runs a command over SSH and returns its combined output. A non-zero
// exit status still returns the output (with a nil error); only connection or
// session failures return an error.
func (s *Service) RunCommand(ctx context.Context, id string, command string, passphrase string) (CommandResult, error) {
	client, err := s.dial(ctx, id, passphrase)
	if err != nil {
		return CommandResult{}, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return CommandResult{}, err
	}
	defer session.Close()

	out, err := session.CombinedOutput(command)
	result := CommandResult{Output: string(out)}
	var exitErr *gossh.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return CommandResult{}, err
	}
	return result, nil
}

// ObserveHostKey gets the server's fingerprint without authenticating or
// storing it. The caller must present this value for an explicit trust action.
func (s *Service) ObserveHostKey(ctx context.Context, id string) (string, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return "", err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return "", err
	}
	addr := net.JoinHostPort(meta.Host, strconv.Itoa(meta.Port))
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	var observed string
	config := &gossh.ClientConfig{
		User:            meta.Username,
		HostKeyCallback: observeHostKeyCallback(&observed),
		Timeout:         10 * time.Second,
	}
	_, _, _, err = gossh.NewClientConn(conn, addr, config)
	if observed != "" && errors.Is(err, errHostKeyObserved) {
		return observed, nil
	}
	if err != nil {
		return "", err
	}
	return "", errors.New("ssh server did not provide a host key")
}

// TrustHostKey persists a fingerprint only after a fresh observation confirms
// the exact value the user approved.
func (s *Service) TrustHostKey(ctx context.Context, id string, fingerprint string) error {
	if fingerprint == "" {
		return errors.New("host key fingerprint is required")
	}
	observed, err := s.ObserveHostKey(ctx, id)
	if err != nil {
		return err
	}
	if observed != fingerprint {
		return errors.New("observed host key fingerprint does not match approval")
	}
	record, err := s.store.Get(id)
	if err != nil {
		return err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return err
	}
	meta.HostKeyFingerprint = fingerprint
	updated, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = s.store.UpdateMetadata(id, updated)
	return err
}

func (s *Service) dial(ctx context.Context, id string, passphrase string) (*gossh.Client, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return nil, err
	}
	secret, err := s.store.OpenPayload(record, secretKeyID)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(secret)

	var auth gossh.AuthMethod
	switch meta.AuthMethod {
	case AuthPassword:
		auth = gossh.Password(string(secret))
	case AuthKey:
		signer, err := parsePrivateKey(secret, passphrase)
		if err != nil {
			return nil, err
		}
		auth = gossh.PublicKeys(signer)
	default:
		return nil, errors.New("unsupported auth method")
	}

	config := &gossh.ClientConfig{
		User:            meta.Username,
		Auth:            []gossh.AuthMethod{auth},
		HostKeyCallback: pinnedHostKeyCallback(meta.HostKeyFingerprint),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(meta.Host, strconv.Itoa(meta.Port))
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	clientConn, chans, reqs, err := gossh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return gossh.NewClient(clientConn, chans, reqs), nil
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
		ID:                 record.ID,
		Name:               meta.Name,
		Host:               meta.Host,
		Port:               meta.Port,
		Username:           meta.Username,
		AuthMethod:         meta.AuthMethod,
		HostKeyFingerprint: meta.HostKeyFingerprint,
	}, nil
}
