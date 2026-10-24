package agentwork

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeOps struct {
	steps            []string
	task             Task
	checkErr         error
	assignErr        error
	branchErr        error
	sendErr          error
	closeErr         error
	branch           string
	brief            string
	closed           bool
	closeErrSeen     error
	closeHasDeadline bool
}

func (f *fakeOps) CheckTask(_ context.Context, id string) (Task, error) {
	f.steps = append(f.steps, "check:"+id)
	return f.task, f.checkErr
}
func (f *fakeOps) CreateChild(_ context.Context, _ Task) (Child, error) {
	f.steps = append(f.steps, "create")
	return Child{ID: "child", Value: "session", Branch: f.branch}, nil
}
func (f *fakeOps) AssignTask(_ context.Context, _ Task, _ Child) error {
	f.steps = append(f.steps, "assign")
	return f.assignErr
}
func (f *fakeOps) RecordBranch(_ context.Context, _ Task, _ Child, branch string) error {
	f.steps = append(f.steps, "branch:"+branch)
	return f.branchErr
}
func (f *fakeOps) SendBrief(_ context.Context, _ Task, _ Child, brief string) (Delivery, error) {
	f.steps = append(f.steps, "send")
	f.brief = brief
	return Delivery{State: "started", MessageID: "m1"}, f.sendErr
}
func (f *fakeOps) CloseChild(ctx context.Context, _ Child) error {
	f.steps = append(f.steps, "close")
	f.closed = true
	f.closeErrSeen = ctx.Err()
	_, f.closeHasDeadline = ctx.Deadline()
	return f.closeErr
}
func (*fakeOps) Logf(string, ...any) {}

func TestHandoffOrdersCheckCreateAssignBranchAndSend(t *testing.T) {
	ops := &fakeOps{task: Task{ID: "t1", Key: "WEB-1", Title: " Fix\nlogin ", Description: "details"}, branch: "feature/task"}
	got, err := Handoff(context.Background(), "t1", ops)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"check:t1", "create", "assign", "branch:feature/task", "send"}
	if !reflect.DeepEqual(ops.steps, want) {
		t.Fatalf("steps = %v, want %v", ops.steps, want)
	}
	if got.Child != "session" || got.Task.ID != "t1" || got.Delivery.State != "started" || !strings.Contains(ops.brief, "WEB-1 — Fix login") {
		t.Fatalf("handoff result/brief = %+v / %q", got, ops.brief)
	}
}

func TestHandoffDoesNotCreateWhenCheckFails(t *testing.T) {
	ops := &fakeOps{checkErr: errors.New("not assignable")}
	if _, err := Handoff(context.Background(), "t1", ops); err == nil {
		t.Fatal("expected check error")
	}
	if !reflect.DeepEqual(ops.steps, []string{"check:t1"}) {
		t.Fatalf("steps = %v", ops.steps)
	}
}

func TestHandoffClosesChildAfterAssignmentOrBranchFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		ops  *fakeOps
		want []string
	}{{"assignment", &fakeOps{assignErr: errors.New("lost claim")}, []string{"check:t1", "create", "assign", "close"}}, {"branch", &fakeOps{branch: "b", branchErr: errors.New("branch write")}, []string{"check:t1", "create", "assign", "branch:b", "close"}}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Handoff(context.Background(), "t1", tc.ops)
			if err == nil || !tc.ops.closed || !reflect.DeepEqual(tc.ops.steps, tc.want) {
				t.Fatalf("err=%v closed=%v steps=%v want=%v", err, tc.ops.closed, tc.ops.steps, tc.want)
			}
		})
	}
}

func TestHandoffKeepsClaimAndReturnsRefusalWhenBriefCannotBeSent(t *testing.T) {
	ops := &fakeOps{sendErr: errors.New("peer unavailable")}
	got, err := Handoff(context.Background(), "t1", ops)
	if err != nil || ops.closed || got.Delivery.State != "refused" || got.Delivery.Code != "not_sent" || got.Child != "session" {
		t.Fatalf("result=%+v err=%v closed=%v", got, err, ops.closed)
	}
}

func TestBriefBoundsDescriptionByRunes(t *testing.T) {
	brief := Brief(Task{ID: "t1", Description: strings.Repeat("é", BriefRunes+2)})
	if !strings.Contains(brief, "cut at") || !strings.Contains(brief, "the rest is on the task") {
		t.Fatalf("brief missing truncation note: %q", brief[len(brief)-100:])
	}
}

func TestHandoffRollbackSurvivesCanceledRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ops := &fakeOps{assignErr: context.Canceled}
	cancel()
	_, err := Handoff(ctx, "t1", ops)
	if !errors.Is(err, context.Canceled) || !ops.closed {
		t.Fatalf("err=%v closed=%v; want canceled handoff and child rollback", err, ops.closed)
	}
	if ops.closeErrSeen != nil || !ops.closeHasDeadline {
		t.Fatalf("close context err=%v deadline=%v; want live bounded rollback context", ops.closeErrSeen, ops.closeHasDeadline)
	}
}
