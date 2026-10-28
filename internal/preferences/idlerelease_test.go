package preferences_test

import (
	"errors"
	"testing"

	"devkit/internal/preferences"
)

func TestIdleReleaseIsOnWithDefaultsUntilThePersonChangesIt(t *testing.T) {
	svc := preferences.NewService(preferences.NewMemoryRepository())
	got, err := svc.AgentIdleRelease()
	if err != nil || got != preferences.DefaultAgentIdleRelease() || !got.Enabled || got.ChildMinutes != 10 || got.TopMinutes != 30 {
		t.Fatalf("an unset policy is the defaults, on, 10 and 30 minutes: %+v %v", got, err)
	}
}

func TestIdleReleaseSurvivesARestartAndCanBeTurnedOff(t *testing.T) {
	db := policyDB(t)
	svc := preferences.NewService(preferences.NewSQLRepository(db))
	want := preferences.AgentIdleRelease{Enabled: false, ChildMinutes: 5, TopMinutes: 60}
	if got, err := svc.SetAgentIdleRelease(want); err != nil || got != want {
		t.Fatalf("set: %+v %v", got, err)
	}
	again := preferences.NewService(preferences.NewSQLRepository(db))
	if got, err := again.AgentIdleRelease(); err != nil || got != want {
		t.Fatalf("the choice must persist: %+v %v", got, err)
	}
}

func TestIdleReleaseRefusesAThresholdOutsideTheRangeAndChangesNothing(t *testing.T) {
	svc := preferences.NewService(preferences.NewMemoryRepository())
	good := preferences.AgentIdleRelease{Enabled: true, ChildMinutes: 7, TopMinutes: 20}
	if _, err := svc.SetAgentIdleRelease(good); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]preferences.AgentIdleRelease{
		"child too short": {Enabled: true, ChildMinutes: 1, TopMinutes: 20},
		"top too long":    {Enabled: true, ChildMinutes: 7, TopMinutes: 1441},
		"zero":            {Enabled: true},
		"negative":        {Enabled: true, ChildMinutes: -5, TopMinutes: 20},
	} {
		if _, err := svc.SetAgentIdleRelease(bad); !errors.Is(err, preferences.ErrIdleReleaseInvalid) {
			t.Fatalf("%s: err = %v, want ErrIdleReleaseInvalid", name, err)
		}
	}
	if got, _ := svc.AgentIdleRelease(); got != good {
		t.Fatalf("a refused write changed the policy: %+v", got)
	}
}

func TestIdleReleaseReadsAnInvalidStoredValueAsTheDefaults(t *testing.T) {
	repo := preferences.NewMemoryRepository()
	if err := repo.SetAgentIdleRelease(preferences.AgentIdleRelease{Enabled: false, ChildMinutes: 0, TopMinutes: 99999}); err != nil {
		t.Fatal(err)
	}
	got, err := preferences.NewService(repo).AgentIdleRelease()
	if err != nil || got != preferences.DefaultAgentIdleRelease() {
		t.Fatalf("a stored value outside the range must not leave the policy broken: %+v %v", got, err)
	}
}
