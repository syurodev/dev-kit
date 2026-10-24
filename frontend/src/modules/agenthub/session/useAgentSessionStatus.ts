import { useState } from "react";
import {
  isAgentApiKeyError,
  isAgentAuthRequiredError,
  isAgentNotInstalledError,
  isRuntimeRequirementError,
} from "../../../bridge/agentRuntime";
import { toAppError } from "../../../bridge/errors";
import type { SessionErrorCause } from "../surfaces";
import type { AuthRetry } from "./sessionModel";

/**
 * What a tab says about the work it is doing right now: whether a turn is in
 * flight, why the last one failed, and what to repeat once the agent is signed
 * in. The draft and the turn pipeline both report through it, so one hook owns
 * the words rather than each keeping a copy.
 */
export function useAgentSessionStatus() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Figma 948:2204 writes the reason as "agenthub.processFailed — the agent
  // exited after …": the DevKit code AND what happened. errorMessage() gives
  // only the humanised half, so the card was saying "Agent process failed."
  // — true, and no more actionable than the red dot the note complains about.
  const [errorReason, setErrorReason] = useState("");
  // Not installed / missing Node are not crashes; the card offers Settings.
  const [errorCause, setErrorCause] = useState<SessionErrorCause | undefined>(undefined);
  const [sandboxHint, setSandboxHint] = useState("");
  // What to do again once the agent is signed in. A failed create keeps its
  // params here while the draft shows the sign-in instead of sending.
  const [authRetry, setAuthRetry] = useState<AuthRetry | null>(null);

  /** Everything a new draft or a new attempt forgets. */
  function resetAll() {
    setError("");
    setErrorReason("");
    setErrorCause(undefined);
    setSandboxHint("");
    setAuthRetry(null);
  }

  function reportError(e: unknown) {
    const app = toAppError(e);
    setError(app.message);
    setErrorReason(app.code === "unknown" ? app.message : `${app.code} — ${app.message}`);
    setErrorCause(
      isAgentNotInstalledError(app)
        ? "notInstalled"
        : isRuntimeRequirementError(app)
          ? "requirement"
          : isAgentAuthRequiredError(app)
            ? "authRequired"
            : isAgentApiKeyError(app)
              ? "apiKey"
              : undefined,
    );
  }

  return {
    busy,
    setBusy,
    error,
    setError,
    errorReason,
    errorCause,
    setErrorCause,
    sandboxHint,
    setSandboxHint,
    authRetry,
    setAuthRetry,
    resetAll,
    reportError,
  };
}

export type AgentSessionStatus = ReturnType<typeof useAgentSessionStatus>;
