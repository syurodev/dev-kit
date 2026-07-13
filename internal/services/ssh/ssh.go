package ssh

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"sort"
	"strconv"
	"time"

	"changeme/internal/crypto"

	gossh "golang.org/x/crypto/ssh"
)

const (
	AuthPassword = "password"
	AuthKey      = "key"

	secretKeyID          = "ssh-secret"
	sshProfileRecordType = "ssh-profile"
)

// Vault is the encryption capability the SSH service needs. The DEK stays inside
// the vault; the service only gets seal/open.
type Vault interface {
	SealV2(associatedData crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
	OpenWithAD(envelope crypto.Envelope, associatedData crypto.AssociatedData) ([]byte, error)
}

// ProfileRecord is the at-rest form of an SSH profile. The secret (password or
// private key PEM, depending on AuthMethod) is encrypted; the rest is
// non-sensitive connection metadata.
type ProfileRecord struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Host               string          `json:"host"`
	Port               int             `json:"port"`
	Username           string          `json:"username"`
	AuthMethod         string          `json:"auth_method"`
	HostKeyFingerprint string          `json:"host_key_fingerprint"`
	Secret             crypto.Envelope `json:"secret"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type Repository interface {
	List() ([]ProfileRecord, error)
	Get(id string) (ProfileRecord, error)
	Put(record ProfileRecord) error
	Delete(id string) error
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
	repo  Repository
	vault Vault
}

func NewService(repo Repository, vault Vault) *Service {
	return &Service{repo: repo, vault: vault}
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

	id, err := newID()
	if err != nil {
		return ProfileSummary{}, err
	}
	envelope, err := s.vault.SealV2(crypto.AssociatedData{RecordType: sshProfileRecordType, RecordID: id, KeyID: secretKeyID}, []byte(input.Secret))
	if err != nil {
		return ProfileSummary{}, err
	}
	now := time.Now().UTC()
	record := ProfileRecord{
		ID:         id,
		Name:       input.Name,
		Host:       input.Host,
		Port:       input.Port,
		Username:   input.Username,
		AuthMethod: input.AuthMethod,
		Secret:     envelope,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repo.Put(record); err != nil {
		return ProfileSummary{}, err
	}
	return toSummary(record), nil
}

func (s *Service) ListProfiles() ([]ProfileSummary, error) {
	records, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	summaries := make([]ProfileSummary, 0, len(records))
	for _, record := range records {
		summaries = append(summaries, toSummary(record))
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func (s *Service) DeleteProfile(id string) error {
	return s.repo.Delete(id)
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
	record, err := s.repo.Get(id)
	if err != nil {
		return "", err
	}
	addr := net.JoinHostPort(record.Host, strconv.Itoa(record.Port))
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	var observed string
	config := &gossh.ClientConfig{
		User:            record.Username,
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
	record, err := s.repo.Get(id)
	if err != nil {
		return err
	}
	record.HostKeyFingerprint = fingerprint
	record.UpdatedAt = time.Now().UTC()
	return s.repo.Put(record)
}

func (s *Service) dial(ctx context.Context, id string, passphrase string) (*gossh.Client, error) {
	record, err := s.repo.Get(id)
	if err != nil {
		return nil, err
	}
	secret, err := s.vault.OpenWithAD(record.Secret, sshProfileAssociatedData(record))
	if err != nil {
		return nil, err
	}
	defer zeroBytes(secret)

	var auth gossh.AuthMethod
	switch record.AuthMethod {
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
		User:            record.Username,
		Auth:            []gossh.AuthMethod{auth},
		HostKeyCallback: pinnedHostKeyCallback(record.HostKeyFingerprint),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(record.Host, strconv.Itoa(record.Port))
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

func sshProfileAssociatedData(record ProfileRecord) crypto.AssociatedData {
	return crypto.AssociatedData{RecordType: sshProfileRecordType, RecordID: record.ID, KeyID: secretKeyID}
}

func toSummary(record ProfileRecord) ProfileSummary {
	return ProfileSummary{
		ID:                 record.ID,
		Name:               record.Name,
		Host:               record.Host,
		Port:               record.Port,
		Username:           record.Username,
		AuthMethod:         record.AuthMethod,
		HostKeyFingerprint: record.HostKeyFingerprint,
	}
}

func newID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
