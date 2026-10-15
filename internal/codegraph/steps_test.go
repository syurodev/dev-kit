package codegraph

import (
	"errors"
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
	if _, err := svc.GetSteps(t.Context(), ids, nil); !errors.Is(err, ErrTooManyNodes) {
		t.Fatalf("err = %v, want ErrTooManyNodes", err)
	}
}

func TestGetStepsOfALegacyIndex(t *testing.T) {
	svc := openWith(t, AnalysisResult{Nodes: []Node{method("a"), method("b")},
		CallSites: []CallSite{{From: "a", To: "b", Line: 2}, {From: "a", Line: 3, Unresolved: "library"}}})
	got, err := svc.GetSteps(t.Context(), []string{"a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Legacy || len(got.Steps["a"]) != 2 {
		t.Fatalf("got = %+v", got)
	}
}
