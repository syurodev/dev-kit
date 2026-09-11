package preferences

import "sync"

type MemoryRepository struct {
	mu        sync.RWMutex
	theme     Theme
	hasTheme  bool
	family    ThemeFamily
	hasFamily bool
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
