package apiclient

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Interchange is how the API client reads and writes other tools' files.
//
// Every format goes through one model, the Bundle, and never touches the vault
// itself: a format turns bytes into a Bundle (Decoder) or a Bundle into bytes
// (Encoder), and this file is the only place a Bundle becomes vault items or
// vault items become a Bundle. So the rules that matter — a secret value never
// leaves, an account is exported as a placeholder, an environment needs a tag
// — are written once, and a format cannot get them wrong.
//
// Adding a format is a new file and a line in `formats`. It may not touch
// the model: if it needs something the Bundle does not carry, the Bundle grows
// a tool-neutral field, never a field shaped like one tool.

var (
	// ErrUnknownFormat is a format id nothing is registered under, or a file
	// no registered format recognises.
	ErrUnknownFormat = errors.New("apiclient.unknownFormat")
	// ErrFormatCannot is a format asked for a direction or a scope it does not
	// do — exporting an environment as a curl script.
	ErrFormatCannot = errors.New("apiclient.formatCannot")
	// ErrNotRecognised is a file the chosen format cannot read.
	ErrNotRecognised = errors.New("apiclient.notRecognised")
	// ErrTagRequired is an import holding an environment with no env tag to
	// put it under. The tag decides whether a send asks first, so it is never
	// guessed.
	ErrTagRequired = errors.New("apiclient.environmentTagRequired")
)

// MaxImportBytes bounds an import. A collection is text; one this size is
// already thousands of requests.
const MaxImportBytes = 20 << 20

// ---- the model -------------------------------------------------------

// Bundle is a portable slice of the API client, in no tool's shape.
type Bundle struct {
	Collections  []BundleCollection
	Environments []BundleEnvironment
	// Globals is nil when the bundle carries none, which is not the same as
	// carrying an empty set.
	Globals []BundleVar
}

// BundleCollection is a collection or a folder in one.
type BundleCollection struct {
	Name     string
	Vars     []BundleVar
	Folders  []BundleCollection
	Requests []BundleRequest
}

// BundleRequest is a request and how it authenticates.
type BundleRequest struct {
	// Request's ID, CollectionID and OriginRef mean nothing outside this vault
	// and are ignored on import.
	Request RequestInput
	// Credential is set when the request authenticates as an account. It
	// names the variables standing in for the value; there is no field that
	// could hold the value.
	Credential *BundleCredential
}

// BundleCredential is an account binding, exported as placeholders.
type BundleCredential struct {
	Kind AuthKind
	// Header is the header name for AuthHeader.
	Header string
	// TokenVar is the variable for a bearer token or an API key; UserVar and
	// PasswordVar for basic auth.
	TokenVar    string
	UserVar     string
	PasswordVar string
}

// BundleVar is one variable. A secret's Value is empty on export, always.
type BundleVar struct {
	Key      string
	Value    string
	Secret   bool
	Disabled bool
}

// BundleEnvironment is an environment. Tag is empty when the file's format
// has nowhere to keep one.
type BundleEnvironment struct {
	Name string
	Tag  string
	Vars []BundleVar
}

// Report collects what a conversion could not carry over, by reason, so the
// person is told once per kind of thing rather than once per row.
type Report struct {
	skipped map[string]int
	order   []string
}

// Skip records one thing left behind. reason is a noun phrase: "file upload
// field", "pre-request script".
func (r *Report) Skip(reason string) {
	if r.skipped == nil {
		r.skipped = map[string]int{}
	}
	if r.skipped[reason] == 0 {
		r.order = append(r.order, reason)
	}
	r.skipped[reason]++
}

// Note records a sentence to show as it is.
func (r *Report) Note(sentence string) { r.Skip("\x00" + sentence) }

// Warnings are the report as sentences.
func (r *Report) Warnings() []string {
	out := make([]string, 0, len(r.order))
	for _, reason := range r.order {
		if strings.HasPrefix(reason, "\x00") {
			out = append(out, reason[1:])
			continue
		}
		out = append(out, fmt.Sprintf("%d × %s not carried over.", r.skipped[reason], reason))
	}
	return out
}

// ---- formats ---------------------------------------------------------

// ExportKind is what an export holds.
type ExportKind string

