package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"

	"devkit/internal/agenthub"
)

const maxHookPayloadBytes = 4 << 20

// runPreToolUseHook is the PreToolUse hook Agent Hub installs into every Claude
// session. It reads one hook payload on stdin and writes one decision on stdout.
//
// Why a hook and not the permission gate: the gate (ACP session/request_permission,
// the SDK's canUseTool) is the LAST step of Claude Code's evaluation order —
// hooks, deny rules, ask rules, permission mode, allow rules, then the gate.
// Anything approved earlier never reaches it. Hooks run before all of it, and a
// hook deny holds even in bypassPermissions mode.
//
// It denies one thing and defers everything else. The jail binds ~/.claude and
// ~/.claude.json read-write because Claude Code needs its own credentials to
// authenticate at all (see claudeCredentialPaths). That grant is for the
// process, not for the model: an agent that can ask a tool to read its own
// OAuth token can put it in a transcript, and DevKit stores transcripts. The
// process keeps its access; the tool surface does not.
//
// Valid, unrelated inputs return an empty decision so the normal flow —
// including DevKit's own permission gate — still runs. Inputs the hook cannot
// safely inspect are denied because the hook is the only check before the
// vendor's earlier allow rules. This hook is defense-in-depth, not a complete
// security boundary against arbitrary command/path construction.
func runPreToolUseHook() error {
	// Read one byte beyond the limit so an exact-size payload is accepted but a
	// truncated or oversized payload is never mistaken for a complete one.
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, maxHookPayloadBytes+1))
	if err != nil {
		return emitHookDecision(hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (payload read failed); the tool call was denied."))
	}
	return emitHookDecision(preToolUseDecision(raw))
}

func preToolUseDecision(raw []byte) map[string]any {
	if len(raw) > maxHookPayloadBytes {
		return hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (payload too large); the tool call was denied.")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (malformed JSON); the tool call was denied.")
	}

	toolNameRaw, hasToolName := fields["tool_name"]
	toolInputRaw, hasToolInput := fields["tool_input"]
	if !hasToolName || !hasToolInput || bytes.Equal(bytes.TrimSpace(toolInputRaw), []byte("null")) {
		return hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (missing required fields); the tool call was denied.")
	}

	var toolName string
	var toolInput map[string]any
	if err := json.Unmarshal(toolNameRaw, &toolName); err != nil || strings.TrimSpace(toolName) == "" {
		return hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (invalid tool_name); the tool call was denied.")
	}
	if err := json.Unmarshal(toolInputRaw, &toolInput); err != nil || toolInput == nil {
		return hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (invalid tool_input); the tool call was denied.")
	}

	// Claude Code writes a result too large for the context to this session's
	// tool-results directory and tells the agent to read it back. That is the
	// one place in ~/.claude a reading tool may reach; see sessionToolResultsDir.
	spill := ""
	switch toolName {
	case "Read", "Grep", "Glob":
		spill = sessionToolResultsDir(fields)
	case "Bash":
		// A shell can write and link as well as read, so it gets the directory
		// only for a command that cannot do either; see readOnlyShell.
		if command, _ := toolInput["command"].(string); readOnlyShell(command) {
			spill = sessionToolResultsDir(fields)
		}
	}
	if hit := protectedPathHitIn(toolName, toolInput, spill); hit != "" {
		return hookDenyDecision(agenthub.HookDenyReason + " (" + hit + "). The agent process authenticates with them; tools may not read them.")
	}
	if sub := shellGitWrite(toolName, toolInput); sub != "" {
		return hookDenyDecision(gitRedirects[sub])
	}
	return nil
}

// gitRedirects is what the agent is told instead of running a git write in
// the shell. A deny reason is read by the model as the tool's result, so it
// says what to call rather than only what was refused.
var gitRedirects = map[string]string{
	"commit": "DevKit runs git commits itself: stage with mcp__devkit__git_stage and commit with " +
		"mcp__devkit__git_commit (pass `repo` for another service; mcp__devkit__git_repos lists them). " +
		"To change the last commit use mcp__devkit__git_amend; to fold several into one, mcp__devkit__git_squash. " +
		"The shell skips the branch guards and the task trailer DevKit applies.",
	"push": "DevKit pushes for you: use mcp__devkit__git_push. It pushes a branch recorded on a task you " +
		"hold (mcp__devkit__tasks_setFields) and asks the user where the remote requires it — the shell skips both.",
	"pull": "DevKit pulls for you: use mcp__devkit__git_pull (fast-forward only; mcp__devkit__git_fetch just " +
		"updates what the remote has). It authenticates with the account bound to the remote, which the shell " +
		"does not have, and never starts a merge behind the user's back.",
	"rebase": "DevKit rebases for you: use mcp__devkit__git_rebase (action start with `onto`, then continue or " +
		"abort after a conflict; resolve with mcp__devkit__git_resolve). It replays only commits no remote has, " +
		"since DevKit never force-pushes, and it honours the branch guards the shell skips.",
	"cherry-pick": "DevKit cherry-picks for you: use mcp__devkit__git_cherryPick (commits oldest first; continue or " +
		"abort after a conflict). It commits as the account's identity under the branch guards the shell skips.",
	"revert": "DevKit reverts for you: use mcp__devkit__git_revert (commits newest first; continue or abort after a " +
		"conflict). It commits as the account's identity under the branch guards the shell skips.",
}

