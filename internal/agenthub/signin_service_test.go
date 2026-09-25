package agenthub

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/identity"
)

// fakeInAppSignIn is a vendor that signs in inside DevKit and records what
// it was asked, so a test can tell a refused call from one that ran.
type fakeInAppSignIn struct {
	*MockAdapter
	calls []string
}

func (f *fakeInAppSignIn) Type() AgentType { return AgentTypeCodex }

func (f *fakeInAppSignIn) SignInStatus(context.Context) (SignInStatus, error) {
	f.calls = append(f.calls, "status")
	return SignInStatus{SignedIn: true, Method: "chatgpt"}, nil
}

func (f *fakeInAppSignIn) StartBrowserSignIn(context.Context) (SignInFlow, error) {
	f.calls = append(f.calls, "browser")
	return SignInFlow{LoginID: "l1", Method: SignInBrowser, AuthURL: "https://auth.example/x"}, nil
}

func (f *fakeInAppSignIn) StartDeviceSignIn(context.Context) (SignInFlow, error) {
	f.calls = append(f.calls, "device")
	return SignInFlow{LoginID: "l2", Method: SignInDeviceCode, UserCode: "ABCD-1234"}, nil
}

func (f *fakeInAppSignIn) CancelSignIn(_ context.Context, loginID string) error {
	f.calls = append(f.calls, "cancel:"+loginID)
	return nil
}

func (f *fakeInAppSignIn) SignOut(context.Context) error {
	f.calls = append(f.calls, "signout")
	return nil
}

// Signing in is the person's: an agent, or a CLI on the shared MCP socket,
// is refused before the vendor hears of it — status included, so an agent
// cannot even learn which account the home holds.
func TestServiceSignInIsForThePersonOnly(t *testing.T) {
	calls := func(svc *Service, ctx context.Context) []error {
		_, statusErr := svc.SignInStatus(ctx, AgentTypeCodex)
		_, browserErr := svc.StartSignIn(ctx, AgentTypeCodex, SignInBrowser)
		_, deviceErr := svc.StartSignIn(ctx, AgentTypeCodex, SignInDeviceCode)
		return []error{
			statusErr, browserErr, deviceErr,
			svc.CancelSignIn(ctx, AgentTypeCodex, "l1"),
			svc.SignOut(ctx, AgentTypeCodex),
		}
	}
	for name, ctx := range map[string]context.Context{
		"agent":         identity.WithCaller(context.Background(), identity.Agent("11111111-1111-1111-1111-111111111111")),
		"user terminal": identity.WithCaller(context.Background(), identity.UserTerminal()),
	} {
		vendor := &fakeInAppSignIn{MockAdapter: NewMockAdapter()}
		reg := NewRegistry()
		reg.Register(vendor)
		for i, err := range calls(&Service{reg: reg}, ctx) {
			if !errors.Is(err, ErrPermissionDenied) {
				t.Errorf("%s: call %d = %v, want ErrPermissionDenied", name, i, err)
			}
		}
		if len(vendor.calls) != 0 {
			t.Errorf("%s: the vendor was reached: %v", name, vendor.calls)
		}
	}
	for name, ctx := range map[string]context.Context{
		"ui":         identity.WithCaller(context.Background(), identity.UI()),
		"in process": context.Background(),
	} {
		vendor := &fakeInAppSignIn{MockAdapter: NewMockAdapter()}
		reg := NewRegistry()
		reg.Register(vendor)
		for i, err := range calls(&Service{reg: reg}, ctx) {
			if err != nil {
				t.Errorf("%s: call %d = %v", name, i, err)
			}
		}
		want := []string{"status", "browser", "device", "cancel:l1", "signout"}
		if len(vendor.calls) != len(want) {
			t.Fatalf("%s: calls = %v, want %v", name, vendor.calls, want)
		}
		for i := range want {
			if vendor.calls[i] != want[i] {
				t.Errorf("%s: calls = %v, want %v", name, vendor.calls, want)
				break
			}
		}
	}
}
