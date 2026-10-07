package codegraph

import (
	"strings"
	"unicode"
)

// ConfigKey is one property a config file sets (application.yml,
// application-prod.properties…), flattened to a dotted key. Value is what the
// file says, never what the running service sees: profiles, environment
// variables and command-line flags all sit above it. A key that names a secret
// has Redacted set and no Value.
type ConfigKey struct {
	Key  string `json:"key"`
	File string `json:"file"`
	Line int    `json:"line"`
	// Value is the text as written, clipped; empty when Redacted.
	Value    string `json:"value"`
	Redacted bool   `json:"redacted,omitempty"`
	// Profile is the profile the entry applies to; empty for every profile.
	Profile string `json:"profile,omitempty"`
	// EnvVars are the environment variables the value is built from
	// (${NAME:default}).
	EnvVars []string `json:"envVars,omitempty"`
}

// The kinds of ConfigUsage.
const (
	ConfigUseValue         = "value"          // @Value
	ConfigUseProperties    = "properties"     // @ConfigurationProperties binding
	ConfigUseCondition     = "condition"      // @ConditionalOnProperty and friends naming a key
	ConfigUseEnvironment   = "environment"    // Environment.getProperty
	ConfigUseEnvVar        = "env-var"        // System.getenv
	ConfigUseProfile       = "profile"        // @Profile
	ConfigUseBeanCondition = "bean-condition" // @ConditionalOnMissingBean… — no key
)

// ConfigUsage is one place the code reads a property or switches on one.
// From is the class (a field, a binding, a gated bean) or the method that
// holds the use; Field is where the value lands, empty when it lands nowhere.
type ConfigUsage struct {
	Key   string `json:"key,omitempty"`
	Kind  string `json:"kind"`
	From  string `json:"from"`
	Field string `json:"field,omitempty"`
	File  string `json:"file"`
	Line  int    `json:"line"`
	// DefaultValue is the default written beside the key: ${a.b:false} → "false".
	DefaultValue string `json:"defaultValue,omitempty"`
	// Detail is what else the source says: havingValue/matchIfMissing,
	// profile names, the condition's target.
	Detail string `json:"detail,omitempty"`
}

// CanonicalConfigKey is the form two spellings of one property share. Spring
// binds FSAP_ACCESS_MODULE_BASED_ENABLED, fsap.access.module-based-enabled and
// fsap.access.moduleBasedEnabled to the same setting by dropping everything
// but letters and digits and ignoring case, and so does this.
func CanonicalConfigKey(key string) string {
	var b strings.Builder
	for _, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
