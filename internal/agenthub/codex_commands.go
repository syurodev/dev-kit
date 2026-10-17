package agenthub

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"devkit/internal/acp"
	"devkit/internal/codexapp"
)

// Session commands for a Codex session.
//
// Claude announces its own commands (available_commands_update) and runs them
// itself. `codex app-server` does neither: /compact, /review and the rest are
// features of Codex's terminal UI, so a prompt that reads "/compact" would go to
// the model as a sentence. DevKit therefore announces the commands it
// implements, in the same wire shape Claude's arrive in, and runs a prompt that
// is exactly one of them through the RPC behind it instead of turn/start.
//
// A command is one entry in codexCommands plus its prepare function. What each
// RPC does was measured with the real app-server 0.163.0; AGENTS.md, "Codex
// session commands", has the table. Only commands that were measured are listed:
// /goal, /rename, /fork, /undo and the shell command are deliberately not.

// codexCommand is one slash command.
type codexCommand struct {
	// Name is how the composer sends it, without the slash.
	Name        string
	Description string
	// Hint is shown after the name when the command takes text; empty when it
	// takes none, in which case text after the name is refused.
	Hint string
	// prepare reads the text after the name and returns the call to make.
	prepare func(args string) (codexTurnCall, error)
}

// codexTurnCall is how a turn is started: the RPC, and what its answer says.
type codexTurnCall struct {
	// stage names the RPC in logs and errors.
	stage string
	// adopt says the answer does not name the turn that runs — thread/compact/start
	// answers `{}` — so the runtime takes the id from the next turn/started.
	adopt bool
	// command marks an RPC the person asked for by name: an app-server refusal
	// is the vendor declining this one request, not a session that broke.
	command bool
	// do makes the call and returns the turn id the answer names, if it does.
	do func(ctx context.Context, rt *codexRuntime, thread string) (turnID string, err error)
	// after runs once the turn ended well.
	after func(rt *codexRuntime)
}

var codexCommands = []codexCommand{
	{
		Name:        "compact",
		Description: "Summarize the conversation to free up context",
		prepare:     prepareCodexCompact,
	},
	{
		Name:        "review",
		Description: "Review code changes — your uncommitted changes unless you say otherwise",
		Hint:        "[base <branch> | commit <sha> | what to look for]",
		prepare:     prepareCodexReview,
	},
}

// codexAvailableCommands is the available_commands_update for a Codex session.
func codexAvailableCommands() []AvailableCommand {
	out := make([]AvailableCommand, 0, len(codexCommands))
	for _, c := range codexCommands {
		command := AvailableCommand{Name: c.Name, Description: c.Description}
		if c.Hint != "" {
			command.Input = &AvailableCommandInput{Hint: c.Hint}
		}
		out = append(out, command)
	}
	return out
}

// codexCommandsUpdate is the update that announces them.
func codexCommandsUpdate() []byte {
	return marshalUpdate(map[string]any{
		"sessionUpdate":     string(UpdateAvailableCommands),
		"availableCommands": codexAvailableCommands(),
	})
}

// codexCommandOf reads a prompt that is a command: one text block, and nothing
// else, that begins with /name for a command in the table. A prompt that only
// contains a slash, names another command, or carries an attachment is an
// ordinary message and is left alone.
func codexCommandOf(prompt []acp.ContentBlock) (cmd *codexCommand, args string, ok bool) {
	name, rest, ok := slashCommandOf(prompt)
	if !ok {
		return nil, "", false
	}
	for i := range codexCommands {
		if codexCommands[i].Name == name {
			return &codexCommands[i], rest, true
		}
	}
	return nil, "", false
}

// slashCommandOf is the shared reading of "this prompt is a slash command":
// exactly one text block whose first word is /name (letters, digits, `-`, `_`,
// `:`; not a path), returning the lower-cased name and the text after it.
// Used by the Codex runtime to run the command and by Service to leave a
// command prompt unwrapped.
func slashCommandOf(prompt []acp.ContentBlock) (name, args string, ok bool) {
	if len(prompt) != 1 || (prompt[0].Type != "text" && prompt[0].Type != "") {
		return "", "", false
	}
	text := strings.TrimSpace(prompt[0].Text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	word, rest := text, ""
	if i := strings.IndexFunc(text, unicode.IsSpace); i >= 0 {
		word, rest = text[:i], text[i:]
	}
	if !slashWord.MatchString(word) {
		return "", "", false
	}
	return strings.ToLower(word[1:]), strings.TrimSpace(rest), true
}

var slashWord = regexp.MustCompile(`^/[A-Za-z][A-Za-z0-9_:-]*$`)

func prepareCodexCompact(args string) (codexTurnCall, error) {
	if args != "" {
		// thread/compact/start takes the thread and nothing else: there is no
		// way to pass instructions, and sending the line to the model would
		// answer a command with a paragraph.
		return codexTurnCall{}, fmt.Errorf("%w: /compact takes no text in Codex sessions", ErrInvalidPrompt)
	}
	return codexTurnCall{
		stage:   codexapp.MethodThreadCompactStart,
		adopt:   true,
		command: true,
		do: func(ctx context.Context, rt *codexRuntime, thread string) (string, error) {
			return "", rt.conn.Call(ctx, codexapp.MethodThreadCompactStart, codexapp.ThreadCompactStartParams{ThreadID: thread}, nil)
		},
		after: (*codexRuntime).republishUsage,
	}, nil
}

// codexRefPattern keeps a branch or commit named in /review to characters a
// git ref is written with: Codex puts the name into the review prompt, where
// the model runs it as part of a shell command.
var codexRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@~^-]*$`)
var codexCommitPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// codexReviewTarget reads /review's text. Nothing: the working tree.
// `base <branch>` and `commit <sha>` — exactly two words — name a branch or a
// commit; anything else is the person's own instructions, so a sentence that
// happens to start with "base" is not read as a branch.
func codexReviewTarget(args string) (codexapp.ReviewTarget, error) {
	words := strings.Fields(args)
	switch {
	case len(words) == 0:
		return codexapp.ReviewTarget{Type: codexapp.ReviewUncommitted}, nil
	case len(words) == 2 && strings.EqualFold(words[0], "base"):
		if !codexRefPattern.MatchString(words[1]) {
			return codexapp.ReviewTarget{}, fmt.Errorf("%w: %q is not a branch name", ErrInvalidPrompt, words[1])
		}
		return codexapp.ReviewTarget{Type: codexapp.ReviewBaseBranch, Branch: words[1]}, nil
	case len(words) == 2 && strings.EqualFold(words[0], "commit") && codexCommitPattern.MatchString(words[1]):
		return codexapp.ReviewTarget{Type: codexapp.ReviewCommit, SHA: words[1]}, nil
	}
	return codexapp.ReviewTarget{Type: codexapp.ReviewCustom, Instructions: args}, nil
}

func prepareCodexReview(args string) (codexTurnCall, error) {
	target, err := codexReviewTarget(args)
	if err != nil {
		return codexTurnCall{}, err
	}
	return codexTurnCall{
		stage:   codexapp.MethodReviewStart,
		command: true,
		do: func(ctx context.Context, rt *codexRuntime, thread string) (string, error) {
			var res codexapp.ReviewStartResponse
			err := rt.conn.Call(ctx, codexapp.MethodReviewStart, codexapp.ReviewStartParams{
				ThreadID: thread, Target: target, Delivery: codexapp.ReviewDeliveryInline,
			}, &res)
			return res.Turn.ID, err
		},
	}, nil
}
