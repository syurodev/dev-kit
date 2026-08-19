package app

import "testing"

func TestContainerAuthAndSyncBlockedAccessorsAreRaceFree(t *testing.T) {
	container := NewContainer()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			container.SetAuth(nil)
			container.SetSyncBlockedErr(nil)
			_ = container.Auth()
			_ = container.SyncBlockedErr()
		}
	}()
	for i := 0; i < 200; i++ {
		_ = container.Auth()
		_ = container.SyncBlockedErr()
		container.SetSyncBlockedErr(nil)
	}
	<-done
}
