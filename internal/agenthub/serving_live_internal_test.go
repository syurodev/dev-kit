package agenthub

import (
	"errors"
	"testing"
)

// The reason a launch is stale decides whether a rewind that already restored
// files may prepare it again: only a moved epoch is S; a Close or a lost endpoint is not.
func TestLaunchStaleReasonsAreDistinguishedAndAllMatchTheOldError(t *testing.T) {
	launch := harnessLaunch{reservation: servingReservation{epoch: 1, set: true}}
	cases := map[string]struct {
		coord servingCoord
		l     harnessLaunch
		want  launchStaleReason
	}{
		"current":                 {servingCoord{epoch: 1}, launch, launchCurrent},
		"epoch moved":             {servingCoord{epoch: 2}, launch, launchStaleEpoch},
		"close in progress":       {servingCoord{epoch: 1, closing: true}, launch, launchStaleClose},
		"failed close fenced":     {servingCoord{epoch: 1, closePending: true}, launch, launchStaleClose},
		"close wins over epoch":   {servingCoord{epoch: 2, closing: true}, launch, launchStaleClose},
		"endpoint no longer ours": {servingCoord{epoch: 1}, harnessLaunch{reservation: launch.reservation, incarnation: "gone"}, launchStaleEndpoint},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			coord := c.coord
			if got := launchStaleLocked(&coord, c.l); got != c.want {
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
