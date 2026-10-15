package codegraph

import (
	"fmt"
	"strings"
)

// Which calls a flow outline shows and which it folds away, decided at read
// time so a rule can change without a re-index. The first rule that has an
// answer wins, and every answer comes with the reason the outline shows for
// it. A signal whose input an analyzer did not record ("" or "unknown") is
// skipped, so a language that records less still gets an answer.

// CallStats is how often each callee is called across the index.
type CallStats struct {
	Callers map[string]int // callee key → distinct calling methods
	Methods int            // method nodes in the index
}

// StepFacts is what Visibility judges a call by.
type StepFacts struct {
	CalleeKey string
	Text      string
	Receiver  string
	ResultUse string
	// Project is true when the call lands on a node of this project.
	Project bool
	// Helper: a project callee none of whose own steps is visible (steps.go).
	Helper bool
	// WrapsProjectCall: a closure passed to this call holds a visible project call.
	WrapsProjectCall bool
	Effect           *StepEffect
}

// A callee called from at least commonMin methods that are at least
// commonShare of the index is plumbing: logging, auditing, a shared helper.
// First guesses, to tune on real projects.
const (
	commonShare = 0.15
	commonMin   = 8
)

// infraPrefixes match a callee key's start; one table, rows for every language.
var infraPrefixes = []struct{ prefix, reason string }{
	{"org.slf4j.", "logging"},
	{"org.apache.logging.", "logging"},
	{"org.apache.commons.logging.", "logging"},
	{"java.util.logging.", "logging"},
	{"ch.qos.logback.", "logging"},
	{"io.micrometer.", "metrics"},
	{"io.opentelemetry.", "metrics"},
	{"io.prometheus.", "metrics"},
	{"java.lang.String#", "utility"},
	{"java.lang.StringBuilder#", "utility"},
	{"java.lang.Math#", "utility"},
	{"java.util.Objects#", "utility"},
	{"java.util.Optional#", "utility"},
	{"java.util.List#", "utility"},
	{"java.util.Map#", "utility"},
	{"java.util.Set#", "utility"},
	{"java.util.Collection#", "utility"},
	{"java.util.Collections#", "utility"},
	{"java.util.Arrays#", "utility"},
	{"java.util.stream.", "utility"},
	{"java.time.", "utility"},
	// Lua: OpenResty and Kong.
	{"ngx#log", "logging"},
	{"kong.log", "logging"},
	{"string#", "utility"},
	{"table#", "utility"},
	// JavaScript / TypeScript.
	{"console#", "logging"},
	{"JSON#", "utility"},
	{"Math#", "utility"},
	{"Object#", "utility"},
	{"Array#", "utility"},
}

// infraSimpleTypes catch a Java key whose declaring type did not resolve and
// fell back to the declared simple name ("Counter#increment").
var infraSimpleTypes = map[string]string{
	"Logger": "logging", "Log": "logging",
	"Counter": "metrics", "MeterRegistry": "metrics", "Timer": "metrics", "Gauge": "metrics",
	"Tracer": "metrics", "Span": "metrics",
	"Optional": "utility", "Objects": "utility", "String": "utility", "StringBuilder": "utility",
}

// Visibility decides whether a step is shown, and why.
func Visibility(f StepFacts, stats CallStats, overrides map[string]string) (bool, string) {
	if mode, ok := overrideFor(f.CalleeKey, overrides); ok {
		return mode == "show", "you chose"
	}
	if f.Project {
		if f.Helper {
			return false, "helper"
		}
		return true, "project"
	}
	if f.WrapsProjectCall {
		return true, "wraps a project call"
	}
	if f.Effect != nil {
		return true, effectLabel(*f.Effect)
	}
	if f.ResultUse == "condition" {
		return true, "decides a branch"
	}
	if reason := infraReason(f.CalleeKey); reason != "" {
		// A collection on a field is the object's state, not plumbing.
		if !(reason == "utility" && f.Receiver == "field" && f.ResultUse == "discarded") {
			return false, reason
		}
	}
	if writesState(f.Receiver) && f.ResultUse == "discarded" {
		return true, "writes to " + receiverName(f.Text)
	}
	if n := stats.Callers[f.CalleeKey]; f.CalleeKey != "" && stats.Methods > 0 &&
		n >= commonMin && float64(n)/float64(stats.Methods) >= commonShare {
		return false, fmt.Sprintf("common (%d methods)", n)
	}
	switch {
	case f.ResultUse == "discarded":
		return true, "side effect"
	case isAccessor(memberOf(f.CalleeKey)) && f.ResultUse != "" && f.ResultUse != "unknown":
		return false, "accessor"
	default:
		return false, "value"
	}
}

// overrideFor looks the key up, then its type with "#*".
func overrideFor(key string, overrides map[string]string) (string, bool) {
	if key == "" || len(overrides) == 0 {
		return "", false
	}
	if mode, ok := overrides[key]; ok && (mode == "show" || mode == "hide") {
		return mode, true
	}
	if i := strings.IndexByte(key, '#'); i >= 0 {
		if mode, ok := overrides[key[:i]+"#*"]; ok && (mode == "show" || mode == "hide") {
			return mode, true
		}
	}
	return "", false
}

func infraReason(key string) string {
	if key == "" {
		return ""
	}
	for _, p := range infraPrefixes {
		if strings.HasPrefix(key, p.prefix) {
			return p.reason
		}
	}
	typ, member := key, ""
	if i := strings.IndexByte(key, '#'); i >= 0 {
		typ, member = key[:i], key[i+1:]
	}
	if !strings.Contains(typ, ".") {
		if reason, ok := infraSimpleTypes[typ]; ok {
			return reason
		}
	}
	simple := typ[strings.LastIndexByte(typ, '.')+1:]
	if member == "builder" || member == "build" || strings.HasSuffix(simple, "Builder") {
		return "builder"
	}
	return ""
}

func writesState(receiver string) bool {
	return receiver == "param" || receiver == "field" || receiver == "local-from-param"
}

// receiverName is the identifier a call is made on, read off its text.
func receiverName(text string) string {
	end := 0
	for end < len(text) {
		c := text[end]
		if c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || end > 0 && c >= '0' && c <= '9' {
			end++
			continue
		}
		break
	}
	if end == 0 || end >= len(text) || text[end] != '.' {
		return "its receiver"
	}
	return text[:end]
}

func memberOf(key string) string {
	if i := strings.IndexByte(key, '#'); i >= 0 {
		return key[i+1:]
	}
	return ""
}

func isAccessor(member string) bool {
	for _, p := range []string{"get", "is", "has"} {
		if member == p {
			return true
		}
		if strings.HasPrefix(member, p) && len(member) > len(p) {
			if c := member[len(p)]; c >= 'A' && c <= 'Z' {
				return true
			}
		}
	}
	return false
}

func effectLabel(e StepEffect) string {
	kind := map[string]string{"kafka": "Kafka", "http": "HTTP", "redis": "Redis", "db": "DB"}[e.Kind]
	if kind == "" {
		kind = e.Kind
	}
	return kind + " " + e.Role
}
