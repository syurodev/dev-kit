package ssh

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	// ForwardDirectionLocal is equivalent to OpenSSH's local (-L) forwarding:
	// the application listens locally and opens the target connection through
	// the SSH server.
	ForwardDirectionLocal = "local"
	// ForwardDirectionRemote is equivalent to OpenSSH's remote (-R) forwarding:
	// the SSH server listens and the application opens the target locally.
	ForwardDirectionRemote = "remote"
)

var ErrForwardNotFound = errors.New("SSH forward not found")

// ForwardSpec describes one TCP forwarding rule. ListenHost is deliberately
// restricted to loopback addresses so a started tunnel cannot become an
// accidental LAN-facing proxy. For local forwarding, TargetHost/TargetPort
// are resolved from the SSH server; for remote forwarding, they are resolved
// by the DevKit process.
type ForwardSpec struct {
	Direction  string `json:"direction"`
	ListenHost string `json:"listen_host"`
	ListenPort int    `json:"listen_port"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
}

// ForwardInfo is the safe runtime representation returned to the bridge. It
// contains no credentials, passphrases, private keys, or SSH client details.
type ForwardInfo struct {
	ID         string `json:"id"`
	ProfileID  string `json:"profile_id"`
	Direction  string `json:"direction"`
	ListenHost string `json:"listen_host"`
	ListenPort int    `json:"listen_port"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
}

// forwardingClient is the small part of *ssh.Client required by forwarding.
// Keeping this interface narrow makes lifecycle and proxy tests deterministic
// without requiring an SSH daemon in every test environment.
type forwardingClient interface {
	DialContext(context.Context, string, string) (net.Conn, error)
	Listen(string, string) (net.Listener, error)
	Close() error
}

type forwardSession struct {
	info     ForwardInfo
	client   forwardingClient
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once
	workers  sync.WaitGroup
}

func validateForwardSpec(spec ForwardSpec) error {
	switch spec.Direction {
	case ForwardDirectionLocal, ForwardDirectionRemote:
	default:
		if spec.Direction == "" {
			return errors.New("forward direction is required")
		}
		return errors.New("unsupported forward direction")
	}
	if !isLoopbackBindHost(spec.ListenHost) {
		return errors.New("bind host must be loopback")
	}
	if spec.ListenPort < 0 || spec.ListenPort > 65535 {
		return errors.New("bind port must be between 0 and 65535")
	}
	if err := validateTargetHost(spec.TargetHost); err != nil {
		return err
	}
	if spec.TargetPort < 1 || spec.TargetPort > 65535 {
		return errors.New("target port must be between 1 and 65535")
	}
	return nil
}

func isLoopbackBindHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateTargetHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return errors.New("target host is required")
	}
	// TargetHost is passed to net.JoinHostPort and then to either the SSH
	// server or the local dialer. Reject URL/path syntax and control characters
	// instead of letting an ambiguous value reach either network boundary.
	if strings.ContainsAny(host, "\x00\r\n\t /\\") {
		return errors.New("target host is invalid")
	}
	return nil
}