// shellGitWrite reports which git write a shell command runs — one of the
// keys of gitRedirects — or "".
//
// These are the ones DevKit's prompt says go through its tools, and the
// prompt alone did not hold: a real session committed with `git commit`
// against it. They are also the ones whose rules a shell bypasses — a guarded
// branch's confirmation, the task trailer, the push-only-your-task's-branch
// rule, rewriting only history no remote has — so refusing them here is
// enforcing those rules, not a preference.
// Everything else git does (status, diff, log, add, stash…) stays the
// agent's to run as it likes.
//
// It reads the command the way a person skimming it would: split into simple
// commands at the shell's separators, each one's leading environment
// assignments and wrappers (sudo, env, time…) dropped, git's own global
// options skipped to reach the subcommand. It is not a shell parser and does
// not try to be — `sh -c "git commit"` or an alias gets through, and the
// prompt and DevKit's tools are the rule; this is the backstop for the plain
// spelling, which is what an agent actually writes.
func shellGitWrite(toolName string, input map[string]any) string {
	if toolName != "Bash" {
		return ""
	}
	command, _ := input["command"].(string)
	for _, segment := range shellSegments(command) {
		if sub := gitSubcommand(strings.Fields(segment)); gitRedirects[sub] != "" {
			return sub
		}
	}
	return ""
}

// shellSegments splits a command line at ; && || | & newlines and the openings
// of subshells and command substitutions.
func shellSegments(command string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n", "&", "\n",
		"$(", "\n", "(", "\n", ")", "\n", "`", "\n", "{", "\n", "}", "\n")
	return strings.Split(replacer.Replace(command), "\n")
}

var shellWrappers = map[string]bool{"sudo": true, "env": true, "command": true, "exec": true, "time": true, "nohup": true, "then": true, "do": true, "else": true}

// gitGlobalOptionsWithValue take the next word as their value.
var gitGlobalOptionsWithValue = map[string]bool{"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true, "--exec-path": true}

func gitSubcommand(words []string) string {
	word := func(i int) string { return strings.Trim(words[i], `"'`) }
	i := 0
	// VAR=value and wrappers before the command itself.
	for i < len(words) && (shellWrappers[word(i)] || (strings.Contains(word(i), "=") && !strings.HasPrefix(word(i), "-"))) {
		i++
	}
	if i == len(words) || filepath.Base(word(i)) != "git" {
		return ""
	}
	for i++; i < len(words); i++ {
		switch w := word(i); {
		case gitGlobalOptionsWithValue[w]:
			i++
		case strings.HasPrefix(w, "-"):
		default:
			return w
		}
	}
	return ""
}

func hookDenyDecision(reason string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	}
}

func currentUsername() (string, error) {
	if value := os.Getenv("USER"); value != "" {
		return value, nil
	}
	if current, err := user.Current(); err == nil {
		return current.Username, nil
	}
	return "", fmt.Errorf("current user unavailable")
}

func protectedPathHit(toolName string, input map[string]any) string {
	return protectedPathHitIn(toolName, input, "")
}

