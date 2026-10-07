package codegraph

import (
	"strings"
	"testing"
)

// configIndex is the shape the feedback described: a flag read from the
// environment through application.yaml, injected with @Value, read by a
// method that chooses between two callees, plus a bean that exists only when
// the flag is on, and a @ConfigurationProperties class read through a getter.
func configIndex() AnalysisResult {
	const (
		controller = "acme.AccessController"
		service    = "acme.AccessService"
		isAvail    = "acme.AccessService.isAvailable(java.lang.String)"
		byModule   = "acme.AccessService.byModule(java.lang.String)"
		byRole     = "acme.AccessService.byRole(java.lang.String)"
		get        = "acme.AccessController.get(java.lang.String)"
		config     = "acme.AccessConfig"
		bean       = "acme.AccessConfig.moduleBased()"
		props      = "acme.LimitProperties"
		getter     = "acme.LimitProperties.getMaxRequests()"
		limiter    = "acme.Limiter.check()"
		limited    = "acme.LimitController.list()"
	)
	cls := func(id string) Node {
		n := method(id)
		n.Kind = "class"
		return n
	}
	// A real method node's label is "<class>.<name>", with no parameter list.
	labelled := func(id, label string) Node {
		n := method(id)
		n.Label = label
		return n
	}
	return AnalysisResult{
		Nodes: []Node{
			cls(controller), cls(service), cls(config), cls(props), cls("acme.Limiter"), cls("acme.LimitController"),
			method(get), method(isAvail), method(byModule), method(byRole), method(bean), labelled(getter, "acme.LimitProperties.getMaxRequests"), method(limiter), method(limited),
		},
		Edges: []Edge{
			{From: get, To: isAvail, Kind: "call"},
			{From: isAvail, To: byModule, Kind: "call", Guard: "moduleBasedAccessEnabled"},
			{From: isAvail, To: byRole, Kind: "call", Guard: "!moduleBasedAccessEnabled"},
			{From: limiter, To: getter, Kind: "call"},
			{From: limited, To: limiter, Kind: "call"},
		},
		EntryPoints: []EntryPoint{
			{ID: "GET /v1/access/{org}", Verb: "GET", Target: "/v1/access/{org}", EntryNode: get},
			{ID: "GET /v1/limits", Verb: "GET", Target: "/v1/limits", EntryNode: limited},
		},
		FieldAccesses: []FieldAccess{
			{ClassID: service, NodeID: isAvail, FieldName: "moduleBasedAccessEnabled", Kind: "read", Line: 5},
		},
		ConfigKeys: []ConfigKey{
			{Key: "fsap.access.module-based-enabled", File: "src/main/resources/application.yaml", Line: 4,
				Value: "${FSAP_ACCESS_MODULE_BASED_ENABLED:false}", EnvVars: []string{"FSAP_ACCESS_MODULE_BASED_ENABLED"}},
			{Key: "fsap.access.module-based-enabled", File: "src/main/resources/application-prod.yaml", Line: 2, Value: "true", Profile: "prod"},
			{Key: "limits.max-requests", File: "src/main/resources/application.yaml", Line: 9, Value: "100"},
			{Key: "spring.datasource.password", File: "src/main/resources/application.yaml", Line: 12, Redacted: true},
		},
		ConfigUsages: []ConfigUsage{
			{Key: "fsap.access.module-based-enabled", Kind: ConfigUseValue, From: service, Field: "moduleBasedAccessEnabled",
				File: "AccessService.java", Line: 4, DefaultValue: "false"},
			{Key: "fsap.access.module-based-enabled", Kind: ConfigUseCondition, From: bean,
				File: "AccessConfig.java", Line: 8, Detail: "@ConditionalOnProperty havingValue=true"},
			{Key: "limits.max-requests", Kind: ConfigUseProperties, From: props, Field: "maxRequests", File: "LimitProperties.java", Line: 3},
			{Key: "FSAP_OTHER", Kind: ConfigUseEnvVar, From: isAvail, File: "AccessService.java", Line: 20},
			{Kind: ConfigUseProfile, From: config, File: "AccessConfig.java", Line: 5, Detail: "prod"},
			{Kind: ConfigUseBeanCondition, From: bean, File: "AccessConfig.java", Line: 9, Detail: "@ConditionalOnMissingBean"},
		},
	}
}

