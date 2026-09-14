package agenthub

import (
	"context"
	"errors"
	"testing"
)

// resumableSessionStore hands back a session the vendor can resume.
type resumableSessionStore struct{ Store }

func (resumableSessionStore) GetSession(_ context.Context, id string) (AgentSession, error) {
	return AgentSession{ID: id, AgentType: AgentTypeMock, ACPSessionID: "acp-" + id, Status: SessionStatusIdle}, nil
}

// secondLoadMock fails any Load: a caller that finds a resume in flight must
// take that one's result, not start another.
type secondLoadMock struct{ *MockAdapter }

func (secondLoadMock) Load(context.Context, OpenSpec, string) (SessionRuntime, error) {
	return nil, errors.New("resumed again instead of waiting on the resume in flight")
}

// A settings call waiting on Invoke's resume of a dead session was told
// "unavailable" whatever the resume actually hit — auth required, not
// installed — so the composer showed the wrong recovery.
func TestAResumeWaiterGetsTheResumesOwnError(t *testing.T) {
	reg := NewRegistry()
	reg.Register(secondLoadMock{NewMockAdapter()})
	svc := NewService(reg, resumableSessionStore{}, nil)

	// A resume in flight, finished the way the first caller's did. The entry
	// stays in the map, so the waiter finds it whenever it gets there.
	call := &resumeCall{done: make(chan struct{})}
	svc.resuming["s1"] = call

	result := make(chan error, 1)
	go func() { result <- svc.SetMode(context.Background(), "s1", "plan") }()
	call.err = ErrAgentAuthRequired
	close(call.done)
	if err := <-result; !errors.Is(err, ErrAgentAuthRequired) {
		t.Fatalf("waiter err = %v, want the resume's ErrAgentAuthRequired", err)
	}
}
