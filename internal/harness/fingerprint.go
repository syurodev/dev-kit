package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// fingerprintVersion changes when the set of hashed facts changes, so two
// builds never compare fingerprints of different shapes as equal.
const fingerprintVersion = "sha256:v1:"

// fingerprint binds a proposal or a snapshot to the inputs a diff depends on:
// the V, W and S revisions, the registry's tool names and exemptions, and the
// vendor-defined servers and skills with their commands. Sets are sorted and
// paths canonical, so ordering never causes a change. It holds names, shapes
// and paths only: no environment values and no vendor error text. Vendor
// files can still change after this is computed; it records what was seen.
func fingerprint(in Inputs, envSkills map[string]bool) string {
	type serverFact struct{ Name, Shape string }
	facts := struct {
		Vendor    int64
		Workspace *int64
		Session   *int64
		Registry  []string
		Servers   []serverFact
		Skills    []string
	}{Vendor: in.Vendor.Revision}
	if in.Workspace != nil {
		r := in.Workspace.Revision
		facts.Workspace = &r
	}
	if in.Session != nil {
		r := in.Session.Revision
		facts.Session = &r
	}
	for _, t := range in.Registry.Tools {
		mark := t.Name
		if t.BudgetExempt {
			mark += "!"
		}
		facts.Registry = append(facts.Registry, mark)
	}
	sort.Strings(facts.Registry)
	for _, s := range in.Environment.Servers {
		facts.Servers = append(facts.Servers, serverFact{s.Name, serverShape(s)})
	}
	sort.Slice(facts.Servers, func(i, j int) bool { return facts.Servers[i].Name < facts.Servers[j].Name })
	facts.Skills = sortedKeys(envSkills)
	b, _ := json.Marshal(facts)
	sum := sha256.Sum256(b)
	return fingerprintVersion + hex.EncodeToString(sum[:])
}