func TestFindConfigUsageFromTheEnvironmentVariable(t *testing.T) {
	svc := openWith(t, configIndex())
	// The feedback's question: turn this flag on — what changes?
	report, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "FSAP_ACCESS_MODULE_BASED_ENABLED"})
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Keys) != 2 {
		t.Fatalf("keys = %+v, want the yaml entry and the prod one", report.Keys)
	}
	if len(report.EnvVars) != 1 || report.EnvVars[0] != "FSAP_ACCESS_MODULE_BASED_ENABLED" {
		t.Fatalf("env vars = %v", report.EnvVars)
	}

	byKind := map[string]ConfigUsageDetail{}
	for _, u := range report.Usages {
		byKind[u.Kind] = u
	}
	value := byKind[ConfigUseValue]
	if value.Label != "acme.AccessService" || value.DefaultValue != "false" {
		t.Fatalf("value use = %+v", value)
	}
	if len(value.Readers) != 1 || value.Readers[0].NodeID != "acme.AccessService.isAvailable(java.lang.String)" || value.Readers[0].Access != "read" {
		t.Fatalf("readers = %+v", value.Readers)
	}
	if len(value.Guarded) != 2 {
		t.Fatalf("guarded calls = %+v, want both sides of the branch", value.Guarded)
	}
	gated := byKind[ConfigUseCondition]
	if len(gated.Gated) != 1 || gated.Gated[0].NodeID != "acme.AccessConfig.moduleBased()" {
		t.Fatalf("gated = %+v", gated.Gated)
	}

	if report.EntryPointTotal != 1 || report.EntryPoints[0].ID != "GET /v1/access/{org}" {
		t.Fatalf("entry points = %+v", report.EntryPoints)
	}
	if report.EntryPoints[0].Depth != 1 || !strings.Contains(report.EntryPoints[0].Via, "isAvailable") {
		t.Fatalf("the endpoint reaches the flag through isAvailable, one call down: %+v", report.EntryPoints[0])
	}
}

// Every spelling of one property is the same property.
func TestFindConfigUsageIgnoresCaseAndSeparators(t *testing.T) {
	svc := openWith(t, configIndex())
	var counts []int
	for _, key := range []string{"fsap.access.module-based-enabled", "fsap.access.moduleBasedEnabled", "FSAP_ACCESS_MODULE_BASED_ENABLED"} {
		report, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: key})
		if err != nil {
			t.Fatal(err)
		}
		counts = append(counts, len(report.Keys)*100+len(report.Usages)*10+len(report.EntryPoints))
	}
	if counts[0] != counts[1] || counts[1] != counts[2] || counts[0] == 0 {
		t.Fatalf("spellings disagree: %v", counts)
	}
}

func TestFindConfigUsageByPrefix(t *testing.T) {
	svc := openWith(t, configIndex())
	exact, _ := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "fsap.access"})
	if len(exact.Keys) != 0 || len(exact.Usages) != 0 {
		t.Fatalf("an exact match on a prefix finds nothing: %+v", exact)
	}
	report, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "fsap.access", Match: MatchPrefix})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Keys) != 2 || len(report.Usages) != 2 {
		t.Fatalf("prefix: keys %d, usages %d", len(report.Keys), len(report.Usages))
	}
}

// A @ConfigurationProperties field is read through its getter, and the
// getter's callers are what the setting changes.
func TestFindConfigUsageFollowsAnAccessorToTheEndpoint(t *testing.T) {
	svc := openWith(t, configIndex())
	report, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "limits.max-requests"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Usages) != 1 || len(report.Usages[0].Readers) != 1 || report.Usages[0].Readers[0].NodeID != "acme.LimitProperties.getMaxRequests()" {
		t.Fatalf("usages = %+v", report.Usages)
	}
	if report.EntryPointTotal != 1 || report.EntryPoints[0].ID != "GET /v1/limits" || report.EntryPoints[0].Depth != 2 {
		t.Fatalf("entry points = %+v", report.EntryPoints)
	}
}

