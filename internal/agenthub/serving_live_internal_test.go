package agenthub

import (
	"errors"
	"testing"
)

// The reason a launch is stale decides whether a rewind that already restored
// files may prepare it again: only a moved epoch with nothing else wrong is S; a Close
// (in progress, failed or finished) or a lost or replaced endpoint is not, even when S
// moved at the same time.
func TestLaunchStaleReasonsAreDistinguishedAndAllMatchTheOldError(t *testing.T) {
	launch := harnessLaunch{reservation: servingReservation{epoch: 1, set: true}}
	gone := harnessLaunch{reservation: launch.reservation, incarnation: "gone"}
	cases := []struct {
		name                  string
		epoch, lifecycle      uint64
		closing, closePending bool
		l                     harnessLaunch
		want                  launchStaleReason
	}{
		{"current", 1, 0, false, false, launch, launchCurrent},
		{"epoch moved", 2, 0, false, false, launch, launchStaleEpoch},
		{"close in progress", 1, 0, true, false, launch, launchStaleClose},
		{"failed close fenced", 1, 0, false, true, launch, launchStaleClose},
		{"close wins over epoch", 2, 0, true, false, launch, launchStaleClose},
		{"endpoint no longer ours", 1, 0, false, false, gone, launchStaleEndpoint},
		// A finished Close advances the epoch exactly as an S commit does; only the
		// lifecycle generation tells them apart.
		{"a finished close", 2, 1, false, false, launch, launchStaleLifecycle},
		{"a finished close wins over a lost endpoint", 2, 1, false, false, gone, launchStaleLifecycle},
		// S moved and the endpoint is lost: not S alone, so not retryable.
		{"a lost endpoint wins over S", 2, 0, false, false, gone, launchStaleEndpoint},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			coord := &servingCoord{epoch: c.epoch, lifecycle: c.lifecycle, closing: c.closing, closePending: c.closePending}
			if got := launchStaleLocked(coord, c.l); got != c.want {
				t.Fatalf("reason = %d, want %d", got, c.want)
			}
		})
	}
	var stale error = &launchStaleError{reason: launchStaleEpoch}
	if !errors.Is(stale, ErrHarnessLaunchStale) {
		t.Fatal("a typed stale launch must still be ErrHarnessLaunchStale")
	}
	var he interface{ Error() string } = stale
	if he.Error() != ErrHarnessLaunchStale.Error() {
		t.Fatal("the message must be the published one")
	}
}
