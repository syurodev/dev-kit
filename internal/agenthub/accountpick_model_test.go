package agenthub_test

import (
	"testing"

	"devkit/internal/agenthub"
)

// A model's own weekly window ("Weekly · Fable") only matters to a session on that
// model: it must not move a session that runs another one, nor keep an account out as
// a target for it.
func TestPickAccountReadsAModelWindowOnlyForThatModel(t *testing.T) {
	full := func(id string) agenthub.AccountRecord {
		return pickAccount(id, pickWindow("5-hour", 5), pickWindow("Weekly", 30), pickWindow("Weekly · Fable", 100))
	}
	spare := pickAccount("b", pickWindow("5-hour", 10), pickWindow("Weekly", 20), pickWindow("Weekly · Fable", 0))

	pick := func(model string, accounts ...agenthub.AccountRecord) agenthub.PickDecision {
		return agenthub.PickAccount(agenthub.PickInput{Policy: pickPolicy(), Accounts: accounts, Current: "a", Now: pickNow, Model: model})
	}

	if d := pick("claude-opus-5-5", full("a"), spare); d.Switch {
		t.Errorf("a full Fable window moved an Opus session: %+v", d)
	}
	if d := pick("", full("a"), spare); d.Switch {
		t.Errorf("a model window moved a session whose model is unknown: %+v", d)
	}
	d := pick("claude-fable-5-1", full("a"), spare)
	if !d.Switch || d.Target != "b" {
		t.Errorf("a Fable session on an account with a full Fable window must move to b: %+v", d)
	}
	// The target's own Fable window counts for a Fable session, and not for an Opus one.
	if d := pick("claude-fable-5-1", full("a"), full("b")); d.Switch {
		t.Errorf("moved a Fable session to an account whose Fable window is full: %+v", d)
	}
	blockedOpus := pickAccount("a", pickWindow("5-hour", 95), pickWindow("Weekly · Fable", 100))
	if d := pick("claude-opus-5-5", blockedOpus, full("b")); !d.Switch || d.Target != "b" {
		t.Errorf("a plain window over the threshold moves any model, to a target whose Fable window is irrelevant to it: %+v", d)
	}
	// A plain weekly window is not a model window: it blocks whatever the model is.
	weeklyFull := pickAccount("a", pickWindow("Weekly", 100))
	if d := pick("claude-opus-5-5", weeklyFull, spare); !d.Switch {
		t.Errorf("the plain weekly window stopped counting: %+v", d)
	}
}
