package codegraph

import "testing"

func TestVisibility(t *testing.T) {
	stats := CallStats{Methods: 40, Callers: map[string]int{"acme.Audit#record": 12, "acme.Rare#call": 2}}
	cases := []struct {
		name    string
		facts   StepFacts
		over    map[string]string
		visible bool
		reason  string
	}{
		// The worked example (spec §2): fsap-event-router's process() and quarantine().
		{"setHeader writes to the message", StepFacts{CalleeKey: "org.apache.camel.Message#setHeader", Text: "in.setHeader(X, y)",
			Receiver: "local-from-param", ResultUse: "discarded"}, nil, true, "writes to in"},
		{"producerTemplate.send", StepFacts{CalleeKey: "org.apache.camel.ProducerTemplate#send", Text: "producerTemplate.send(uri, exchange)",
			Receiver: "field", ResultUse: "discarded"}, nil, true, "writes to producerTemplate"},
		{"executeCallable wraps deliver", StepFacts{CalleeKey: "io.github.resilience4j.circuitbreaker.CircuitBreaker#executeCallable",
			Receiver: "local", ResultUse: "discarded", WrapsProjectCall: true}, nil, true, "wraps a project call"},
		{"log.warn", StepFacts{CalleeKey: "org.slf4j.Logger#warn", Receiver: "unknown", ResultUse: "discarded"}, nil, false, "logging"},
		{"counter increment, unresolved type", StepFacts{CalleeKey: "Counter#increment", Receiver: "local", ResultUse: "discarded"}, nil, false, "metrics"},
		{"exchange.getIn", StepFacts{CalleeKey: "org.apache.camel.Exchange#getIn", Receiver: "param", ResultUse: "assigned"}, nil, false, "accessor"},
		{"in.getHeader", StepFacts{CalleeKey: "org.apache.camel.Message#getHeader", Receiver: "local-from-param", ResultUse: "argument"}, nil, false, "accessor"},
		{"circuitBreaker(...) is assigned", StepFacts{CalleeKey: "io.github.resilience4j.circuitbreaker.CircuitBreakerRegistry#circuitBreaker",
			Receiver: "field", ResultUse: "assigned"}, nil, false, "value"},
		{"a project call", StepFacts{CalleeKey: "acme.P#quarantine", Project: true}, nil, true, "project"},
		{"a project helper", StepFacts{CalleeKey: "acme.P#metricPrefix", Project: true, Helper: true}, nil, false, "helper"},

		// One row per remaining rule.
		{"override show beats infra", StepFacts{CalleeKey: "org.slf4j.Logger#warn"}, map[string]string{"org.slf4j.Logger#warn": "show"}, true, "you chose"},
		{"type-wide override", StepFacts{CalleeKey: "org.apache.camel.Message#setHeader", Receiver: "local-from-param", ResultUse: "discarded"},
			map[string]string{"org.apache.camel.Message#*": "hide"}, false, "you chose"},
		{"method override beats type override", StepFacts{CalleeKey: "org.apache.camel.Message#setHeader"},
			map[string]string{"org.apache.camel.Message#*": "hide", "org.apache.camel.Message#setHeader": "show"}, true, "you chose"},
		{"an effect", StepFacts{CalleeKey: "org.springframework.kafka.core.KafkaTemplate#send", Effect: &StepEffect{Kind: "kafka", Role: "produce", Target: "t"}},
			nil, true, "Kafka produce"},
		{"decides a branch", StepFacts{CalleeKey: "acme.Lib#ready", ResultUse: "condition"}, nil, true, "decides a branch"},
		{"utility on a temporary", StepFacts{CalleeKey: "java.util.List#add", Receiver: "local", ResultUse: "discarded"}, nil, false, "utility"},
		{"utility on a field is a state write", StepFacts{CalleeKey: "java.util.Map#put", Text: "cache.put(k, v)", Receiver: "field", ResultUse: "discarded"},
			nil, true, "writes to cache"},
		{"builder", StepFacts{CalleeKey: "com.x.Request.Builder#header", Receiver: "local", ResultUse: "argument"}, nil, false, "builder"},
		{"common in this project", StepFacts{CalleeKey: "acme.Audit#record", Receiver: "local", ResultUse: "discarded"}, nil, false, "common (12 methods)"},
		{"rare side effect", StepFacts{CalleeKey: "acme.Rare#call", Receiver: "local", ResultUse: "discarded"}, nil, true, "side effect"},
		{"unknown receiver skips the write rule", StepFacts{CalleeKey: "acme.Rare#call", Receiver: "unknown", ResultUse: "discarded"}, nil, true, "side effect"},
		{"legacy row: no key, no use", StepFacts{}, nil, false, "value"},
		{"lua logging", StepFacts{CalleeKey: "ngx#log", ResultUse: "discarded"}, nil, false, "logging"},
		{"ts console", StepFacts{CalleeKey: "console#log", ResultUse: "discarded"}, nil, false, "logging"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			visible, reason := Visibility(c.facts, stats, c.over)
			if visible != c.visible || reason != c.reason {
				t.Fatalf("Visibility = (%v, %q), want (%v, %q)", visible, reason, c.visible, c.reason)
			}
		})
	}
}
