package agentwork

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// RequirementsSnapshot is assembled by the host from the current task and its
// authorized scope. It deliberately excludes progress, comments, timestamps,
// and execution state. A digest detects change; it never conveys permission.
// Never persist this plaintext snapshot in the execution metadata ledger.
type RequirementsSnapshot struct {
	WorkspaceID string `json:"workspaceId"`
	BoardID     string `json:"boardId"`
	TaskID      string `json:"taskId"`
	Branch      string `json:"branch"`
	// Deprecated compatibility fields. They are intentionally excluded from v2.
	Title            string            `json:"title,omitempty"`
	Description      string            `json:"description"`
	RoleContract     string            `json:"roleContract,omitempty"`
	Acceptance       []string          `json:"acceptance,omitempty"`
	Scope            []string          `json:"scope"`
	Dependencies     []string          `json:"dependencies"`
	DependencyInputs []DependencyInput `json:"dependencyInputs"`
	Progress         string            `json:"progress,omitempty"`
}

// DependencyInput is retained for source compatibility with earlier callers.
// Dependency result/revision bindings belong to later immutable context/result
// records and are deliberately ignored by RequirementsDigest.
type DependencyInput struct {
	TaskID               string `json:"taskId"`
	RequirementsRevision string `json:"requirementsRevision"`
	ResultRevision       string `json:"resultRevision"`
}

// RequirementsDigest returns the v2 requirements revision approved for TEAM1:
// host-resolved workspace/board/task identity, description (which carries the
// acceptance criteria), sorted blockedBy task IDs, folder scope and branch.
// Progress, title, status, role contract, separate acceptance fields and
// dependency result bindings do not affect this hash. Callers must reload the
// host snapshot before dispatch and acceptance, rather than trusting a
// caller-supplied digest.
func RequirementsDigest(snapshot RequirementsSnapshot) (string, error) {
	if strings.TrimSpace(snapshot.WorkspaceID) == "" || strings.TrimSpace(snapshot.BoardID) == "" || strings.TrimSpace(snapshot.TaskID) == "" {
		return "", errors.New("requirements snapshot requires workspace, board and task identity")
	}
	canonical := struct {
		WorkspaceID string   `json:"workspaceId"`
		BoardID     string   `json:"boardId"`
		TaskID      string   `json:"taskId"`
		Description string   `json:"description"`
		BlockedBy   []string `json:"blockedBy"`
		FolderScope []string `json:"folderScope"`
		Branch      string   `json:"branch"`
	}{
		WorkspaceID: snapshot.WorkspaceID,
		BoardID:     snapshot.BoardID,
		TaskID:      snapshot.TaskID,
		Description: snapshot.Description,
		BlockedBy:   canonicalSet(snapshot.Dependencies),
		FolderScope: canonicalSet(snapshot.Scope),
		Branch:      snapshot.Branch,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(append([]byte("devkit-managed-requirements-v2\x00"), encoded...))
	return "sha256:v2:" + hex.EncodeToString(hash[:]), nil
}

func canonicalSet(values []string) []string {
	result := append([]string{}, values...)
	slices.Sort(result)
	return slices.Compact(result)
}