const (
	ExportCollection  ExportKind = "collection"
	ExportEnvironment ExportKind = "environment"
	ExportGlobals     ExportKind = "globals"
)

// FormatInfo describes a format to the window.
type FormatInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Extension is the file extension an export gets, without the dot.
	Extension string `json:"extension"`
	CanImport bool   `json:"canImport"`
	// Exports lists what the format can write; empty when it cannot write.
	Exports []ExportKind `json:"exports"`
}

// Format is a file format the API client speaks. A format implements
// Decoder, Encoder or both.
type Format interface {
	Info() FormatInfo
}

// Decoder reads a file into a Bundle.
type Decoder interface {
	Format
	// Recognises is a cheap look at the bytes, for "detect the format".
	Recognises(data []byte) bool
	Decode(data []byte, report *Report) (Bundle, error)
}

// Encoder writes a Bundle into a file. The bundle holds exactly one
// collection, one environment, or the globals — the kind is passed so a
// format whose files differ by kind (Postman's) does not have to guess.
type Encoder interface {
	Format
	Encode(kind ExportKind, bundle Bundle, report *Report) ([]byte, error)
}

// formats is the registry, in the order the window lists them and detection
// tries them.
var formats = []Format{postmanFormat{}, curlFormat{}}

// Formats lists what is registered.
func Formats() []FormatInfo {
	out := make([]FormatInfo, 0, len(formats))
	for _, format := range formats {
		info := format.Info()
		_, info.CanImport = format.(Decoder)
		if _, ok := format.(Encoder); !ok {
			info.Exports = nil
		}
		out = append(out, info)
	}
	return out
}

func formatByID(id string) (Format, error) {
	for _, format := range formats {
		if format.Info().ID == id {
			return format, nil
		}
	}
	return nil, ErrUnknownFormat
}

// ---- import ----------------------------------------------------------

// ImportOptions are the person's answers to what a file cannot say.
type ImportOptions struct {
	// EnvTag is the tag for environments the file brings without one.
	EnvTag string `json:"envTag"`
}

// ImportResult says what an import made and what it could not carry over.
type ImportResult struct {
	Format        string `json:"format"`
	CollectionID  string `json:"collectionId,omitempty"`
	EnvironmentID string `json:"environmentId,omitempty"`
	Name          string `json:"name"`
	Folders       int    `json:"folders"`
	Requests      int    `json:"requests"`
	Variables     int    `json:"variables"`
	// Warnings are sentences, one per kind of thing left behind, with a count.
	Warnings []string `json:"warnings"`
}

// Import reads a file into the vault. formatID empty means detect it.
//
// Nothing in the file runs: a script is counted, not kept; a request is
// saved, not sent.
func (s *Service) Import(formatID, data string, opts ImportOptions) (ImportResult, error) {
	if len(data) > MaxImportBytes {
		return ImportResult{}, ErrInvalid
	}
	decoder, err := s.decoderFor(formatID, []byte(data))
	if err != nil {
		return ImportResult{}, err
	}
	var report Report
	bundle, err := decoder.Decode([]byte(data), &report)
	if err != nil {
		return ImportResult{}, err
	}
	// Checked before anything is written, so a refused import leaves nothing.
	for _, env := range bundle.Environments {
		if s.tagFor(env, opts) == "" {
			return ImportResult{}, ErrTagRequired
		}
	}
	result := ImportResult{Format: decoder.Info().ID}
	if err := s.materialize(bundle, opts, &result); err != nil {
		return result, err
	}
	result.Warnings = report.Warnings()
	return result, nil
}

func (s *Service) decoderFor(formatID string, data []byte) (Decoder, error) {
	if formatID != "" {
		format, err := formatByID(formatID)
		if err != nil {
			return nil, err
		}
		decoder, ok := format.(Decoder)
		if !ok {
			return nil, ErrFormatCannot
		}
		return decoder, nil
	}
	for _, format := range formats {
		if decoder, ok := format.(Decoder); ok && decoder.Recognises(data) {
			return decoder, nil
		}
	}
	return nil, ErrUnknownFormat
}

// tagFor is the env tag an imported environment goes under: the one the
// person chose, else the file's own if this machine knows it.
func (s *Service) tagFor(env BundleEnvironment, opts ImportOptions) string {
	if opts.EnvTag != "" {
		return opts.EnvTag
	}
	if env.Tag != "" && (s.envs == nil || s.envs.Exists(env.Tag)) {
		return env.Tag
	}
	return ""
}

