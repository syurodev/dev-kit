package ssh

import (
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidateForwardSpec(t *testing.T) {
	tests := []struct {
		name string
		spec ForwardSpec
		want string
	}{
		{
			name: "local forwarding",
			spec: ForwardSpec{
				Direction:  ForwardDirectionLocal,
				ListenHost: "127.0.0.1",
				ListenPort: 0,
				TargetHost: "database.internal",
				TargetPort: 5432,
			},
		},
		{
			name: "remote forwarding",
			spec: ForwardSpec{
				Direction:  ForwardDirectionRemote,
				ListenHost: "localhost",
				ListenPort: 15432,
				TargetHost: "127.0.0.1",
				TargetPort: 5432,
			},
		},
		{
			name: "public bind is rejected",
			spec: ForwardSpec{
				Direction:  ForwardDirectionLocal,
				ListenHost: "0.0.0.0",
				ListenPort: 15432,
				TargetHost: "127.0.0.1",
				TargetPort: 5432,
			},
			want: "bind host must be loopback",
		},
		{
			name: "target port is required",
			spec: ForwardSpec{
				Direction:  ForwardDirectionLocal,
				ListenHost: "127.0.0.1",
				ListenPort: 15432,
				TargetHost: "127.0.0.1",
			},
			want: "target port must be between 1 and 65535",
		},
		{
			name: "target host cannot contain an address separator",
			spec: ForwardSpec{
				Direction:  ForwardDirectionLocal,
				ListenHost: "127.0.0.1",
				ListenPort: 15432,
				TargetHost: "https://internal.example",
				TargetPort: 443,
			},
			want: "target host is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateForwardSpec(tt.spec)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("validateForwardSpec() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateForwardSpec() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestStartLocalForwardProxiesAndStops(t *testing.T) {
	svc, _ := newServiceWithStore(t)
	profile, err := svc.CreateProfile(CreateProfileInput{
		Name:       "local",
		Host:       "127.0.0.1",
		Username:   "developer",
		AuthMethod: AuthPassword,
		Secret:     "password",
		Env:        "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	fake := &fakeForwardClient{}
	svc.forwardDial = func(context.Context, string, string) (forwardingClient, error) {
		return fake, nil
	}

	info, err := svc.StartForward(context.Background(), profile.ID, ForwardSpec{
		Direction:  ForwardDirectionLocal,
		ListenHost: "127.0.0.1",
		ListenPort: 0,
		TargetHost: "database.internal",
		TargetPort: 5432,
	}, "")
	if err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	if info.ListenPort == 0 {
		t.Fatal("StartForward returned no assigned local port")
	}

	local, err := net.Dial("tcp", net.JoinHostPort(info.ListenHost, strconv.Itoa(info.ListenPort)))
	if err != nil {
		t.Fatalf("dial local forward: %v", err)
	}
	defer local.Close()

	peer := fake.waitForPeer(t)
	go func() {
		payload := make([]byte, 4)
		if _, err := io.ReadFull(peer, payload); err == nil {
			_, _ = peer.Write([]byte("pong"))
		}
	}()

	if _, err := local.Write([]byte("ping")); err != nil {
		t.Fatalf("write through local forward: %v", err)
	}
	response := make([]byte, 4)
	if _, err := io.ReadFull(local, response); err != nil {
		t.Fatalf("read through local forward: %v", err)
	}
	if string(response) != "pong" {
		t.Fatalf("forward response = %q, want pong", response)
	}

	if err := svc.DeleteProfile(profile.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	if forwards := svc.ListForwards(); len(forwards) != 0 {
		t.Fatalf("active forwards after profile deletion = %d, want 0", len(forwards))
	}
	if err := svc.StopForward(info.ID); err != ErrForwardNotFound {
		t.Fatalf("StopForward after profile deletion = %v, want ErrForwardNotFound", err)
	}
}

func TestStartRemoteForwardDialsLocalTarget(t *testing.T) {
	svc, _ := newServiceWithStore(t)
	profile, err := svc.CreateProfile(CreateProfileInput{
		Name:       "remote",
		Host:       "127.0.0.1",
		Username:   "developer",
		AuthMethod: AuthPassword,
		Secret:     "password",
		Env:        "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen target: %v", err)
	}
	defer targetListener.Close()
	targetPort := targetListener.Addr().(*net.TCPAddr).Port
	go func() {
		connection, acceptErr := targetListener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		payload := make([]byte, 4)
		if _, readErr := io.ReadFull(connection, payload); readErr == nil {
			_, _ = connection.Write([]byte("pong"))
		}
	}()

	fake := &fakeForwardClient{}
	fake.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen remote forward: %v", err)
	}
	svc.forwardDial = func(context.Context, string, string) (forwardingClient, error) {
		return fake, nil
	}

	info, err := svc.StartForward(context.Background(), profile.ID, ForwardSpec{
		Direction:  ForwardDirectionRemote,
		ListenHost: "127.0.0.1",
		ListenPort: fake.listener.Addr().(*net.TCPAddr).Port,
		TargetHost: "127.0.0.1",
		TargetPort: targetPort,
	}, "")
	if err != nil {
		t.Fatalf("StartForward: %v", err)
	}

	remote, err := net.Dial("tcp", net.JoinHostPort(info.ListenHost, strconv.Itoa(info.ListenPort)))
	if err != nil {
		t.Fatalf("dial remote forward: %v", err)
	}
	defer remote.Close()
	if _, err := remote.Write([]byte("ping")); err != nil {
		t.Fatalf("write through remote forward: %v", err)
	}
	response := make([]byte, 4)
	if _, err := io.ReadFull(remote, response); err != nil {
		t.Fatalf("read through remote forward: %v", err)
	}
	if string(response) != "pong" {
		t.Fatalf("forward response = %q, want pong", response)
	}

	if err := svc.StopForward(info.ID); err != nil {
		t.Fatalf("StopForward: %v", err)
	}
}

func TestStopForwardReturnsNotFound(t *testing.T) {
	svc, _ := newServiceWithStore(t)
	if err := svc.StopForward("000000000000000000000000"); err != ErrForwardNotFound {
		t.Fatalf("StopForward error = %v, want ErrForwardNotFound", err)
	}
}

// fakeForwardClient lets the forwarding lifecycle test exercise real TCP
// listener and proxy behavior without requiring an SSH daemon in CI.
type fakeForwardClient struct {
	mu       sync.Mutex
	peer     net.Conn
	listener net.Listener
}

func (c *fakeForwardClient) DialContext(context.Context, string, string) (net.Conn, error) {
	client, peer := net.Pipe()
	c.mu.Lock()
	c.peer = peer
	c.mu.Unlock()
	return client, nil
}

func (c *fakeForwardClient) Listen(string, string) (net.Listener, error) {
	c.mu.Lock()
	listener := c.listener
	c.mu.Unlock()
	if listener == nil {
		return nil, net.ErrClosed
	}
	return listener, nil
}

func (c *fakeForwardClient) Close() error {
	c.mu.Lock()
	peer := c.peer
	listener := c.listener
	c.peer = nil
	c.listener = nil
	c.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	if peer != nil {
		return peer.Close()
	}
	return nil
}

func (c *fakeForwardClient) waitForPeer(t *testing.T) net.Conn {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		peer := c.peer
		c.mu.Unlock()
		if peer != nil {
			return peer
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("forward client did not receive a tunneled connection")
	return nil
}
