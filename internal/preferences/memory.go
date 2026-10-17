package preferences

import "sync"

type MemoryRepository struct {
	mu        sync.RWMutex
	theme     Theme
	hasTheme  bool
	family    ThemeFamily
	hasFamily bool

	agentNotifications    bool
	hasAgentNotifications bool
	updateChecks          bool
	hasUpdateChecks       bool
	agentCheckpoints      AgentCheckpointSettings
	hasAgentCheckpoints   bool
	agentSandbox          AgentSandboxSettings
	hasAgentSandbox       bool
	codexAPIKeyAccount    string
	hasCodexAPIKeyAccount bool
	callVisibility        map[string]string
	hasCallVisibility     bool
	modelProfiles         AgentModelProfiles
	hasModelProfiles      bool
	subagentLimits        AgentSubagentLimits
	hasSubagentLimits     bool

	revealRequiresPassword    bool
	hasRevealRequiresPassword bool
}

func (r *MemoryRepository) GetRevealRequiresPassword() (bool, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.revealRequiresPassword, r.hasRevealRequiresPassword, nil
}

func (r *MemoryRepository) SetRevealRequiresPassword(required bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revealRequiresPassword = required
	r.hasRevealRequiresPassword = true
	return nil
}

func (r *MemoryRepository) GetAgentNotifications() (bool, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agentNotifications, r.hasAgentNotifications, nil
}

func (r *MemoryRepository) GetAgentCheckpoints() (AgentCheckpointSettings, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agentCheckpoints, r.hasAgentCheckpoints, nil
}

func (r *MemoryRepository) SetAgentCheckpoints(settings AgentCheckpointSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentCheckpoints = settings
	r.hasAgentCheckpoints = true
	return nil
}

func (r *MemoryRepository) GetAgentSandbox() (AgentSandboxSettings, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agentSandbox, r.hasAgentSandbox, nil
}

func (r *MemoryRepository) SetAgentSandbox(settings AgentSandboxSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentSandbox = settings
	r.hasAgentSandbox = true
	return nil
}

func (r *MemoryRepository) SetAgentNotifications(enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentNotifications = enabled
	r.hasAgentNotifications = true
	return nil
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{}
}

func (r *MemoryRepository) GetTheme() (Theme, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.theme, r.hasTheme, nil
}

func (r *MemoryRepository) SetTheme(theme Theme) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.theme = theme
	r.hasTheme = true
	return nil
}

func (r *MemoryRepository) GetThemeFamily() (ThemeFamily, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.family, r.hasFamily, nil
}

func (r *MemoryRepository) SetThemeFamily(family ThemeFamily) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.family = family
	r.hasFamily = true
	return nil
}

func (r *MemoryRepository) GetCodexAPIKeyAccount() (string, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.codexAPIKeyAccount, r.hasCodexAPIKeyAccount, nil
}

func (r *MemoryRepository) SetCodexAPIKeyAccount(accountID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.codexAPIKeyAccount = accountID
	r.hasCodexAPIKeyAccount = true
	return nil
}

func (r *MemoryRepository) GetUpdateChecks() (bool, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.updateChecks, r.hasUpdateChecks, nil
}

func (r *MemoryRepository) SetUpdateChecks(enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateChecks = enabled
	r.hasUpdateChecks = true
	return nil
}

func (r *MemoryRepository) GetCallVisibility() (map[string]string, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.callVisibility))
	for k, v := range r.callVisibility {
		out[k] = v
	}
	return out, r.hasCallVisibility, nil
}

func (r *MemoryRepository) SetCallVisibility(overrides map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callVisibility = make(map[string]string, len(overrides))
	for k, v := range overrides {
		r.callVisibility[k] = v
	}
	r.hasCallVisibility = true
	return nil
}

func (r *MemoryRepository) GetAgentModelProfiles() (AgentModelProfiles, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modelProfiles.clone(), r.hasModelProfiles, nil
}

func (r *MemoryRepository) SetAgentModelProfiles(profiles AgentModelProfiles) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modelProfiles = profiles.clone()
	r.hasModelProfiles = true
	return nil
}

func (r *MemoryRepository) GetAgentSubagentLimits() (AgentSubagentLimits, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.subagentLimits.clone(), r.hasSubagentLimits, nil
}

func (r *MemoryRepository) SetAgentSubagentLimits(limits AgentSubagentLimits) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subagentLimits = limits.clone()
	r.hasSubagentLimits = true
	return nil
}
