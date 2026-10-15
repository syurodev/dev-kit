package codegraph

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The shape of fsap-event-router's process(): an abstract decide with two
// overrides, a quarantine that writes a header, helpers that only count.
// A call sits at its name's column, as the Java analyzer records it, so the
// chained increment() comes after counter(...) on line 8.
func stepsFixture() AnalysisResult {
	nodes := []Node{method("process"), method("decide"), method("implA"), method("implB"), method("deliver"),
		method("kafkaDeliver"), method("quarantine"), method("counter"), method("metricPrefix")}
	return AnalysisResult{
		Nodes: nodes,
		CallSites: []CallSite{
			{From: "process", To: "decide", Line: 3, Col: 9, Text: "decide(exchange)", CalleeKey: "acme#decide", ResultUse: "assigned",
				Frames: []StepFrame{{ID: "2:5", Kind: "try", Text: "try"}}},
			{From: "process", To: "quarantine", Line: 5, Col: 9, Text: "quarantine(exchange, R)", CalleeKey: "acme#quarantine", ResultUse: "discarded",
				Frames: []StepFrame{{ID: "4:7", Kind: "catch", Text: "catch AclLookupException", Exits: true}}},
			{From: "process", To: "counter", Line: 8, Col: 9, Text: "counter(metricPrefix())", CalleeKey: "acme#counter", ResultUse: "argument"},
			{From: "process", To: "metricPrefix", Line: 8, Col: 17, Text: "metricPrefix()", CalleeKey: "acme#metricPrefix", ResultUse: "argument"},
			{From: "process", Line: 8, Col: 33, Unresolved: "library", Text: "counter(metricPrefix()).increment()", CalleeKey: "Counter#increment",
				Receiver: "local", ResultUse: "discarded"},
			{From: "process", Line: 11, Col: 9, Unresolved: "library", Text: "breaker.executeCallable(() -> …)",
				CalleeKey: "io.github.resilience4j.circuitbreaker.CircuitBreaker#executeCallable", Receiver: "local", ResultUse: "discarded"},
			{From: "process", To: "deliver", Line: 12, Col: 13, Text: "deliver(proceed, exchange)", CalleeKey: "acme#deliver", ResultUse: "discarded",
				Frames: []StepFrame{{ID: "11:9", Kind: "closure", Text: "breaker.executeCallable(() -> …)"}}},
			{From: "decide", To: "implA", Line: 1, Role: RoleOverride},
			{From: "decide", To: "implB", Line: 1, Role: RoleOverride},
			{From: "deliver", To: "kafkaDeliver", Line: 1, Role: RoleOverride},
			{From: "kafkaDeliver", Line: 2, Unresolved: "library", Text: "producerTemplate.send(uri, exchange)",
				CalleeKey: "org.apache.camel.ProducerTemplate#send", Receiver: "field", ResultUse: "discarded"},
			{From: "quarantine", Line: 2, Unresolved: "library", Text: "in.setHeader(H, r)", CalleeKey: "org.apache.camel.Message#setHeader",
				Receiver: "local-from-param", ResultUse: "discarded"},
			{From: "quarantine", Line: 3, Unresolved: "lombok", Text: "log.warn(m)", CalleeKey: "org.slf4j.Logger#warn", Receiver: "unknown", ResultUse: "discarded"},
			{From: "counter", Line: 2, Unresolved: "library", Text: "Counter.builder(n)", CalleeKey: "Counter#builder", Receiver: "static", ResultUse: "returned"},
		},
		KafkaAccesses: []KafkaAccess{},
	}
}

func stepByText(t *testing.T, steps []Step, text string) Step {
	t.Helper()
	for _, s := range steps {
		if strings.HasPrefix(s.Text, text) {
			return s
		}
	}
	t.Fatalf("no step %q in %+v", text, steps)
	return Step{}
}

