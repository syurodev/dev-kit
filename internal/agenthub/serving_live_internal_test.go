package agenthub

import (
	"errors"
	"testing"
)

// The reason a launch is stale decides whether a rewind that already restored
// files may prepare it again: only a moved epoch is S; a Close or a lost endpoint is not.
func TestLaunchStaleReasonsAreDistinguishedAndAllMatchTheOldError(t *testing.T) {
	launch := harnessLaunch{reservation: servingReservation{epoch: 1, set: true}}
	gone := harnessLaunch{reservation: launch.reservation, incarnation: "gone"}
	cases := []struct {
		name                  string
		epoch                 uint64
		closing, closePending bool
		l                     harnessLaunch
		want                  launchStaleReason
	}{
		{"current", 1, false, false, launch, launchCurrent},
		{"epoch moved", 2, false, false, launch, launchStaleEpoch},
		{"close in progress", 1, true, false, launch, launchStaleClose},
		{"failed close fenced", 1, false, true, launch, launchStaleClose},
		{"close wins over epoch", 2, true, false, launch, launchStaleClose},
		{"endpoint no longer ours", 1, false, false, gone, launchStaleEndpoint},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			coord := &servingCoord{epoch: c.epoch, closing: c.closing, closePending: c.closePending}
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
