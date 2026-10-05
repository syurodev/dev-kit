package accounts

import "strings"

// Provider is one service DevKit can hold an identity on. It is a value in a
// registry rather than a branch in the service, the same shape codegraph's
// Toolchain uses: adding a provider is adding an entry, not editing logic.
type Provider struct {
	ID    string
	Name  string
	Kinds []Kind
	// Verify says how to check the credential is real and who it belongs to.
	// Zero means the provider cannot verify itself — the connection providers
	// below, whose real check is the owning module's Test Connection button.
	Verify VerifySpec
	// Hosts are the hosts this provider's accounts are for. It is what lets a
	// request to api.github.com find the GitHub account without a menu. One
	// match is chosen; two or more must be asked about, never guessed.
	Hosts []string
	// SSHHost is where this provider answers SSH, for identifying a key. Empty
	// means keys cannot be verified against it and it accepts none.
	SSHHost string
}

// VerifySpec is one HTTPS request and where to read the answer.
type VerifySpec struct {
	// URL is the endpoint that identifies the caller.
	URL string
	// Header is how the credential is presented, as a format with one %s.
	Header       string
	HeaderFormat string
	// SubjectField is the JSON field naming who this is.
	SubjectField string
	// ScopesHeader is the response header listing granted scopes, comma
	// separated; empty when the provider does not report them.
	ScopesHeader string
}

// Verifiable reports whether the provider can check a credential by itself.
func (p Provider) Verifiable() bool { return p.Verify.URL != "" }

// Accepts reports whether this provider holds credentials of kind k.
func (p Provider) Accepts(k Kind) bool {
	for _, candidate := range p.Kinds {
		if candidate == k {
			return true
		}
	}
	return false
}

// Connection provider ids. These exist so a scoped credential has a provider
// like everything else — one listing, one vocabulary — not because DevKit can
// verify them: the module that owns the connection does that.
const (
	ProviderDatabase = "database"
	ProviderSSH      = "ssh"
	ProviderKafka    = "kafka"
	ProviderGeneric  = "generic"
)

var bearer = VerifySpec{Header: "Authorization", HeaderFormat: "Bearer %s"}

// DefaultProviders is the registry. A provider not listed here is `generic`,
// which takes whatever verify URL the user types.
func DefaultProviders() []Provider {
	github := bearer
	github.URL = "https://api.github.com/user"
	github.SubjectField = "login"
	github.ScopesHeader = "X-OAuth-Scopes"

	gitlab := VerifySpec{Header: "PRIVATE-TOKEN", HeaderFormat: "%s", URL: "https://gitlab.com/api/v4/user", SubjectField: "username"}

	anthropic := VerifySpec{Header: "x-api-key", HeaderFormat: "%s"}

	return []Provider{
		{ID: "github", Name: "GitHub", Kinds: []Kind{KindToken, KindOAuth, KindSSHKey}, Verify: github, SSHHost: "github.com",
			Hosts: []string{"github.com", "api.github.com", "raw.githubusercontent.com"}},
		{ID: "gitlab", Name: "GitLab", Kinds: []Kind{KindToken, KindOAuth, KindSSHKey}, Verify: gitlab, SSHHost: "gitlab.com",
			Hosts: []string{"gitlab.com"}},
		{ID: "bitbucket", Name: "Bitbucket", Kinds: []Kind{KindToken, KindBasic, KindSSHKey}, SSHHost: "bitbucket.org",
			Hosts: []string{"bitbucket.org", "api.bitbucket.org"}},
		{ID: "anthropic", Name: "Anthropic", Kinds: []Kind{KindToken}, Verify: anthropic,
			Hosts: []string{"api.anthropic.com"}},
		// External: Codex's ChatGPT sign-in, kept in Codex's own home.
		{ID: "openai", Name: "OpenAI", Kinds: []Kind{KindToken, KindExternal}, Hosts: []string{"api.openai.com"}},
		{ID: "google", Name: "Google", Kinds: []Kind{KindToken, KindOAuth}},
		{ID: "kong", Name: "Kong Admin", Kinds: []Kind{KindToken, KindBasic}},
		{ID: ProviderDatabase, Name: "Database", Kinds: []Kind{KindSecret, KindBasic}},
		{ID: ProviderSSH, Name: "SSH", Kinds: []Kind{KindSecret}},
		{ID: ProviderKafka, Name: "Kafka", Kinds: []Kind{KindSecret, KindBasic}},
		{ID: ProviderGeneric, Name: "Generic", Kinds: []Kind{KindToken, KindBasic, KindSecret, KindExternal}},
	}
}

// Registry looks providers up by id and by host.
type Registry struct {
	byID   map[string]Provider
	byHost map[string][]string
}

func NewRegistry(providers []Provider) *Registry {
	r := &Registry{byID: map[string]Provider{}, byHost: map[string][]string{}}
	for _, p := range providers {
		r.byID[p.ID] = p
		for _, host := range p.Hosts {
			r.byHost[strings.ToLower(host)] = append(r.byHost[strings.ToLower(host)], p.ID)
		}
	}
	return r
}

func (r *Registry) Get(id string) (Provider, bool) {
	if r == nil {
		return Provider{}, false
	}
	p, ok := r.byID[id]
	return p, ok
}

func (r *Registry) List() []Provider {
	if r == nil {
		return nil
	}
	out := make([]Provider, 0, len(r.byID))
	for _, p := range DefaultProviders() {
		if known, ok := r.byID[p.ID]; ok {
			out = append(out, known)
		}
	}
	return out
}

// ProvidersForHost returns the provider ids whose hosts cover host. The match
// is exact or on a registered suffix, so api.github.com finds GitHub and
// evil-github.com.attacker.test does not.
func (r *Registry) ProvidersForHost(host string) []string {
	if r == nil || host == "" {
		return nil
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if ids, ok := r.byHost[host]; ok {
		return ids
	}
	var out []string
	for registered, ids := range r.byHost {
		if strings.HasSuffix(host, "."+registered) {
			out = append(out, ids...)
		}
	}
	return out
}
