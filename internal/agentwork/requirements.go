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
	WorkspaceID      string            `json:"workspaceId"`
	BoardID          string            `json:"boardId"`
	TaskID           string            `json:"taskId"`
	Title            string            `json:"title"`
	Description      string            `json:"description"`
	RoleContract     string            `json:"roleContract"`
	Acceptance       []string          `json:"acceptance"`
	Scope            []string          `json:"scope"`
	Dependencies     []string          `json:"dependencies"`
	DependencyInputs []DependencyInput `json:"dependencyInputs"`
}

// DependencyInput binds the resolved input revision as well as task identity.
// ResultRevision is empty while there is no accepted result; it is never a
// worker's claim of acceptance. Host adapters must resolve these values.
type DependencyInput struct {
	TaskID               string `json:"taskId"`
	RequirementsRevision string `json:"requirementsRevision"`
	ResultRevision       string `json:"resultRevision"`
}

// RequirementsDigest returns a versioned content revision, independent of task
// updatedAt. Dependency and scope order have no significance; acceptance order
// and all task text are preserved. Callers must reload the host snapshot before
// dispatch and acceptance, rather than trusting a caller-supplied digest.
func RequirementsDigest(snapshot RequirementsSnapshot) (string, error) {
	if strings.TrimSpace(snapshot.WorkspaceID) == "" || strings.TrimSpace(snapshot.BoardID) == "" || strings.TrimSpace(snapshot.TaskID) == "" {
		return "", errors.New("requirements snapshot requires workspace, board and task identity")
	}
	snapshot.Scope = canonicalSet(snapshot.Scope)
	snapshot.Dependencies = canonicalSet(snapshot.Dependencies)
	inputs := append([]DependencyInput{}, snapshot.DependencyInputs...)
	slices.SortFunc(inputs, func(a, b DependencyInput) int { return strings.Compare(a.TaskID, b.TaskID) })
	if len(inputs) != len(snapshot.Dependencies) {
		return "", errors.New("requirements snapshot requires resolved dependency revisions")
	}
	for index, input := range inputs {
		if input.TaskID != snapshot.Dependencies[index] || strings.TrimSpace(input.RequirementsRevision) == "" {
			return "", errors.New("requirements snapshot dependency revision mismatch")
		}
	}
	snapshot.DependencyInputs = inputs
	if snapshot.Acceptance == nil {
		snapshot.Acceptance = []string{}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(append([]byte("devkit-managed-requirements-v1\x00"), encoded...))
	return "sha256:v1:" + hex.EncodeToString(hash[:]), nil
}

func canonicalSet(values []string) []string {
	result := append([]string{}, values...)
	slices.Sort(result)
	return slices.Compact(result)
}
