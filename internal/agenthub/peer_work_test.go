package agenthub_test

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

// rounds sends n messages alternately, first -> second -> first ..., each turn
// the receiver runs finishing before the next message. text is what every
// message says (the mock runs a command when it contains DO_WORK, calls only an
// agenthub tool for CHAT_TOOL). It returns the first refused result, or the
// last one.
func (r *peerRig) rounds(first, second agenthub.AgentSession, n int, text string) (agenthub.SendMessageResult, int) {
	r.t.Helper()
	from, to := first, second
	var last agenthub.SendMessageResult
	for i := 1; i <= n; i++ {
		last = r.send(from, to, text, "")
		if last.Delivery == agenthub.DeliveryRefused {
			return last, i
		}
		r.waitIdle(to)
		from, to = to, from
	}
	return last, n
}

// A review loop on a long task: the parent reviews, the child fixes, and so on.
// Both sides run commands, so none of it is two agents trading messages.
func TestARealReviewLoopIsNeverCutByTheExchangeCap(t *testing.T) {
	for name, pair := range map[string]func(r *peerRig) (agenthub.AgentSession, agenthub.AgentSession){
		"parent and child": func(r *peerRig) (agenthub.AgentSession, agenthub.AgentSession) { return r.a, r.child(r.a, "qa") },
		"siblings": func(r *peerRig) (agenthub.AgentSession, agenthub.AgentSession) {
			return r.child(r.a, "qa"), r.child(r.a, "frontend")
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newPeerRig(t)
			first, second := pair(r)
			last, n := r.rounds(first, second, 10, "review round DO_WORK")
			if last.Delivery == agenthub.DeliveryRefused || n != 10 {
				t.Fatalf("round %d refused: %+v", n, last)
			}
			if last.Hop != 1 {
				t.Fatalf("hop after work between = %d, want the count to have restarted", last.Hop)
			}
		})
	}
}

// The same ten rounds with nothing happening in between stop at the cap.
func TestTheSameLoopWithoutWorkStopsAtTheCap(t *testing.T) {
	for _, text := range []string{"ping", "ping CHAT_TOOL"} { // no tool at all / only an agenthub tool
		t.Run(text, func(t *testing.T) {
			r := newPeerRig(t)
			x := r.child(r.a, "qa")
			last, n := r.rounds(r.a, x, 10, text)
			if n != agenthub.MaxPeerHops+1 || last.Code != agenthub.PeerLoop || !strings.Contains(last.Reason, "restarts the count") {
				t.Fatalf("parent and child: stopped at round %d: %+v", n, last)
			}

			y, z := r.child(r.a, "frontend"), r.child(r.a, "infra")
			last, n = r.rounds(y, z, 10, text)
			if n != agenthub.MaxSiblingHops+1 || last.Code != agenthub.PeerLoop {
				t.Fatalf("siblings: stopped at round %d: %+v", n, last)
			}
		})
	}
}

// Work in one exchange does not hide chatter in the next, and a turn that only
// spoke gives nothing back: the count restarts only for the session that worked.
func TestOnlyATurnThatWorkedRestartsTheCount(t *testing.T) {
	r := newPeerRig(t)
	x := r.child(r.a, "qa")
	// Four chatty messages, then x works in its turn on the fifth.
	for i, text := range []string{"1", "2", "3", "4"} {
		from, to := r.a, x
		if i%2 == 1 {
			from, to = x, r.a
		}
		if res := r.send(from, to, text, ""); res.Delivery == agenthub.DeliveryRefused || res.Hop != i+1 {
			t.Fatalf("message %d: %+v", i+1, res)
		}
		r.waitIdle(to)
	}
	if res := r.send(r.a, x, "DO_WORK", ""); res.Hop != 5 {
		t.Fatalf("fifth message: %+v", res)
	}
	r.waitIdle(x)
	if res := r.send(x, r.a, "report", ""); res.Hop != 1 {
		t.Fatalf("after x ran a command, its answer = hop %d, want 1", res.Hop)
	}
}

// Whatever the work in between, two sessions do not trade more than 30
// messages an hour — except a child's reports to its parent.
func TestAPairHasAnHourlyBackstopThatNeverStopsAReportToTheParent(t *testing.T) {
	r := newPeerRig(t)
	var offset atomic.Int64
	base := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	r.svc.SetPeerClockForTest(func() time.Time { return base.Add(time.Duration(offset.Load())) })
	x := r.child(r.a, "qa")

	from, to := r.a, x
	for i := 1; i <= agenthub.PeerPairPerHour; i++ {
		res := r.send(from, to, "round DO_WORK", "")
		if res.Delivery == agenthub.DeliveryRefused {
			t.Fatalf("message %d of the hour refused: %+v", i, res)
		}
		r.waitIdle(to)
		from, to = to, from
		offset.Add(int64(time.Minute)) // clear of the per-minute limits
	}
	// from is the parent again: the 31st message.
	over := r.send(from, to, "round DO_WORK", "")
	if over.Delivery != agenthub.DeliveryRefused || over.Code != agenthub.PeerLoop || !strings.Contains(over.Reason, "last hour") {
		t.Fatalf("31st message: %+v", over)
	}
	// The child can still report.
	rep := r.send(x, r.a, "final report", "")
	if rep.Delivery == agenthub.DeliveryRefused {
		t.Fatalf("the child's report was refused by the backstop: %+v", rep)
	}
	r.waitIdle(r.a)
	// An hour after the first messages the pair may talk again.
	offset.Add(int64(agenthub.PeerPairHour))
	if again := r.send(r.a, x, "new task", ""); again.Delivery == agenthub.DeliveryRefused {
		t.Fatalf("after the hour: %+v", again)
	}
}