func splitVars(vars []BundleVar) (plain, secrets map[string]string, count int) {
	plain, secrets = map[string]string{}, map[string]string{}
	for _, v := range vars {
		if v.Disabled || strings.TrimSpace(v.Key) == "" {
			continue
		}
		if v.Secret {
			secrets[v.Key] = v.Value
		} else {
			plain[v.Key] = v.Value
		}
		count++
	}
	return plain, secrets, count
}

func (s *Service) materialize(bundle Bundle, opts ImportOptions, result *ImportResult) error {
	for _, collection := range bundle.Collections {
		id, err := s.materializeCollection(collection, "", 0, result)
		if err != nil {
			return err
		}
		if result.CollectionID == "" {
			result.CollectionID, result.Name = id, collection.Name
		}
	}
	for _, env := range bundle.Environments {
		plain, secrets, count := splitVars(env.Vars)
		created, err := s.CreateEnvironment(nonEmpty(env.Name, "Imported"), s.tagFor(env, opts), plain, secrets)
		if err != nil {
			return err
		}
		result.Variables += count
		if result.EnvironmentID == "" {
			result.EnvironmentID = created.ID
		}
		if result.Name == "" {
			result.Name = created.Name
		}
	}
	if bundle.Globals != nil {
		// Merged into what is there: a globals file is somebody's globals,
		// not a reason to drop this machine's.
		current, err := s.Globals()
		if err != nil {
			return err
		}
		plain, secrets, count := splitVars(bundle.Globals)
		update := VarsUpdate{Vars: map[string]string{}, Secrets: secrets, KeepSecrets: current.SecretVars}
		for key, value := range current.Vars {
			update.Vars[key] = value
		}
		for key, value := range plain {
			update.Vars[key] = value
		}
		if _, err := s.SetGlobals(update); err != nil {
			return err
		}
		result.Variables += count
		if result.Name == "" {
			result.Name = "Globals"
		}
	}
	return nil
}

func (s *Service) materializeCollection(c BundleCollection, parentID string, order int, result *ImportResult) (string, error) {
	created, err := s.SaveCollection(Collection{Name: nonEmpty(c.Name, "Folder"), ParentID: parentID, Order: order})
	if err != nil {
		return "", err
	}
	if parentID != "" {
		result.Folders++
	}
	plain, secrets, count := splitVars(c.Vars)
	if count > 0 {
		if _, err := s.SetCollectionVariables(created.ID, VarsUpdate{Vars: plain, Secrets: secrets}); err != nil {
			return "", err
		}
		result.Variables += count
	}
	for i, folder := range c.Folders {
		if _, err := s.materializeCollection(folder, created.ID, i, result); err != nil {
			return "", err
		}
	}
	for _, br := range c.Requests {
		req := br.Request.Request()
		req.ID, req.OriginRef, req.CollectionID = "", "", created.ID
		req.Name = nonEmpty(req.Name, "Request")
		// An account means nothing on another vault; the placeholders it was
		// exported as come back as the header they stand for, so the request
		// still works once the variable is set — and binding an account to
		// it again is one click.
		req.Auth = Auth{}
		if br.Credential != nil {
			req.Headers = append(req.Headers, credentialHeaders(*br.Credential)...)
		}
		if _, err := s.SaveRequest(req); err != nil {
			return "", err
		}
		result.Requests++
	}
	return created.ID, nil
}

// credentialHeaders is the header a credential's placeholders stand for.
// Basic auth has none: base64 of a variable is not the variable's base64.
func credentialHeaders(c BundleCredential) KVList {
	switch c.Kind {
	case AuthBearer:
		return KVList{{Key: "Authorization", Value: "Bearer {{" + c.TokenVar + "}}"}}
	case AuthHeader:
		return KVList{{Key: nonEmpty(c.Header, "X-API-Key"), Value: "{{" + c.TokenVar + "}}"}}
	}
	return nil
}

// ---- export ----------------------------------------------------------

// ExportScope is what to export: one collection (with everything below it),
// one environment, or the globals.
type ExportScope struct {
	Kind ExportKind `json:"kind"`
	// ID is the collection or environment; empty for globals.
	ID string `json:"id"`
}

