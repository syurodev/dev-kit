package mcp

import (
	"context"
	"os"
	"testing"
	"time"
)

// ListenUnix binds without accepting: the bind error is returned to the caller
// (startup can fail on it) and a socket another live instance answers on is never
// taken over.
func TestListenUnixBindsOwnerOnlyAndRefusesALiveSocket(t *testing.T) {
	sock := testSocketPath(t)
	ln, err := ListenUnix(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	info, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, want 0600", info.Mode().Perm())
	}

	accepted := make(chan struct{})
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
			close(accepted)
		}
	}()
	if second, err := ListenUnix(sock); err == nil {
		second.Close()
		t.Fatal("a socket a live listener answers on must not be taken over")
	}
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("the probe connection never reached the live listener")
	}
}

func TestServeListenerClosesAndRemovesTheSocketOnCancel(t *testing.T) {
	sock := testSocketPath(t)
	ln, err := ListenUnix(sock)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(testDeps(&fakeCaller{}, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeListener(ctx, ln, server) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeListener: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeListener did not return after cancel")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(sock); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the socket file was not removed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