// protectedPathHitIn is protectedPathHit with one directory carved out: spill,
// the session's own tool-results directory, or "" for none.
func protectedPathHitIn(toolName string, input map[string]any, spill string) string {
	// Built-in tools have documented, stable path/command fields. Inspect only
	// those fields so source code, prompts, and replacement text may discuss
	// Claude configuration without being mistaken for an attempted read.
	var values []any
	switch toolName {
	case "Bash", "PowerShell":
		values = []any{input["command"]}
	case "Read", "Edit", "Write":
		values = []any{input["file_path"]}
	case "Glob", "Grep":
		values = []any{input["path"]}
	case "NotebookEdit":
		values = []any{input["notebook_path"]}
	case "WebFetch", "WebSearch", "Agent", "Workflow", "AskUserQuestion", "ExitPlanMode", "Skill":
		return ""
	default:
		// MCP and future tools have vendor-defined shapes. Keep the conservative
		// recursive behavior for those unknown surfaces until their semantics are
		// known; false positives there are preferable to silently exposing auth.
		values = []any{input}
	}

	var hit string
	var walk func(any)
	walk = func(v any) {
		if hit != "" {
			return
		}
		switch t := v.(type) {
		case string:
			if matched := protectedReferenceHit(t, spill); matched != "" {
				hit = matched
				return
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		case map[string]any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	for _, value := range values {
		walk(value)
	}
	return hit
}

// protectedReferenceHit recognizes direct paths and the common shell spellings
// Claude emits. It intentionally does not try to interpret arbitrary shell
// programs. Complete isolation requires keeping vendor credentials outside the
// tool process; this hook only reduces exposure while that is not yet possible.
func protectedReferenceHit(value, spill string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	home = filepath.ToSlash(filepath.Clean(home))

	for _, token := range referenceTokens(value) {
		if hit := credentialFileReference(token); hit != "" {
			return hit
		}
		expanded := expandHomeReferences(token, home)
		// Options such as --file=/Users/x/.claude.json retain the path in the
		// same shell word. Test each suffix beginning at the effective home.
		for offset := indexPath(expanded, home); offset >= 0; {
			candidate := strings.TrimRight(expanded[offset:], ",:]}\"")
			candidate = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(candidate, "\\", "/")))
			if isProtectedHomePath(candidate, home, spill) {
				return candidate
			}
			next := indexPath(expanded[offset+1:], home)
			if next < 0 {
				break
			}
			offset += next + 1
		}
	}
	return ""
}

func expandHomeReferences(value, home string) string {
	value = strings.NewReplacer(
		"${HOME}", home,
		"$HOME", home,
		"~/", home+"/",
	).Replace(value)
	if username, err := currentUsername(); err == nil && username != "" {
		parent := filepath.ToSlash(filepath.Dir(home))
		value = strings.NewReplacer(
			parent+"/${USER}", home,
			parent+"/$USER", home,
		).Replace(value)
	}
	return filepath.ToSlash(value)
}

func referenceTokens(value string) []string {
	// Quotes group shell words but do not become part of the resulting path.
	// Removing them also joins common concatenated spellings such as
	// "$HOME"/.claude.json before tokenization.
	value = strings.NewReplacer("\"", "", "'", "").Replace(value)
	return strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(";&|<>()`", r)
	})
}

func indexPath(value, path string) int {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.Index(strings.ToLower(value), strings.ToLower(path))
	}
	return strings.Index(value, path)
}

func isProtectedHomePath(candidate, home, spill string) bool {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		candidate = strings.ToLower(candidate)
		home = strings.ToLower(home)
		spill = strings.ToLower(spill)
	}
	dir := home + "/.claude"
	if candidate == home+"/.claude.json" {
		return true
	}
	if candidate != dir && !strings.HasPrefix(candidate, dir+"/") {
		return false
	}
	if spill != "" && (candidate == spill || strings.HasPrefix(candidate, spill+"/")) {
		return false
	}
	return !isAgentOwnedClaudePath(strings.TrimPrefix(candidate, dir))
}

// readOnlyShellCommands are the programs a command may run to count as a
// read of a spill file: ones that only read their arguments and write to
// stdout. Left out on purpose: sed -i, sort -o and uniq's second operand write
// files, and cd/ln/cp/mv/rm/tee/echo are not reads.
var readOnlyShellCommands = map[string]bool{
	"cat": true, "head": true, "tail": true, "wc": true, "cut": true, "tr": true, "grep": true, "jq": true,
}

// stderrRedirect is the one redirect a read-only command may carry.
var stderrRedirect = regexp.MustCompile(`\s2>(&1|/dev/null)(\s|$)`)

// readOnlyShell reports whether command is nothing but readOnlyShellCommands
// joined by | && || ; or newlines. It is deliberately narrow and fails closed:
// no redirects (but 2>&1 and 2>/dev/null), no variable or command expansion,
// no subshell, background job, backslash or environment prefix, and every
// quote must close. Quotes are honoured, so a jq program such as
// '.[] | select(.a > 1)' is text, not a redirect or a pipe.
//
// What it protects is the spill directory itself: with it, "read the
// tool-results file" cannot become "write into it" or "link out of it". It
// does not make the rest of the hook any stronger or weaker.
func readOnlyShell(command string) bool {
	command = " " + strings.TrimSpace(command)
	command = stderrRedirect.ReplaceAllString(command, " ")
	runes := []rune(command)

	var (
		words  []string
		word   strings.Builder
		inWord bool
		quote  rune
	)
	flush := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	endSegment := func() bool {
		flush()
		ok := len(words) > 0 && readOnlyShellCommands[filepath.Base(words[0])]
		words = nil
		return ok
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '`', '$', '\\':
				return false
			default:
				word.WriteRune(r)
			}
		default:
			switch r {
			case '\'', '"':
				quote, inWord = r, true
			case '`', '$', '(', ')', '{', '}', '<', '>', '\\':
				return false
			case ' ', '\t':
				flush()
			case '\n', ';':
				if !endSegment() {
					return false
				}
			case '|', '&':
				if next == r {
					i++
				} else if r == '&' {
					return false
				}
				if !endSegment() {
					return false
				}
			default:
				word.WriteRune(r)
				inWord = true
			}
		}
	}
	if quote != 0 {
		return false
	}
	return endSegment()
}

