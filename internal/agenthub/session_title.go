package agenthub

import (
	"context"
	"strings"
	"unicode/utf8"
)

// A title for a session whose agent never names it.
//
// Claude Code names its session after the first exchange (session_info_update).
// Codex's app-server does not — its thread "title" is the first message, which
// for DevKit is the workspace context block — and opencode's ACP was never seen
// to send one (measured on the real sessions: none of the Codex or opencode
// sessions of the person's database had a title that the agent chose). Those
// rows read "Session 43ceeb81" for ever. DevKit therefore derives one from what
// the person asked, once, and only while the row has none: it never replaces a
// name an agent or the person chose (AdoptSessionTitle checks in the same
// statement), and a vendor that names its sessions is left to.

const sessionTitleRunes = 60

// vendorNamesSessions are the vendors that send a title of their own.
func vendorNamesSessions(t AgentType) bool { return t == AgentTypeClaude }

// titleFromPrompt reads a short title off the first line that says something.
// A slash command or a path ("/compact", "/etc/hosts") says nothing about the
// work, so it gives none.
func titleFromPrompt(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "#>*-` \t"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") || strings.HasPrefix(line, "@") {
			return ""
		}
		line = strings.Join(strings.Fields(line), " ")
		if utf8.RuneCountInString(line) <= sessionTitleRunes {
			return line
		}
		cut := string([]rune(line)[:sessionTitleRunes])
		// End at a word, not in the middle of one, when a space is near the end.
		if i := strings.LastIndex(cut, " "); i > len(cut)*2/3 {
			cut = cut[:i]
		}
		return strings.TrimRight(cut, " ,.;:-") + "…"
	}
	return ""
}

// nameFromPrompt gives a session with no title one from its first prompt.
func (s *Service) nameFromPrompt(ctx context.Context, sess AgentSession, prompt string) {
	if vendorNamesSessions(sess.AgentType) || strings.TrimSpace(derefString(sess.Title)) != "" {
		return
	}
	title := titleFromPrompt(prompt)
	if title == "" {
		return
	}
	if err := s.adoptSessionTitle(ctx, sess.ID, title); err != nil && s.OnRecordError != nil {
		s.OnRecordError(sess.ID, err)
	}
}

// BackfillSessionTitles names the sessions that predate nameFromPrompt, from
// the first thing the person asked in each. Startup only. The row's time is
// kept, so the rail does not reorder itself.
func (s *Service) BackfillSessionTitles(ctx context.Context) error {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return err
	}
	for _, sess := range list {
		if vendorNamesSessions(sess.AgentType) || strings.TrimSpace(derefString(sess.Title)) != "" {
			continue
		}
		entry, found, err := s.store.FindTranscriptKind(ctx, sess.ID, UpdateUserMessageChunk)
		if err != nil {
			return err
		}
		// A message from another session says nothing about what this one is for.
		if !found || strings.Contains(string(entry.Payload), `"source"`) {
			continue
		}
		if title := titleFromPrompt(entry.Text); title != "" {
			if err := s.store.AdoptSessionTitle(ctx, sess.ID, title, sess.UpdatedAt); err != nil {
				return err
			}
		}
	}
	return nil
}
