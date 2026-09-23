package codegraph

import (
	"sync/atomic"
	"testing"
)

// Close leaves the store to the container that built the Service; Shutdown
// is how the Registry evicts one, and must not leak it.
func TestShutdownClosesTheStoreCloseDoesNot(t *testing.T) {
	store := &closeCountingStore{Store: NewNilStore()}
	svc := NewService(store, nil)

	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if n := store.closed; n != 0 {
		t.Fatalf("Close closed the store %d time(s); it belongs to the container", n)
	}
	if err := svc.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if n := store.closed; n != 1 {
		t.Fatalf("Shutdown closed the store %d time(s), want 1", n)
	}
	if err := svc.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if n := store.closed; n != 1 {
		t.Fatalf("a second Shutdown closed the store again (%d)", n)
	}
}

// A query still reading when Shutdown runs keeps its store until it is done.
func TestShutdownWaitsForAReaderBeforeClosing(t *testing.T) {
	store := &closeCountingStore{Store: NewNilStore()}
	svc := NewService(store, nil)
	_, release, ok := svc.lease.acquire()
	if !ok {
		t.Fatal("could not borrow the store")
	}
	if err := svc.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if n := store.closed; n != 0 {
		t.Fatal("Shutdown closed the store under a reader")
	}
	release()
	if n := store.closed; n != 1 {
		t.Fatalf("store closed %d time(s) after the reader finished, want 1", n)
	}
}

// The build is the Host's. Quitting (Close) stops it; evicting one service
// (Shutdown) must not stop a build started for the whole app.
func TestShutdownLeavesTheHostBuildRunning(t *testing.T) {
	host := NewHost()
	svc := NewServiceWithHost(host, NewNilStore(), nil)
	var cancelled atomic.Bool
	host.buildCancel = func() { cancelled.Store(true) }

	if err := svc.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if cancelled.Load() {
		t.Fatal("Shutdown cancelled the Host's analyzer build")
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if !cancelled.Load() {
		t.Fatal("Close did not cancel the analyzer build")
	}
}

// Services on one Host see one toolchain list and one config directory, set
// through either of them.
func TestServicesOnOneHostShareItsState(t *testing.T) {
	host := NewHost()
	a := NewServiceWithHost(host, NewNilStore(), nil)
	b := NewServiceWithHost(host, NewNilStore(), nil)

	a.SetToolchains([]Toolchain{LuaToolchain()})
	b.SetConfigDir("/cfg")

	if got := b.toolchainList(); len(got) != 1 || got[0].Language != LuaToolchain().Language {
		t.Fatalf("b's toolchains = %v, want the one set through a", got)
	}
	if host.ConfigDir() != "/cfg" || a.Host() != b.Host() {
		t.Fatalf("config dir %q through a shared Host, want /cfg", host.ConfigDir())
	}
	// A Service built on its own gets a Host of its own — today's app.
	if NewService(NewNilStore(), nil).Host() == host {
		t.Fatal("NewService reused another Service's Host")
	}
}
