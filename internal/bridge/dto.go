package bridge

type ModuleDTO struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Commands []string `json:"commands"`
	Views    []string `json:"views"`
}

type ExecuteCommandRequest struct {
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload"`
}

// VaultSearchResult is one cross-type search hit. Label is the item's plaintext
// display name (note title or profile name); no secret content is included.
type VaultSearchResult struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Label     string `json:"label"`
	UpdatedAt string `json:"updated_at"`
}
