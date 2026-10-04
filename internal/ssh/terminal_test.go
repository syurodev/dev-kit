package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"devkit/internal/terminal"
	gossh "golang.org/x/crypto/ssh"
)

// A real SSH protocol peer on loopback exercises auth, pinning, PTY, resizing,
// byte transport, exit codes and cleanup without an external sshd or account.
func terminalPeer(t *testing.T) (string, int, string, <-chan string) {
	t.Helper()
	key, _ := newHostKey(t)
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := &gossh.ServerConfig{PasswordCallback: func(meta gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
		if meta.User() != "deploy" || string(password) != "secret" {
			return nil, errors.New("bad credentials")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	requests := make(chan string, 32)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				server, channels, globals, err := gossh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go gossh.DiscardRequests(globals)
				for incoming := range channels {
					if incoming.ChannelType() != "session" {
						_ = incoming.Reject(gossh.UnknownChannelType, "session only")
						continue
					}
					channel, reqs, err := incoming.Accept()
					if err != nil {
						return
					}
					go func() {
						defer channel.Close()
						for req := range reqs {
							requests <- req.Type
							if req.WantReply {
								_ = req.Reply(true, nil)
							}
							if req.Type == "shell" {
								go func() {
									_, _ = io.WriteString(channel, "ready\r\n")
									data := make([]byte, 256)
									for {
										n, err := channel.Read(data)
										if n > 0 {
											if strings.Contains(string(data[:n]), "disconnect") {
												_ = server.Close()
												return
											}
											if strings.Contains(string(data[:n]), "exit") {
												_, _ = channel.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{7}))
												_ = channel.Close()
												return
											}
											_, _ = channel.Write(data[:n])
										}
										if err != nil {
											return
										}
									}
								}()
							}
						}
					}()
				}
			}()
		}
	}()
	addr := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, gossh.FingerprintSHA256(signer.PublicKey()), requests
}

func terminalFixture(t *testing.T) (*Service, *terminal.Registry, ProfileSummary, <-chan string, <-chan int, <-chan string) {
	t.Helper()
	host, port, pin, requests := terminalPeer(t)
	svc := newService(t)
	p, err := svc.CreateProfile(CreateProfileInput{Name: "test", Host: host, Port: port, Username: "deploy", Secret: "secret", Env: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.TrustHostKey(context.Background(), p.ID, pin); err != nil {
		t.Fatal(err)
	}
	output := make(chan string, 32)
	exit := make(chan int, 8)
	reg := terminal.NewRegistry(func(_ string, bytes []byte) { output <- string(bytes) })
	reg.OnExit = func(_ string, code int) { exit <- code }
	svc.SetTerminalRegistry(reg)
	t.Cleanup(func() { svc.Close(); reg.CloseAll() })
	return svc, reg, p, output, exit, requests
}

func receive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}

func TestRemoteTerminalProtocolAndExit(t *testing.T) {
	svc, reg, p, output, exit, requests := terminalFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	id, err := svc.StartTerminal(ctx, p.ID, "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	cancel() // A completed bridge call must not kill a registered terminal.
	if owner, ok := reg.Owner(id); !ok || owner != "builtin.ssh" {
		t.Fatalf("owner=%q exists=%v", owner, ok)
	}
	if req := receive(t, requests); req != "pty-req" {
		t.Fatalf("request=%q", req)
	}
	if req := receive(t, requests); req != "shell" {
		t.Fatalf("request=%q", req)
	}
	if got := receive(t, output); got != "ready\r\n" {
		t.Fatalf("output=%q", got)
	}
	if err := reg.Resize(id, 40, 120); err != nil {
		t.Fatal(err)
	}
	if req := receive(t, requests); req != "window-change" {
		t.Fatalf("request=%q", req)
	}
	if err := reg.Write(id, []byte("hello ✓\n")); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, output); got != "hello ✓\n" {
		t.Fatalf("output=%q", got)
	}
	if err := reg.Write(id, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	if code := receive(t, exit); code != 7 {
		t.Fatalf("exit=%d", code)
	}
}

func TestRemoteTerminalStopsOnConnectionEditAndDelete(t *testing.T) {
	for _, action := range []string{"edit", "delete", "close"} {
		t.Run(action, func(t *testing.T) {
			svc, reg, p, output, exit, _ := terminalFixture(t)
			id, err := svc.StartTerminal(context.Background(), p.ID, "", 24, 80)
			if err != nil {
				t.Fatal(err)
			}
			receive(t, output)
			switch action {
			case "edit":
				_, err = svc.UpdateProfile(p.ID, UpdateProfileInput{Name: "renamed", Host: p.Host, Port: p.Port, Username: p.Username, Env: p.Env})
			case "delete":
				err = svc.DeleteProfile(p.ID)
			case "close":
				err = reg.Close(id)
			}
			if err != nil {
				t.Fatal(err)
			}
			if code := receive(t, exit); code != -1 {
				t.Fatalf("exit=%d", code)
			}
			svc.mu.Lock()
			count := len(svc.terminals)
			svc.mu.Unlock()
			if count != 0 {
				t.Fatalf("leaked %d sessions", count)
			}
		})
	}
}

func TestRemoteTerminalRejectsUntrustedAndInvalidSize(t *testing.T) {
	svc, _, p, _, _, _ := terminalFixture(t)
	if _, err := svc.StartTerminal(context.Background(), p.ID, "", 0, 80); err == nil {
		t.Fatal("accepted invalid size")
	}
	updated, err := svc.UpdateProfile(p.ID, UpdateProfileInput{Name: p.Name, Host: p.Host, Port: p.Port + 1, Username: p.Username, Env: p.Env})
	if err != nil {
		t.Fatal(err)
	}
	// Restore the reachable address without restoring the pin.
	_, err = svc.UpdateProfile(p.ID, UpdateProfileInput{Name: p.Name, Host: p.Host, Port: p.Port, Username: p.Username, Env: p.Env})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartTerminal(context.Background(), updated.ID, "", 24, 80); err == nil || !strings.Contains(fmt.Sprint(err), "not trusted") {
		t.Fatalf("untrusted connection: %v", err)
	}
}

func TestSSHHandshakeCancellationClosesUnresponsivePeer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	svc := newService(t)
	p, err := svc.CreateProfile(CreateProfileInput{Name: "stalled", Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Username: "deploy", Secret: "secret", Env: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		client, err := svc.dial(ctx, p.ID, "")
		if client != nil {
			client.Close()
		}
		finished <- err
	}()
	conn := receive(t, accepted)
	defer conn.Close()
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancellation succeeded unexpectedly")
		}
	case <-time.After(time.Second):
		t.Fatal("handshake ignored cancellation")
	}
}

func TestRemoteTerminalDisconnectAndReconnect(t *testing.T) {
	svc, reg, p, output, exit, _ := terminalFixture(t)
	id, err := svc.StartTerminal(context.Background(), p.ID, "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	receive(t, output)
	if err := reg.Write(id, []byte("disconnect\n")); err != nil {
		t.Fatal(err)
	}
	if code := receive(t, exit); code != -1 {
		t.Fatalf("disconnect exit=%d", code)
	}
	// A lost transport ends only this session. The pinned profile can open another.
	if _, err := svc.StartTerminal(context.Background(), p.ID, "", 24, 80); err != nil {
		t.Fatal(err)
	}
	if greeting := receive(t, output); !strings.Contains(greeting, "ready") {
		t.Fatalf("reconnect output=%q", greeting)
	}
}
