package agenthub

import (
	"fmt"
	"strings"
)

// workspaceTurnContext deliberately does not render tool placeholders: workspace
// instructions are user-authored text, not part of DevKit's prompt template.
func (s *Service) workspaceTurnContext(sess AgentSession) (string, error) {
	if s.WorkspaceInstructions == nil && s.SessionBrief == nil {
		return "", nil
	}
	instructions := ""
	if sess.WorkspaceID != "" && s.WorkspaceInstructions != nil {
		var err error
		instructions, err = s.WorkspaceInstructions(sess.WorkspaceID)
		if err != nil {
			return "", fmt.Errorf("read workspace instructions: %w", err)
		}
	}
	var b strings.Builder
	b.WriteString("# Current DevKit workspace context\n\nThis context supersedes all earlier workspace instructions and session briefs. DevKit rules still take precedence.\n\n## Workspace instructions\n\n")
	if sess.WorkspaceID == "" {
		b.WriteString("No workspace is selected. No workspace instructions apply; retire any previously supplied workspace instructions.\n")
	} else if strings.TrimSpace(instructions) == "" {
		b.WriteString("No workspace instructions are configured. Retire any previously supplied workspace instructions.\n")
	} else {
		b.WriteString(instructions)
		b.WriteByte('\n')
	}
	b.WriteString("\n## Current session brief\n\n")
	brief := ""
	if s.SessionBrief != nil {
		brief = s.SessionBrief(sess.WorkspaceID, sess.ProjectPath, append([]string{sess.ProjectPath}, sess.ExtraReadWritePaths...))
	}
	if strings.TrimSpace(brief) == "" {
		b.WriteString("No current session brief is available. Use DevKit's workspace.list tool to discover the current folders and grants.\n")
	} else {
		b.WriteString(brief)
	}
	return b.String(), nil
}
