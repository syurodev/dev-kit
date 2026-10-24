import { OPENCODE_VENDOR } from "../../../bridge/agenthub";
import { vendorInfo } from "../vendors";
import { useClaudeSignIn } from "../ClaudeSignIn";
import { useCodexSignIn } from "../CodexSignIn";
import { useAgentDraft } from "./useAgentDraft";
import { useAgentHubEffects } from "./useAgentHubEffects";
import { useAgentSessionActions } from "./useAgentSessionActions";
import { useAgentSessionCreate } from "./useAgentSessionCreate";
import { useAgentSessionReads } from "./useAgentSessionReads";
import { useAgentSessionStatus } from "./useAgentSessionStatus";

/**
 * One Agent Hub tab, as the surface needs it: what it reads, the draft it may
 * be writing, the actions on the session, the effects the tab runs while it is
 * on screen, and the two ways a vendor signs in.
 *
 * The order is the dependency order — status first (the draft and the turn
 * pipeline both report through it), then the draft, then the reads that need to
 * know whether a draft is open, then what changes a session, then the sign-in
 * hooks that retry an action once a login lands, then the effects.
 */
export function useAgentHubController(tabSessionId: string) {
  const status = useAgentSessionStatus();
  const draft = useAgentDraft({ status, tabSessionId });
  const reads = useAgentSessionReads(tabSessionId, draft.draft !== null);
  const { handleCreateSession } = useAgentSessionCreate({ status, draft });
  const actions = useAgentSessionActions({ reads, draft, status });

  /** The action that was waiting on a sign-in, carried on once it lands. */
  const retryAfterSignIn = (outcome: { signedIn: boolean }) => {
    const retry = status.authRetry;
    if (retry?.kind === "create") {
      // The draft is still on screen with its message, so a successful login
      // carries straight on: the session is created and the message sent.
      if (outcome.signedIn) void handleCreateSession(retry.params);
    } else if (retry?.kind === "turn" && outcome.signedIn) {
      void actions.handleSend(retry);
    }
  };
  // One per way of signing in (vendors.ts): Claude's runs its login in a
  // terminal, Codex's inside DevKit. Both retry the same way.
  const claudeSignIn = useClaudeSignIn(retryAfterSignIn);
  const codexSignIn = useCodexSignIn(retryAfterSignIn);
  // opencode's sign-in methods are listed by starting opencode, so they are only
  // asked for once opencode is what the person is working with (the draft's
  // agent, this tab's session, or a create waiting on a sign-in).
  const openCodeSignIn = useClaudeSignIn(
    retryAfterSignIn,
    OPENCODE_VENDOR,
    (draft.draft !== null && draft.draftAgent === OPENCODE_VENDOR) ||
      reads.activeSession?.agentType === OPENCODE_VENDOR ||
      (status.authRetry?.kind === "create" && status.authRetry.params.vendor === OPENCODE_VENDOR),
  );
  /** The terminal sign-in controller of a vendor that signs in that way. */
  const terminalSignIn = (vendor: string | undefined) => (vendor === OPENCODE_VENDOR ? openCodeSignIn : claudeSignIn);

  /** A failure's hint, each mapped to the thing this app already does for it. */
  function handleFailureAction(action: string) {
    if (action === "resume") { void actions.handleResumeSession(); return; }
    if (action === "retry") {
      const retry = actions.retryTurn();
      if (retry) void actions.handleSend(retry);
      return;
    }
    if (action === "new_session") {
      draft.newSession();
      return;
    }
    if (action === "login") {
      const terminal = terminalSignIn(reads.activeSession?.agentType);
      const methods = terminal.methods.data ?? [];
      // One way in: take it. Several: the error card's panel names them, and
      // picking for the user would be guessing. An agent that signs in
      // inside DevKit shows its panel, where the browser is the first button.
      if (vendorInfo(reads.activeSession?.agentType).signIn === "terminal" && methods.length === 1) {
        terminal.start(methods[0]);
      } else {
        status.setErrorCause("authRequired");
      }
    }
  }

  const { focusComposerRequest } = useAgentHubEffects({ tabSessionId, reads, draft, actions });

  return {
    tabSessionId,
    status,
    draft,
    reads,
    create: handleCreateSession,
    actions,
    claudeSignIn,
    codexSignIn,
    terminalSignIn,
    handleFailureAction,
    focusComposerRequest,
  };
}

export type AgentHubController = ReturnType<typeof useAgentHubController>;