func TestGetStepsOfAFlow(t *testing.T) {
	svc := openWith(t, stepsFixture())
	got, err := svc.GetSteps(t.Context(), []string{"process", "quarantine", "decide"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	process := got.Steps["process"]
	if len(process) != 7 {
		t.Fatalf("process steps = %+v", process)
	}
	if s := stepByText(t, process, "decide("); !s.Visible || strings.Join(s.Impls, ",") != "implA,implB" {
		t.Fatalf("decide = %+v", s)
	}
	if s := stepByText(t, process, "quarantine("); !s.Visible || s.Frames[0].Kind != "catch" {
		t.Fatalf("quarantine = %+v", s)
	}
	for _, helper := range []string{"counter(metricPrefix())", "metricPrefix()"} {
		if s := stepByText(t, process, helper); s.Visible || s.Reason != "helper" {
			t.Fatalf("%s = %+v, want a hidden helper", helper, s)
		}
	}
	if s := stepByText(t, process, "counter(metricPrefix()).increment()"); s.Visible || s.Reason != "metrics" {
		t.Fatalf("increment = %+v", s)
	}
	if s := stepByText(t, process, "breaker.executeCallable"); !s.Visible || s.Reason != "wraps a project call" {
		t.Fatalf("executeCallable = %+v", s)
	}
	if s := stepByText(t, got.Steps["quarantine"], "in.setHeader"); !s.Visible || s.Reason != "writes to in" {
		t.Fatalf("setHeader = %+v", s)
	}
	if s := stepByText(t, got.Steps["quarantine"], "log.warn"); s.Visible {
		t.Fatalf("log.warn = %+v", s)
	}
	if len(got.Steps["decide"]) != 0 {
		t.Fatalf("an abstract method has no steps of its own, got %+v", got.Steps["decide"])
	}
	if got.Legacy {
		t.Fatal("an index with step text is not legacy")
	}
}

func TestGetStepsHonoursOverrides(t *testing.T) {
	svc := openWith(t, stepsFixture())
	got, err := svc.GetSteps(t.Context(), []string{"quarantine"}, map[string]string{"org.slf4j.Logger#warn": "show"})
	if err != nil {
		t.Fatal(err)
	}
	if s := stepByText(t, got.Steps["quarantine"], "log.warn"); !s.Visible || s.Reason != "you chose" {
		t.Fatalf("log.warn = %+v", s)
	}
}

func TestGetStepsAttachesEffects(t *testing.T) {
	result := stepsFixture()
	result.KafkaAccesses = []KafkaAccess{{From: "kafkaDeliver", Topic: "out", Role: KafkaProduce, Client: KafkaTemplate, File: "k.java", Line: 2}}
	svc := openWith(t, result)
	got, err := svc.GetSteps(t.Context(), []string{"kafkaDeliver"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := got.Steps["kafkaDeliver"][0]
	if s.Effect == nil || *s.Effect != (StepEffect{Kind: "kafka", Role: "produce", Target: "out"}) || s.Reason != "Kafka produce" {
		t.Fatalf("step = %+v", s)
	}
}

func TestGetStepsCapsAndRefuses(t *testing.T) {
	sites := make([]CallSite, 0, maxStepsPerMethod+5)
	for i := range maxStepsPerMethod + 5 {
		sites = append(sites, CallSite{From: "big", Line: i + 1, Unresolved: "library", Text: "x()", CalleeKey: "acme.Lib#x"})
	}
	svc := openWith(t, AnalysisResult{Nodes: []Node{method("big")}, CallSites: sites})
	got, err := svc.GetSteps(t.Context(), []string{"big"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Steps["big"]) != maxStepsPerMethod || len(got.Truncated) != 1 {
		t.Fatalf("steps = %d, truncated = %v", len(got.Steps["big"]), got.Truncated)
	}
	ids := make([]string, MaxStepNodeIDs+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("m%d", i)
	}
	if _, err := svc.GetSteps(t.Context(), ids, nil); !errors.Is(err, ErrTooManyNodes) {
		t.Fatalf("err = %v, want ErrTooManyNodes", err)
	}
	// The cap counts methods, not how often one was named.
	same := make([]string, 2*MaxStepNodeIDs)
	for i := range same {
		same[i] = "big"
	}
	if got, err := svc.GetSteps(t.Context(), same, nil); err != nil || len(got.Truncated) != 1 {
		t.Fatalf("repeated id: truncated = %v, err = %v", got.Truncated, err)
	}
}

// openAs opens result as a project detected as language, the way OpenProject
// detects a real folder: by a marker file the toolchain claims.
func openAs(t *testing.T, language string, result AnalysisResult) *Service {
	t.Helper()
	svc := newTestService(t, fakeAnalyzer{result: result})
	svc.SetToolchains([]Toolchain{{Language: language, Markers: []string{language + ".build"}, SourceExt: "." + language}})
	root := t.TempDir()
	write(t, root, language+".build", "")
	if err := svc.OpenProject(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	return svc
}

func legacyResult() AnalysisResult {
	return AnalysisResult{Nodes: []Node{method("a"), method("b")},
		CallSites: []CallSite{{From: "a", To: "b", Line: 2}, {From: "a", Line: 3, Unresolved: "library"}}}
}

func TestGetStepsOfALegacyIndex(t *testing.T) {
	svc := openAs(t, "java", legacyResult())
	got, err := svc.GetSteps(t.Context(), []string{"a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Legacy || len(got.Steps["a"]) != 2 {
		t.Fatalf("got = %+v", got)
	}
}

// Only the Java analyzer records steps; a Lua or TypeScript index with none
// is what that analyzer writes, and re-indexing would not change it.
func TestGetStepsOfAnIndexThatNeverHadSteps(t *testing.T) {
	svc := openAs(t, "lua", legacyResult())
	got, err := svc.GetSteps(t.Context(), []string{"a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Legacy || len(got.Steps["a"]) != 2 {
		t.Fatalf("got = %+v", got)
	}
}

// A repository, Feign client or HTTP interface method is the project's own,
// with no body to follow: the step is shown as a project call, with what the
// extractors read off its declaration.
func TestGetStepsShowsACallIntoABodylessProjectMethod(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{method("svc"), method("findByEmail"), method("fetchInvoice"), method("count")},
		CallSites: []CallSite{
			{From: "svc", Line: 3, Col: 21, Unresolved: "interface", Target: "findByEmail", Text: "userRepo.findByEmail(e)",
				CalleeKey: "acme.UserRepo#findByEmail", Receiver: "field", ResultUse: "assigned"},
			{From: "svc", Line: 4, Col: 17, Unresolved: "interface", Target: "fetchInvoice", Text: "billing.fetchInvoice(id)",
				CalleeKey: "acme.Billing#fetchInvoice", Receiver: "field", ResultUse: "assigned"},
			{From: "svc", Line: 5, Col: 15, Unresolved: "interface", Target: "count", Text: "counts.count()",
				CalleeKey: "acme.Counts#count", Receiver: "field", ResultUse: "returned"},
		},
		TableAccesses: []TableAccess{
			{From: "findByEmail", Table: "users", Access: "read", Via: "repository-derived", File: "findByEmail.java", Line: 1},
			{From: "findByEmail", Table: "audit", Access: "write", Via: "repository-derived", File: "findByEmail.java", Line: 1},
		},
		OutboundCalls: []OutboundCall{{From: "fetchInvoice", File: "fetchInvoice.java", Line: 1, Client: "feign", HTTPMethod: "GET", URL: "/invoices/{id}"}},
	}
	got, err := openWith(t, result).GetSteps(t.Context(), []string{"svc"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	steps := got.Steps["svc"]
	if s := stepByText(t, steps, "userRepo.findByEmail"); !s.Visible || s.Reason != "DB write" || s.Target != "findByEmail" ||
		s.Effect == nil || *s.Effect != (StepEffect{Kind: "db", Role: "write", Target: "audit"}) {
		t.Fatalf("findByEmail = %+v (effect %v)", s, s.Effect)
	}
	if s := stepByText(t, steps, "billing.fetchInvoice"); !s.Visible || s.Reason != "HTTP call" || s.Effect == nil || *s.Effect != (StepEffect{Kind: "http", Role: "call", Target: "GET /invoices/{id}"}) {
		t.Fatalf("fetchInvoice = %+v (effect %v)", s, s.Effect)
	}
	if s := stepByText(t, steps, "counts.count"); !s.Visible || s.Reason != "project" || s.Effect != nil {
		t.Fatalf("count = %+v", s)
	}
}

// A project method whose body shows nothing still matters when an extractor
// saw it touch Kafka, HTTP, Redis or a table — even off any call site.
func TestGetStepsKeepsAMethodWithAnEffect(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{method("root"), method("listener")},
		CallSites: []CallSite{
			{From: "root", To: "listener", Target: "listener", Line: 2, Col: 9, Text: "listener(m)", CalleeKey: "acme#listener", ResultUse: "discarded"},
			{From: "listener", Line: 5, Col: 9, Unresolved: "library", Text: "log.info(m)", CalleeKey: "org.slf4j.Logger#info", Receiver: "field", ResultUse: "discarded"},
		},
		KafkaAccesses: []KafkaAccess{{From: "listener", Topic: "orders", Role: "consume", Client: "kafka-listener", File: "l.java", Line: 1}},
	}
	got, err := openWith(t, result).GetSteps(t.Context(), []string{"root"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := got.Steps["root"][0]; !s.Visible || s.Reason != "project" {
		t.Fatalf("listener = %+v, want it shown", s)
	}
}

// What an extractor saw goes to the step at its exact position, never to
// another call on the same line.
func TestGetStepsJoinsEffectsByPosition(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{method("m"), method("lua")},
		CallSites: []CallSite{
			// kafka\n  .send(t, x): the effect is on the line of send's name.
			{From: "m", Line: 4, Col: 5, Unresolved: "library", Text: "audit.mark()", CalleeKey: "acme.Lib#mark", Receiver: "local", ResultUse: "argument"},
			{From: "m", Line: 4, Col: 14, Unresolved: "library", Text: "kafka.send(t, x)", CalleeKey: "org.springframework.kafka.core.KafkaTemplate#send",
				Receiver: "field", ResultUse: "argument"},
			// redisTemplate.opsForValue().set(k, v): only set touches Redis.
			{From: "m", Line: 6, Col: 23, Unresolved: "library", Text: "redisTemplate.opsForValue()",
				CalleeKey: "org.springframework.data.redis.core.RedisTemplate#opsForValue", Receiver: "field", ResultUse: "unknown"},
			{From: "m", Line: 6, Col: 37, Unresolved: "library", Text: "redisTemplate.opsForValue().set(k, v)",
				CalleeKey: "org.springframework.data.redis.core.ValueOperations#set", Receiver: "unknown", ResultUse: "argument"},
			// log.info("x", ops.get(k)): the read is the argument's, not the log's.
			{From: "m", Line: 8, Col: 13, Unresolved: "library", Text: `log.info("x", ops.get(k))`, CalleeKey: "org.slf4j.Logger#info",
				Receiver: "field", ResultUse: "discarded"},
			{From: "m", Line: 8, Col: 27, Unresolved: "library", Text: "ops.get(k)", CalleeKey: "org.springframework.data.redis.core.ValueOperations#get",
				Receiver: "local", ResultUse: "argument"},
			// A row read off a declaration (col 0) in a method whose calls have
			// columns is at no call: @KafkaListener on a one-line method.
			{From: "m", Line: 12, Col: 45, Unresolved: "library", Text: "System.out.println(m)", CalleeKey: "java.io.PrintStream#println",
				Receiver: "static", ResultUse: "discarded"},
			// No column anywhere in the method (Lua, an older Java index): the
			// first library call on the line.
			{From: "lua", Line: 10, Unresolved: "library", Text: "red:set(k, v)", CalleeKey: "resty.redis#set", ResultUse: "argument"},
			{From: "lua", Line: 10, Unresolved: "library", Text: "red:expire(k, 9)", CalleeKey: "resty.redis#expire", ResultUse: "argument"},
		},
		KafkaAccesses: []KafkaAccess{
			{From: "m", Topic: "orders", Role: "produce", Client: "kafka-template", File: "m.java", Line: 4, Col: 14},
			{From: "m", Topic: "orders", Role: "consume", Client: "kafka-listener", File: "m.java", Line: 12},
		},
		RedisAccesses: []RedisAccess{
			{From: "m", Key: "k:{id}", Access: "read", Command: "get", File: "m.java", Line: 6, Col: 37},
			{From: "m", Key: "k:{id}", Access: "write", Command: "set", File: "m.java", Line: 6, Col: 37},
			{From: "m", Key: "q:{id}", Access: "read", Command: "get", File: "m.java", Line: 8, Col: 27},
			{From: "lua", Key: "r:{id}", Access: "write", Command: "set", File: "m.lua", Line: 10},
		},
	}
	got, err := openWith(t, result).GetSteps(t.Context(), []string{"m", "lua"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	steps := append(got.Steps["m"], got.Steps["lua"]...)
	effect := func(text string) string {
		t.Helper()
		for _, s := range steps {
			if s.Text == text {
				if s.Effect == nil {
					return ""
				}
				return s.Effect.Kind + " " + s.Effect.Role + " " + s.Effect.Target
			}
		}
		t.Fatalf("no step %q", text)
		return ""
	}
	for text, want := range map[string]string{
		"audit.mark()":                          "",
		"kafka.send(t, x)":                      "kafka produce orders",
		"redisTemplate.opsForValue()":           "",
		"redisTemplate.opsForValue().set(k, v)": "redis write k:{id}", // a write is preferred over a read at one step
		`log.info("x", ops.get(k))`:             "",
		"ops.get(k)":                            "redis read q:{id}",
		"System.out.println(m)":                 "",
		"red:set(k, v)":                         "redis write r:{id}",
		"red:expire(k, 9)":                      "",
	} {
		if got := effect(text); got != want {
			t.Errorf("%s: effect = %q, want %q", text, got, want)
		}
	}
	if s := stepByText(t, steps, "log.info"); s.Visible || s.Reason != "logging" {
		t.Errorf("log.info = %+v, want hidden as logging", s)
	}
}

// Whether a callee is a helper does not depend on which other methods were
// asked for in the same call: R1 reaches X three calls down, where its own
// callee is past the depth limit; R2 calls it directly.
func TestGetStepsJudgesAHelperTheSameWhateverElseIsAsked(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{method("r1"), method("a"), method("b"), method("r2"), method("x"), method("y")},
		CallSites: []CallSite{
			{From: "r1", To: "a", Line: 2, Col: 9, Text: "a()", CalleeKey: "acme#a", ResultUse: "discarded"},
			{From: "a", To: "b", Line: 2, Col: 9, Text: "b()", CalleeKey: "acme#b", ResultUse: "discarded"},
			{From: "b", To: "x", Line: 2, Col: 9, Text: "x()", CalleeKey: "acme#x", ResultUse: "discarded"},
			{From: "r2", To: "x", Line: 2, Col: 9, Text: "x()", CalleeKey: "acme#x", ResultUse: "discarded"},
			{From: "x", To: "y", Line: 2, Col: 9, Text: "y()", CalleeKey: "acme#y", ResultUse: "discarded"},
			{From: "y", Line: 2, Col: 13, Unresolved: "library", Text: "log.warn(m)", CalleeKey: "org.slf4j.Logger#warn", Receiver: "field", ResultUse: "discarded"},
		},
	}
	svc := openWith(t, result)
	alone, err := svc.GetSteps(t.Context(), []string{"r2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	both, err := svc.GetSteps(t.Context(), []string{"r1", "r2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b := alone.Steps["r2"][0], both.Steps["r2"][0]
	if a.Visible != b.Visible || a.Reason != b.Reason {
		t.Fatalf("x() alone = (%v, %s), with r1 = (%v, %s)", a.Visible, a.Reason, b.Visible, b.Reason)
	}
	if a.Visible || a.Reason != "helper" {
		t.Fatalf("x() = %+v, want a hidden helper", a)
	}
}

// withLock(k, () -> pay(x)): a project method that runs a closure is not a
// helper when what the closure does is shown.
func TestGetStepsShowsAProjectWrapperOfAVisibleCall(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{method("process"), method("withLock"), method("pay")},
		CallSites: []CallSite{
			{From: "process", To: "withLock", Line: 3, Col: 9, Text: "withLock(k, () -> …)", CalleeKey: "acme#withLock", ResultUse: "discarded"},
			{From: "process", To: "pay", Line: 3, Col: 27, Text: "pay(x)", CalleeKey: "acme#pay", ResultUse: "discarded",
				Frames: []StepFrame{{ID: "3:9", Kind: "closure", Text: "withLock(k, () -> …)"}}},
			{From: "withLock", Line: 2, Col: 13, Unresolved: "library", Text: "log.debug(k)", CalleeKey: "org.slf4j.Logger#debug", Receiver: "field", ResultUse: "discarded"},
			{From: "pay", Line: 2, Col: 12, Unresolved: "library", Text: "in.setHeader(H, x)", CalleeKey: "org.apache.camel.Message#setHeader",
				Receiver: "param", ResultUse: "discarded"},
		},
	}
	got, err := openWith(t, result).GetSteps(t.Context(), []string{"process"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := stepByText(t, got.Steps["process"], "withLock("); !s.Visible || s.Reason != "project" {
		t.Fatalf("withLock = %+v, want it shown: it runs pay", s)
	}
}

// Overrides of a method the flow calls are named even when the methods
// below the asked ones fill the load cap before that callee is reached.
func TestGetStepsNamesOverridesPastTheLoadCap(t *testing.T) {
	result := AnalysisResult{Nodes: []Node{method("a"), method("b"), method("decide"), method("impl")}}
	for i := range maxStepGraph {
		id := fmt.Sprintf("c%04d", i)
		result.Nodes = append(result.Nodes, method(id))
		result.CallSites = append(result.CallSites, CallSite{From: "a", To: id, Line: i + 2, Text: id + "()", CalleeKey: "acme#" + id})
	}
	result.CallSites = append(result.CallSites,
		CallSite{From: "b", To: "decide", Line: 2, Col: 9, Text: "decide()", CalleeKey: "acme#decide", ResultUse: "assigned"},
		CallSite{From: "decide", To: "impl", Line: 1, Role: RoleOverride})
	got, err := openWith(t, result).GetSteps(t.Context(), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := got.Steps["b"][0]; strings.Join(s.Impls, ",") != "impl" {
		t.Fatalf("decide = %+v, want its override named", s)
	}
}

// Lua and TypeScript record a call site with no text and no callee key. The
// outline could only title it "call", so it is folded away as what it is —
// unless an extractor saw something there, which says what it does.
func TestGetStepsFoldsACallItKnowsNothingAbout(t *testing.T) {
	svc := openAs(t, "lua", AnalysisResult{
		Nodes: []Node{method("a")},
		CallSites: []CallSite{
			{From: "a", Line: 2, Unresolved: "library", Text: "ngx.say(x)", CalleeKey: "ngx#say", ResultUse: "discarded"},
			{From: "a", Line: 3, Unresolved: "library"},
			{From: "a", Line: 4, Unresolved: "library"},
		},
		KafkaAccesses: []KafkaAccess{{From: "a", Topic: "orders", Role: KafkaProduce, Client: KafkaTemplate, File: "a.lua", Line: 4}},
	})
	got, err := svc.GetSteps(t.Context(), []string{"a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	steps := got.Steps["a"]
	if len(steps) != 3 {
		t.Fatalf("steps = %+v", steps)
	}
	if !steps[0].Visible || steps[0].Reason != "side effect" {
		t.Fatalf("call with text = %+v", steps[0])
	}
	if steps[1].Visible || steps[1].Reason != "unknown call" {
		t.Fatalf("unknown call = %+v", steps[1])
	}
	if !steps[2].Visible || steps[2].Reason != "Kafka produce" {
		t.Fatalf("call with an effect = %+v", steps[2])
	}
}
