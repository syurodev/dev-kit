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
	agentCheckpoints      AgentCheckpointSettings
	hasAgentCheckpoints   bool
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
