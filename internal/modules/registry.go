package modules

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// CurrentAPIVersion is the first version of the internal module contract.
// It is kept in the manifest so compatibility can be checked before a future
// module implementation is activated.
const CurrentAPIVersion = "1"

var (
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	versionPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	apiVersionPattern = regexp.MustCompile(`^[1-9][0-9]*$`)
	capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?::[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*)+$`)
)

// ModuleManifest is the declarative metadata registered by a built-in
// module. Capabilities are declarations only: registering a manifest never
// changes the permission policy or grants the module access to a capability.
type ModuleManifest struct {
	ID           string
	Title        string
	Version      string
	APIVersion   string
	Commands     []string
	Views        []string
	Capabilities []string
}

// Registry stores validated module manifests and their contribution IDs.
// Runtime handlers and permission grants live in their respective registries
// and are intentionally not part of this metadata registry.
type Registry struct {
	mu       sync.RWMutex
	modules  map[string]ModuleManifest
	commands map[string]string
	views    map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		modules:  map[string]ModuleManifest{},
		commands: map[string]string{},
		views:    map[string]string{},
	}
}

// ValidateManifest checks the stable, serializable part of the module
// contract before it can affect registry state.
func ValidateManifest(manifest ModuleManifest) error {
	if err := validateID("module id", manifest.ID); err != nil {
		return err
	}
	if manifest.Title == "" {
		return errors.New("module title is required")
	}
	if !versionPattern.MatchString(manifest.Version) {
		return fmt.Errorf("module %q version must be semantic version (for example, 1.0.0)", manifest.ID)
	}
	if !apiVersionPattern.MatchString(manifest.APIVersion) {
		return fmt.Errorf("module %q apiVersion must be a positive integer", manifest.ID)
	}

	if err := validateUniqueIDs("command", manifest.Commands); err != nil {
		return err
	}
	if err := validateUniqueIDs("view", manifest.Views); err != nil {
		return err
	}
	if err := validateUniqueCapabilities(manifest.Capabilities); err != nil {
		return err
	}
	return nil
}

func validateID(kind, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", kind)
	}
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("%s %q must contain only lowercase letters, numbers, '.', '_' or '-' and start with a letter", kind, value)
	}
	return nil
}

func validateUniqueIDs(kind string, ids []string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if err := validateID(kind+" id", id); err != nil {
			return err
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate %s contribution %q", kind, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateUniqueCapabilities(capabilities []string) error {
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if !capabilityPattern.MatchString(capability) {
			return fmt.Errorf("capability %q must use a namespaced lowercase identifier", capability)
		}
		if _, exists := seen[capability]; exists {
			return fmt.Errorf("duplicate capability declaration %q", capability)
		}
		seen[capability] = struct{}{}
	}
	return nil
}

// Register validates the complete manifest and checks all contribution IDs
// before mutating the registry. This makes a failed registration atomic: it
// cannot leave behind a module or a partial set of command/view contributions.
func (r *Registry) Register(manifest ModuleManifest) error {
	if r == nil {
		return errors.New("module registry is required")
	}
	if err := ValidateManifest(manifest); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.modules[manifest.ID]; exists {
		return fmt.Errorf("module %q is already registered", manifest.ID)
	}
	for _, commandID := range manifest.Commands {
		if owner, exists := r.commands[commandID]; exists {
			return fmt.Errorf("command %q is already contributed by module %q", commandID, owner)
		}
	}
	for _, viewID := range manifest.Views {
		if owner, exists := r.views[viewID]; exists {
			return fmt.Errorf("view %q is already contributed by module %q", viewID, owner)
		}
	}

	// Copy all slices before storing the manifest so callers cannot mutate the
	// registry through the input they passed to Register.
	stored := cloneManifest(manifest)
	r.modules[stored.ID] = stored
	for _, commandID := range stored.Commands {
		r.commands[commandID] = stored.ID
	}
	for _, viewID := range stored.Views {
		r.views[viewID] = stored.ID
	}
	return nil
}

// List returns a deterministic, caller-owned snapshot of registered manifests.
// The returned slices can be changed safely without affecting the registry.
func (r *Registry) List() []ModuleManifest {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ModuleManifest, 0, len(r.modules))
	for _, manifest := range r.modules {
		result = append(result, cloneManifest(manifest))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

func cloneManifest(manifest ModuleManifest) ModuleManifest {
	manifest.Commands = append([]string(nil), manifest.Commands...)
	manifest.Views = append([]string(nil), manifest.Views...)
	manifest.Capabilities = append([]string(nil), manifest.Capabilities...)
	return manifest
}
