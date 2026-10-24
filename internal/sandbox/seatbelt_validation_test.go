package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeProfileRunner struct {
	err      error
	outcomes []error
	calls    int
	profile  string
}

func (r *fakeProfileRunner) Run(ctx context.Context, _ Policy, profile string) error {
	r.calls++
	r.profile = profile
	if _, ok := ctx.Deadline(); !ok {
		panic("profile check must have deadline")
	}
	if len(r.outcomes) >= r.calls {
		return r.outcomes[r.calls-1]
	}
	return r.err
}
func TestSeatbeltProfileValidationCachesBothOutcomes(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "supported", true: "rejected"}[failure], func(t *testing.T) {
			runner := &fakeProfileRunner{}
			if failure {
				runner.err = errors.New("profile rejected")
			}
			gate := unixProfileGate{runner: runner}
			p := Policy{WorkDir: "/work", SessionTempDir: "/private/tmp/dk-12345678", DenyReadPaths: []string{"/secret"}}
			if !strings.Contains(gate.status(), "not been checked") {
				t.Fatal(gate.status())
			}
			profile := gate.profile(p)
			p.SessionTempDir = "/private/tmp/dk-87654321"
			next := gate.profile(p)
			if runner.calls != 1 || !strings.Contains(runner.profile, "local unix-socket") {
				t.Fatal("must probe full policy exactly once")
			}
			for _, result := range []string{profile, next} {
				if strings.Contains(result, "local unix-socket") == failure {
					t.Fatal("wrong optional rule fallback", result)
				}
				if !strings.Contains(result, `(regex #"^/private/tmp/dk-")`) || !strings.Contains(result, `(subpath "/secret")`) {
					t.Fatal("fallback removed file protection")
				}
			}
			if failure && !strings.Contains(gate.status(), "unavailable") {
				t.Fatal(gate.status())
			}
		})
	}
}

func TestSeatbeltProfileValidationRetriesOnlyTimeoutOnce(t *testing.T) {
	for _, recover := range []bool{false, true} {
		t.Run(map[bool]string{false: "repeated-timeout", true: "transient-timeout"}[recover], func(t *testing.T) {
			runner := &fakeProfileRunner{outcomes: []error{context.DeadlineExceeded, context.DeadlineExceeded}}
			if recover {
				runner.outcomes[1] = nil
			}
			gate := unixProfileGate{runner: runner}
			p := Policy{WorkDir: "/work", SessionTempDir: "/private/tmp/dk-12345678"}
			result := gate.profile(p)
			gate.profile(p)
			if runner.calls != 2 {
				t.Fatalf("timeout must get exactly one retry, got %d", runner.calls)
			}
			if strings.Contains(result, "local unix-socket") != recover {
				t.Fatal("wrong result after timeout retry")
			}
		})
	}
}