func TestFindConfigUsageListsTheSwitchesThatNameNoKey(t *testing.T) {
	svc := openWith(t, configIndex())
	report, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, u := range report.Usages {
		kinds[u.Kind] = true
	}
	if len(report.Usages) != 2 || !kinds[ConfigUseProfile] || !kinds[ConfigUseBeanCondition] {
		t.Fatalf("switches = %+v", report.Usages)
	}
}

func TestFindConfigUsageForAnUnknownKeyIsEmptyAndSaysSo(t *testing.T) {
	svc := openWith(t, configIndex())
	report, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "no.such.key"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Keys) != 0 || len(report.Usages) != 0 || len(report.EntryPoints) != 0 || len(report.Notes) == 0 {
		t.Fatalf("report = %+v", report)
	}
	if report.Keys == nil || report.Usages == nil || report.EntryPoints == nil {
		t.Fatal("empty lists must be [] in JSON, not null")
	}
}

func TestFindConfigUsageKeepsASecretOutOfTheAnswer(t *testing.T) {
	svc := openWith(t, configIndex())
	report, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "spring.datasource.password"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Keys) != 1 || !report.Keys[0].Redacted || report.Keys[0].Value != "" {
		t.Fatalf("keys = %+v", report.Keys)
	}
}

func TestFindConfigUsageRejectsBadInput(t *testing.T) {
	svc := openWith(t, configIndex())
	if _, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "x", Match: "fuzzy"}); err == nil {
		t.Fatal("an unknown match mode must be refused")
	}
	if _, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "...", Limit: 1}); err == nil {
		t.Fatal("a key with no letters or digits must be refused")
	}
	if _, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "x", Limit: -1}); err == nil {
		t.Fatal("a negative limit must be refused")
	}
}

func TestFindConfigUsageCapsTheEntryPointsItLists(t *testing.T) {
	result := configIndex()
	for i := 0; i < 5; i++ {
		id := "acme.Extra" + string(rune('A'+i)) + ".go()"
		result.Nodes = append(result.Nodes, method(id))
		result.Edges = append(result.Edges, Edge{From: id, To: "acme.AccessService.isAvailable(java.lang.String)", Kind: "call"})
		result.EntryPoints = append(result.EntryPoints, EntryPoint{ID: "GET /e" + string(rune('a'+i)), Verb: "GET", Target: "/e" + string(rune('a'+i)), EntryNode: id})
	}
	svc := openWith(t, result)
	report, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "fsap.access.module-based-enabled", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.EntryPoints) != 2 || report.EntryPointTotal != 6 {
		t.Fatalf("listed %d of %d", len(report.EntryPoints), report.EntryPointTotal)
	}
}

func TestAnIndexWithAUseOfAnUnknownNodeIsRejected(t *testing.T) {
	result := AnalysisResult{
		Nodes:        []Node{method("a")},
		ConfigUsages: []ConfigUsage{{Key: "k", Kind: ConfigUseValue, From: "missing"}},
	}
	if err := result.Validate(); err == nil {
		t.Fatal("a config use pointing at a node the index lacks must fail validation")
	}
}

func TestCanonicalConfigKey(t *testing.T) {
	for in, want := range map[string]string{
		"FSAP_ACCESS_MODULE_BASED_ENABLED": "fsapaccessmodulebasedenabled",
		"fsap.access.module-based-enabled": "fsapaccessmodulebasedenabled",
		"fsap.access.moduleBasedEnabled":   "fsapaccessmodulebasedenabled",
		"a.b[0].c":                         "ab0c",
		"...":                              "",
	} {
		if got := CanonicalConfigKey(in); got != want {
			t.Errorf("CanonicalConfigKey(%q) = %q, want %q", in, got, want)
		}
	}
}