// ExportFile is an export, ready to save.
type ExportFile struct {
	FileName string `json:"fileName"`
	Content  string `json:"content"`
	// Warnings say what the file does not carry: secret values, accounts.
	Warnings []string `json:"warnings"`
}

// AccountNamer gives an account's display name, so an exported placeholder
// reads {{github_work_token}} rather than {{token}}. Optional: without it the
// placeholder is still unique, only less readable.
type AccountNamer interface {
	AccountLabel(id string) string
}

// Export writes one scope in a format.
//
// What never leaves, whatever the format: a secret variable's value (its name
// does, empty, so the person knows what to fill in), and an account's
// credential (the request names a variable in its place).
func (s *Service) Export(scope ExportScope, formatID string) (ExportFile, error) {
	format, err := formatByID(formatID)
	if err != nil {
		return ExportFile{}, err
	}
	encoder, ok := format.(Encoder)
	if !ok || !exports(format.Info(), scope.Kind) {
		return ExportFile{}, ErrFormatCannot
	}
	var report Report
	bundle, name, err := s.bundleFor(scope, &report)
	if err != nil {
		return ExportFile{}, err
	}
	content, err := encoder.Encode(scope.Kind, bundle, &report)
	if err != nil {
		return ExportFile{}, err
	}
	return ExportFile{
		FileName: fileNameFor(name, scope.Kind, format.Info().Extension),
		Content:  string(content),
		Warnings: report.Warnings(),
	}, nil
}

func exports(info FormatInfo, kind ExportKind) bool {
	for _, k := range info.Exports {
		if k == kind {
			return true
		}
	}
	return false
}

func (s *Service) bundleFor(scope ExportScope, report *Report) (Bundle, string, error) {
	secretsLeft := 0
	toVars := func(plain map[string]string, secretNames []string) []BundleVar {
		out := make([]BundleVar, 0, len(plain)+len(secretNames))
		for _, key := range sortedKeys(plain) {
			out = append(out, BundleVar{Key: key, Value: plain[key]})
		}
		for _, key := range secretNames {
			out = append(out, BundleVar{Key: key, Secret: true})
			secretsLeft++
		}
		return out
	}
	defer func() {
		if secretsLeft > 0 {
			report.Note(fmt.Sprintf("%d secret variable(s) are exported by name with an empty value — DevKit never writes a secret out. Fill them in where the file is opened.", secretsLeft))
		}
	}()

	switch scope.Kind {
	case ExportGlobals:
		globals, err := s.Globals()
		if err != nil {
			return Bundle{}, "", err
		}
		return Bundle{Globals: toVars(globals.Vars, globals.SecretVars)}, "Globals", nil

	case ExportEnvironment:
		envs, err := s.Environments()
		if err != nil {
			return Bundle{}, "", err
		}
		for _, env := range envs {
			if env.ID == scope.ID {
				return Bundle{Environments: []BundleEnvironment{{
					Name: env.Name, Tag: env.Env, Vars: toVars(env.Vars, env.SecretVars),
				}}}, env.Name, nil
			}
		}
		return Bundle{}, "", ErrNotFound

	case ExportCollection:
		collections, err := s.Collections()
		if err != nil {
			return Bundle{}, "", err
		}
		summaries, err := s.Requests()
		if err != nil {
			return Bundle{}, "", err
		}
		byParent := map[string][]Collection{}
		var root *Collection
		for i := range collections {
			byParent[collections[i].ParentID] = append(byParent[collections[i].ParentID], collections[i])
			if collections[i].ID == scope.ID {
				root = &collections[i]
			}
		}
		if root == nil {
			return Bundle{}, "", ErrNotFound
		}
		requestsIn := map[string][]Request{}
		for _, summary := range summaries {
			requestsIn[summary.CollectionID] = append(requestsIn[summary.CollectionID], summary)
		}
		names := credentialNames{taken: map[string]bool{}}
		seen := map[string]bool{}
		var build func(c Collection, depth int) (BundleCollection, error)
		build = func(c Collection, depth int) (BundleCollection, error) {
			seen[c.ID] = true
			out := BundleCollection{Name: c.Name, Vars: toVars(c.Vars, c.SecretVars)}
			for _, summary := range requestsIn[c.ID] {
				full, err := s.Request(summary.ID)
				if err != nil {
					return out, err
				}
				br := BundleRequest{Request: full.input()}
				br.Request.ID, br.Request.CollectionID, br.Request.OriginRef = "", "", ""
				if full.Auth.Kind != AuthNone && full.Auth.AccountID != "" {
					br.Credential = names.credential(full.Auth, s.accountLabel(full.Auth.AccountID))
					br.Request.Auth = Auth{}
				}
				out.Requests = append(out.Requests, br)
			}
			if depth < maxCollectionDepth {
				for _, child := range byParent[c.ID] {
					if seen[child.ID] {
						continue
					}
					folder, err := build(child, depth+1)
					if err != nil {
						return out, err
					}
					out.Folders = append(out.Folders, folder)
				}
			}
			return out, nil
		}
		tree, err := build(*root, 0)
		if err != nil {
			return Bundle{}, "", err
		}
		if len(names.taken) > 0 {
			report.Note(fmt.Sprintf("Requests that authenticate as a DevKit account are exported with a variable in the credential's place (%s). Set it where the file is opened.", strings.Join(names.list(), ", ")))
		}
		return Bundle{Collections: []BundleCollection{tree}}, root.Name, nil
	}
	return Bundle{}, "", ErrInvalid
}