func newForwardID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// StartForward establishes an SSH client and starts a runtime-only forwarding
// session. The caller's context applies to SSH setup only; once the session is
// started, StopForward or Service.Close owns its lifetime.
func (s *Service) StartForward(ctx context.Context, profileID string, spec ForwardSpec, passphrase string) (ForwardInfo, error) {
	if err := validateForwardSpec(spec); err != nil {
		return ForwardInfo{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.forwardDial == nil {
		return ForwardInfo{}, errors.New("SSH forwarding is not initialized")
	}

	client, err := s.forwardDial(ctx, profileID, passphrase)
	if err != nil {
		return ForwardInfo{}, err
	}
	listener, err := s.openForwardListener(client, spec)
	if err != nil {
		_ = client.Close()
		return ForwardInfo{}, err
	}

	id, err := newForwardID()
	if err != nil {
		_ = listener.Close()
		_ = client.Close()
		return ForwardInfo{}, err
	}
	forwardCtx, cancel := context.WithCancel(context.Background())
	info := forwardInfo(id, profileID, spec, listener.Addr())
	session := &forwardSession{
		info:     info,
		client:   client,
		listener: listener,
		ctx:      forwardCtx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	s.forwardMu.Lock()
	s.forwards[id] = session
	s.forwardMu.Unlock()
	go s.serveForward(session)
	return info, nil
}

func (s *Service) openForwardListener(client forwardingClient, spec ForwardSpec) (net.Listener, error) {
	address := net.JoinHostPort(spec.ListenHost, strconv.Itoa(spec.ListenPort))
	if spec.Direction == ForwardDirectionLocal {
		return net.Listen("tcp", address)
	}
	return client.Listen("tcp", address)
}

func forwardInfo(id, profileID string, spec ForwardSpec, address net.Addr) ForwardInfo {
	listenHost := spec.ListenHost
	listenPort := spec.ListenPort
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		if tcpAddress.IP != nil {
			listenHost = tcpAddress.IP.String()
		}
		listenPort = tcpAddress.Port
	} else if host, port, err := net.SplitHostPort(address.String()); err == nil {
		listenHost = host
		if parsedPort, parseErr := strconv.Atoi(port); parseErr == nil {
			listenPort = parsedPort
		}
	}
	return ForwardInfo{
		ID:         id,
		ProfileID:  profileID,
		Direction:  spec.Direction,
		ListenHost: listenHost,
		ListenPort: listenPort,
		TargetHost: spec.TargetHost,
		TargetPort: spec.TargetPort,
	}
}

func (s *Service) serveForward(session *forwardSession) {
	defer close(session.done)
	defer s.removeForward(session.info.ID, session)

	for {
		connection, err := session.listener.Accept()
		if err != nil {
			// Closing the listener is the normal stop path. Any other accept
			// error also terminates this forward instead of retrying forever.
			break
		}
		session.workers.Add(1)
		go func() {
			defer session.workers.Done()
			s.proxyConnection(session, connection)
		}()
	}

	session.stop()
	session.workers.Wait()
}

func (s *Service) proxyConnection(session *forwardSession, incoming net.Conn) {
	defer incoming.Close()

	targetAddress := net.JoinHostPort(session.info.TargetHost, strconv.Itoa(session.info.TargetPort))
	var (
		outgoing net.Conn
		err      error
	)
	if session.info.Direction == ForwardDirectionLocal {
		outgoing, err = session.client.DialContext(session.ctx, "tcp", targetAddress)
	} else {
		outgoing, err = (&net.Dialer{}).DialContext(session.ctx, "tcp", targetAddress)
	}
	if err != nil {
		return
	}
	defer outgoing.Close()

	proxyBidirectional(incoming, outgoing)
}

func proxyBidirectional(left net.Conn, right net.Conn) {
	done := make(chan struct{}, 2)
	copyStream := func(destination net.Conn, source net.Conn) {
		_, _ = io.Copy(destination, source)
		done <- struct{}{}
	}
	go copyStream(right, left)
	go copyStream(left, right)

	// Closing both sockets after the first direction finishes unblocks the
	// opposite io.Copy and prevents one half-open TCP connection from keeping
	// the forward goroutine alive indefinitely.
	<-done
	_ = left.Close()
	_ = right.Close()
	<-done
}

// StopForward closes the listener, SSH client, and all active proxy streams,
// then waits until the forwarding goroutine has exited.
func (s *Service) StopForward(id string) error {
	s.forwardMu.Lock()
	session, ok := s.forwards[id]
	if ok {
		delete(s.forwards, id)
	}
	s.forwardMu.Unlock()
	if !ok {
		return ErrForwardNotFound
	}

	session.stop()
	<-session.done
	return nil
}

// ListForwards returns active runtime sessions only. Forward definitions are
// intentionally not persisted because they contain network routing intent and
// must be explicitly started by the user after each application launch.
func (s *Service) ListForwards() []ForwardInfo {
	s.forwardMu.Lock()
	forwards := make([]ForwardInfo, 0, len(s.forwards))
	for _, session := range s.forwards {
		forwards = append(forwards, session.info)
	}
	s.forwardMu.Unlock()
	sort.Slice(forwards, func(i, j int) bool { return forwards[i].ID < forwards[j].ID })
	return forwards
}

// Close stops every active forward. It is called from the application
// composition root during shutdown so SSH clients do not outlive the app.
func (s *Service) Close() {
	s.forwardMu.Lock()
	sessions := make([]*forwardSession, 0, len(s.forwards))
	for id, session := range s.forwards {
		delete(s.forwards, id)
		sessions = append(sessions, session)
	}
	s.forwardMu.Unlock()

	for _, session := range sessions {
		session.stop()
	}
	for _, session := range sessions {
		<-session.done
	}
}

func (s *Service) stopForwardsForProfile(profileID string) {
	s.forwardMu.Lock()
	sessions := make([]*forwardSession, 0)
	for id, session := range s.forwards {
		if session.info.ProfileID != profileID {
			continue
		}
		delete(s.forwards, id)
		sessions = append(sessions, session)
	}
	s.forwardMu.Unlock()

	for _, session := range sessions {
		session.stop()
	}
	for _, session := range sessions {
		<-session.done
	}
}

func (s *Service) removeForward(id string, session *forwardSession) {
	s.forwardMu.Lock()
	if current, ok := s.forwards[id]; ok && current == session {
		delete(s.forwards, id)
	}
	s.forwardMu.Unlock()
}

func (session *forwardSession) stop() {
	session.stopOnce.Do(func() {
		session.cancel()
		_ = session.listener.Close()
		_ = session.client.Close()
	})
}
