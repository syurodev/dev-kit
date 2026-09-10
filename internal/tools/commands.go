package tools

// Command ids, as the registry, the capability grants, the palette and the MCP
// surface all spell them.
//
// Kebab-case, not the camelCase tools.mdx proposed: module ids are validated
// against ^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$ (internal/modules/registry.go),
// which has no room for a capital. The design named the commands before that
// rule was written down; the rule is the one that is enforced, and every id
// already in the app — tools.hash, notes.create, database.query — is lowercase.
const (
	CommandHash         = "tools.hash"
	CommandBase64Encode = "tools.base64-encode"
	CommandBase64Decode = "tools.base64-decode"
	CommandUUIDGenerate = "tools.uuid-generate"
	CommandURLEncode    = "tools.url-encode"
	CommandURLDecode    = "tools.url-decode"
	CommandJSONFormat   = "tools.json-format"
)

// Capabilities guarding them. tools.mdx splits these deliberately: the util
// group shares one capability because the whole group has the same risk
// profile — no network, no mutation, no vault — unlike SSH, where connect and
// forward are separated because they differ. Adding a capability without
// touching an existing grant is the backward-compatible move technical
// contracts describes.
const (
	CapabilityHash = "tools:hash"
	CapabilityUtil = "tools:util"
)

// SubjectID is the audit subject every tools command runs as.
const SubjectID = "builtin.tools"

// UtilRequest is what every util command receives. One shape rather than six:
// the fields a given command ignores cost nothing, and a single shape means the
// bridge decodes once instead of switching per command.
type UtilRequest struct {
	// Input is the text to convert. Unused by CommandUUIDGenerate.
	Input string
	// Version selects the UUID layout ("v4", "v7"). Only CommandUUIDGenerate
	// reads it.
	Version string
}

// Descriptor is everything the rest of the app needs to know about a command
// without knowing what it does.
type Descriptor struct {
	ID         string
	Title      string
	Capability string
}

// Commands is the whole surface, in the order the rail lists them.
//
// One table so registration, the module manifest, the permission grants and
// the bridge's capability lookup cannot drift apart — the failure mode of
// spreading this across four files is a command that runs while its grant says
// it should not, which nothing would notice.
var Commands = []Descriptor{
	{ID: CommandHash, Title: "Hash text", Capability: CapabilityHash},
	{ID: CommandBase64Encode, Title: "Base64 encode", Capability: CapabilityUtil},
	{ID: CommandBase64Decode, Title: "Base64 decode", Capability: CapabilityUtil},
	{ID: CommandUUIDGenerate, Title: "Generate UUID", Capability: CapabilityUtil},
	{ID: CommandURLEncode, Title: "URL encode", Capability: CapabilityUtil},
	{ID: CommandURLDecode, Title: "URL decode", Capability: CapabilityUtil},
	{ID: CommandJSONFormat, Title: "Format JSON", Capability: CapabilityUtil},
}

// CapabilityFor reports which capability guards a command, and whether the
// command belongs to Tools at all. A caller that gets false must refuse rather
// than pick a default: running an unknown command under a capability meant for
// a known one is exactly the authorisation bug the table exists to prevent.
func CapabilityFor(id string) (string, bool) {
	for _, command := range Commands {
		if command.ID == id {
			return command.Capability, true
		}
	}
	return "", false
}

// CommandIDs lists every id, for the module manifest.
func CommandIDs() []string {
	ids := make([]string, 0, len(Commands))
	for _, command := range Commands {
		ids = append(ids, command.ID)
	}
	return ids
}

// CapabilityIDs lists each distinct capability once, in first-use order.
func CapabilityIDs() []string {
	seen := map[string]bool{}
	result := make([]string, 0, 2)
	for _, command := range Commands {
		if seen[command.Capability] {
			continue
		}
		seen[command.Capability] = true
		result = append(result, command.Capability)
	}
	return result
}