// sessionIDPattern is what a session id may look like before it is allowed
// into a path: no separators and no dots, so it cannot name anything but a
// single directory entry.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// sessionToolResultsDir is the one directory of ~/.claude a reading tool may
// reach: <home>/.claude/projects/<project>/<session>/tool-results, the place
// Claude Code spills a tool result too large for the context and then tells
// the agent to read. It holds what the session's own tools returned, not
// credentials, and is the same data the model already saw the head of.
//
// The directory is derived from the hook payload's session_id and
// transcript_path, which the harness writes, not the model; the model only
// controls tool_input. Anything that does not have exactly the expected shape
// yields "" and so grants nothing: the transcript must sit directly in a
// project directory of ~/.claude/projects and be named <session_id>.jsonl.
// That ties the grant to this session, not to every session or project.
func sessionToolResultsDir(fields map[string]json.RawMessage) string {
	var sessionID, transcript string
	if json.Unmarshal(fields["session_id"], &sessionID) != nil || json.Unmarshal(fields["transcript_path"], &transcript) != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !sessionIDPattern.MatchString(sessionID) || transcript == "" {
		return ""
	}
	home = filepath.ToSlash(filepath.Clean(home))
	transcript = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(transcript, "\\", "/")))

	projects := home + "/.claude/projects/"
	fold := func(s string) string { return s }
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		fold = strings.ToLower
	}
	if !strings.HasPrefix(fold(transcript), fold(projects)) {
		return ""
	}
	parts := strings.Split(transcript[len(projects):], "/")
	if len(parts) != 2 || parts[0] == "" || parts[0] == ".." || fold(parts[1]) != fold(sessionID+".jsonl") {
		return ""
	}
	return projects + parts[0] + "/" + sessionID + "/tool-results"
}

// isAgentOwnedClaudePath reports whether rel — a path inside ~/.claude,
// starting with "/" — is one the agent itself is meant to read and write:
//
//   - projects/<project>/memory/…, Claude Code's auto-memory. The agent
//     saves what it learns there with its own Write/Bash tools, and a
//     blanket block on ~/.claude made every save fail with this hook's
//     denial.
//   - CLAUDE.md, the user-level memory file the agent is asked to edit
//     ("remember this for every project").
//
// Neither holds credentials, and both are exact shapes — a path is cleaned
// before it gets here, so memory/../../../.credentials.json is no longer
// under memory by then. Everything else in ~/.claude stays protected:
// .credentials.json, settings.json (its env block can carry API keys, and a
// tool rewriting it could loosen the agent's own permission rules), and the
// other projects' transcripts.
func isAgentOwnedClaudePath(rel string) bool {
	claudeMemoryFile := "/CLAUDE.md"
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		rel = strings.ToLower(rel)
		claudeMemoryFile = "/claude.md"
	}
	if rel == claudeMemoryFile {
		return true
	}
	parts := strings.Split(strings.TrimPrefix(rel, "/"), "/")
	return len(parts) >= 3 && parts[0] == "projects" && parts[1] != "" && parts[2] == "memory"
}

// credentialFileReference catches the credential files by name when the path
// does not start at the home directory — a relative one the home-prefix
// check cannot see: "cat .claude/.credentials.json" run from $HOME, or
// "cat ../../../.credentials.json" run from inside a memory directory, which
// is reachable now that memory is allowed. .credentials.json is Claude
// Code's own token file and nothing else is called that; .claude.json only
// counts when relative, since an absolute one outside $HOME (a project's
// file of that name) is not the agent's.
func credentialFileReference(token string) string {
	token = strings.TrimRight(token, ",:]}\"")
	if i := strings.LastIndex(token, "="); i >= 0 {
		token = token[i+1:] // --file=.claude/.credentials.json
	}
	path := filepath.ToSlash(strings.ReplaceAll(token, "\\", "/"))
	base := path[strings.LastIndex(path, "/")+1:]
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		base = strings.ToLower(base)
	}
	relative := !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "~") && !strings.HasPrefix(path, "$")
	if base == ".credentials.json" || (relative && base == ".claude.json") {
		return path
	}
	return ""
}

// emitHookDecision writes the decision, or "{}" to defer. A deny is emitted
// as a normal successful hook response so Claude can apply it even when the
// payload itself was malformed or oversized.
func emitHookDecision(decision map[string]any) error {
	if decision == nil {
		_, err := os.Stdout.WriteString("{}\n")
		return err
	}
	body, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(body, '\n'))
	return err
}
