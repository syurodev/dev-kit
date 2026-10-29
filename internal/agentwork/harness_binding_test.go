package agentwork

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func i64(v int64) *int64 { return &v }

func baseFacts() HarnessFacts {
	return HarnessFacts{
		SourceSchema: HarnessSourceSchemaSupported, Vendor: "claude", WorkspaceID: "ws-1",
		VendorRevision: 3, WorkspaceRevision: i64(2), Fingerprint: "fp-1", ExecutableDigest: "ex-1",
	}
}

func TestCompareFactsIgnoresSessionIDModelAndRequirementsDigest(t *testing.T) {
	// The facts type has no field for a model, effort, account, command,
	// path or the v2 requirements digest, so none can leak into a comparison.
	banned := []string{"model", "effort", "account", "requirements", "path", "command", "url", "env", "arg", "session"}
	typ := reflect.TypeOf(HarnessFacts{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(typ.Field(i).Name)
		for _, word := range banned {
			if strings.Contains(name, word) && name != "sessionrevision" {
				t.Fatalf("HarnessFacts.%s must not carry %q", typ.Field(i).Name, word)
			}
		}
	}
	a, b := baseFacts(), baseFacts()
	// The session revision is not part of the comparison: an actual child with
	// an override of its own is refused by a separate rule (harness_override_present).
	b.SessionRevision = i64(9)
	if !compareFacts(a, b) {
		t.Fatalf("facts that differ only in the session override must compare equal")
	}
}

func TestCompareFactsNilRevisionIsNotZero(t *testing.T) {
	a, b := baseFacts(), baseFacts()
	b.WorkspaceRevision = i64(0)
	if compareFacts(a, b) || compareFacts(b, a) {
		t.Fatalf("an absent workspace revision must differ from revision 0")
	}
	a.WorkspaceRevision = nil
	b.WorkspaceRevision = nil
	if !compareFacts(a, b) {
		t.Fatalf("two absent workspace revisions are equal")
	}
	a.WorkspaceRevision, b.WorkspaceRevision = i64(2), i64(2)
	if !compareFacts(a, b) {
		t.Fatalf("equal revisions behind different pointers are equal")
	}
}

func TestCompareFactsDetectsWorkspaceVendorFingerprintAndExecutableChange(t *testing.T) {
	mutations := map[string]func(*HarnessFacts){
		"vendor":        func(f *HarnessFacts) { f.Vendor = "codex" },
		"workspace":     func(f *HarnessFacts) { f.WorkspaceID = "ws-2" },
		"vendorRev":     func(f *HarnessFacts) { f.VendorRevision++ },
		"workspaceRev":  func(f *HarnessFacts) { f.WorkspaceRevision = i64(3) },
		"fingerprint":   func(f *HarnessFacts) { f.Fingerprint = "fp-2" },
		"executables":   func(f *HarnessFacts) { f.ExecutableDigest = "ex-2" },
		"factsComplete": func(f *HarnessFacts) { f.FactsComplete = true },
		"default":       func(f *HarnessFacts) { f.Default = true },
	}
	for name, mutate := range mutations {
		changed := baseFacts()
		mutate(&changed)
		if compareFacts(baseFacts(), changed) {
			t.Fatalf("%s change was not detected", name)
		}
	}
}

func refusalCode(t *testing.T, err error) string {
	t.Helper()
	var r *HarnessRefusal
	if !errors.As(err, &r) {
		t.Fatalf("want a HarnessRefusal, got %v", err)
	}
	return r.Code
}

func TestRefusalCheckOrderIsFixed(t *testing.T) {
	required := []string{"agenthub", "tasks"}
	intended := baseFacts()
	faulty := func() HarnessResolution {
		f := baseFacts()
		f.SourceSchema = HarnessSourceSchemaSupported + 1
		f.SessionRevision = i64(1)
		f.Fingerprint = "drifted"
		return HarnessResolution{Facts: f, Groups: []string{"tasks"}}
	}
	steps := []struct {
		want string
		fix  func(*HarnessResolution)
	}{
		{HarnessSchemaUnsupported, func(r *HarnessResolution) { r.Facts.SourceSchema = HarnessSourceSchemaSupported }},
		{HarnessOverridePresent, func(r *HarnessResolution) { r.Facts.SessionRevision = nil }},
		{HarnessRequiredGroupOff, func(r *HarnessResolution) { r.Groups = []string{"agenthub", "tasks"} }},
		{HarnessConfigChanged, func(r *HarnessResolution) { r.Facts.Fingerprint = intended.Fingerprint }},
	}
	res := faulty()
	for _, step := range steps {
		err := checkResolution(HarnessPhaseActual, res, required, &intended)
		if got := refusalCode(t, err); got != step.want {
			t.Fatalf("with several faults the first refusal must be %s, got %s", step.want, got)
		}
		step.fix(&res)
	}
	if err := checkResolution(HarnessPhaseActual, res, required, &intended); err != nil {
		t.Fatalf("all faults fixed: %v", err)
	}
}

func TestRequiredGroupRefusalNamesOnlyTheMissingRegistryGroups(t *testing.T) {
	res := HarnessResolution{Facts: baseFacts(), Groups: []string{"tasks"}}
	err := checkResolution(HarnessPhaseIntended, res, []string{"git", "tasks", "agenthub"}, nil)
	var r *HarnessRefusal
	if !errors.As(err, &r) || r.Code != HarnessRequiredGroupOff {
		t.Fatalf("want required-group refusal, got %v", err)
	}
	if !reflect.DeepEqual(r.Groups, []string{"agenthub", "git"}) {
		t.Fatalf("groups = %v", r.Groups)
	}
	if !ConfigurationNeeded(err) {
		t.Fatalf("a missing group is fixable by configuration")
	}
}

func TestIntendedResolutionWithAnOverrideIsRefused(t *testing.T) {
	// The intended configuration is W and V only: a resolver that reports an
	// S revision for the probe has read somebody's session.
	f := baseFacts()
	f.SessionRevision = i64(1)
	err := checkResolution(HarnessPhaseIntended, HarnessResolution{Facts: f, Groups: []string{"tasks"}}, nil, nil)
	if got := refusalCode(t, err); got != HarnessOverridePresent {
		t.Fatalf("code = %s", got)
	}
}

func TestConfigurationNeededOnlyForFixableCodes(t *testing.T) {
	fixable := []string{HarnessNotConfigured, HarnessOverridePresent, HarnessRequiredGroupOff, HarnessConfigChanged}
	for _, code := range fixable {
		if !ConfigurationNeeded(&HarnessRefusal{Code: code}) {
			t.Fatalf("%s is fixable by the person", code)
		}
	}
	notFixable := []string{HarnessUnavailable, HarnessSchemaUnsupported, HarnessEndpointNotLive, HarnessChildScope, HarnessChildMode, HarnessChildUnbound, HarnessBindingStale, HarnessBindingMissing}
	for _, code := range notFixable {
		if ConfigurationNeeded(&HarnessRefusal{Code: code}) {
			t.Fatalf("%s must not be reported as configuration-needed", code)
		}
	}
	if ConfigurationNeeded(nil) || ConfigurationNeeded(errors.New("x")) {
		t.Fatalf("only refusals are configuration-needed")
	}
}

func TestUnknownSourceSchemaFailsClosed(t *testing.T) {
	for _, schema := range []int{0, -1, HarnessSourceSchemaSupported + 1} {
		f := baseFacts()
		f.SourceSchema = schema
		err := checkResolution(HarnessPhaseIntended, HarnessResolution{Facts: f, Groups: []string{"tasks"}}, []string{"tasks"}, nil)
		if got := refusalCode(t, err); got != HarnessSchemaUnsupported {
			t.Fatalf("schema %d: code = %s", schema, got)
		}
	}
	intended := baseFacts()
	intended.SourceSchema = 99
	err := checkResolution(HarnessPhaseActual, HarnessResolution{Facts: baseFacts(), Groups: []string{"tasks"}}, nil, &intended)
	if got := refusalCode(t, err); got != HarnessSchemaUnsupported {
		t.Fatalf("an unreadable bound reference is also unsupported, got %s", got)
	}
}