func (s *Service) accountLabel(id string) string {
	if namer, ok := s.creds.(AccountNamer); ok && namer != nil {
		return namer.AccountLabel(id)
	}
	return ""
}

// credentialNames hands out placeholder names, one set per account, unique
// within the export.
type credentialNames struct {
	byAccount map[string]BundleCredential
	taken     map[string]bool
}

func (n *credentialNames) credential(auth Auth, label string) *BundleCredential {
	if n.byAccount == nil {
		n.byAccount = map[string]BundleCredential{}
	}
	key := auth.AccountID + "\x00" + string(auth.Kind)
	if existing, ok := n.byAccount[key]; ok {
		// One account, one set of names; the header it goes in is the
		// request's own.
		existing.Header = auth.Header
		return &existing
	}
	base := slug(label)
	if base == "" {
		base = "devkit"
	}
	unique := func(suffix string) string {
		name := base + "_" + suffix
		for i := 2; n.taken[name]; i++ {
			name = fmt.Sprintf("%s_%s_%d", base, suffix, i)
		}
		n.taken[name] = true
		return name
	}
	c := BundleCredential{Kind: auth.Kind, Header: auth.Header}
	switch auth.Kind {
	case AuthBasic:
		c.UserVar, c.PasswordVar = unique("user"), unique("password")
	case AuthHeader:
		c.TokenVar = unique("key")
	default:
		c.TokenVar = unique("token")
	}
	n.byAccount[key] = c
	return &c
}

func (n *credentialNames) list() []string {
	out := make([]string, 0, len(n.taken))
	for name := range n.taken {
		out = append(out, "{{"+name+"}}")
	}
	sort.Strings(out)
	return out
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// slug is a name fit for a variable and a shell: lowercase, underscores.
func slug(s string) string {
	return strings.Trim(nonWord.ReplaceAllString(strings.ToLower(s), "_"), "_")
}

// fileNameFor is the name a save dialog suggests: the thing's own name, the
// kind when that is not obvious, the format's extension.
func fileNameFor(name string, kind ExportKind, extension string) string {
	base := strings.Trim(regexp.MustCompile(`[^A-Za-z0-9._ -]+`).ReplaceAllString(name, ""), " .")
	if base == "" || !utf8.ValidString(base) {
		base = "export"
	}
	switch kind {
	case ExportEnvironment:
		base += ".environment"
	case ExportGlobals:
		base = "globals"
	}
	return base + "." + extension
}

// input is a saved request as an editor would hold it.
func (r Request) input() RequestInput {
	return RequestInput{
		ID: r.ID, CollectionID: r.CollectionID, Name: r.Name, Method: r.Method, URL: r.URL,
		Query: r.Query, Headers: r.Headers, PathVars: r.PathVars, BodyMode: r.BodyMode,
		Body: r.Body, Form: r.Form, Auth: r.Auth, OriginRef: r.OriginRef,
	}
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
