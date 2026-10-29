/** The react-query key of one Environment report: a vendor's setup, or a session's own. */
export function agentEnvironmentKey(vendor: string, sessionId = ""): readonly string[] {
  return ["agenthub", "environment", vendor, sessionId];
}

/** Matches every Environment report of one session, whatever its vendor. */
export function isSessionEnvironmentKey(queryKey: readonly unknown[], sessionId: string): boolean {
  return queryKey[0] === "agenthub" && queryKey[1] === "environment" && queryKey[3] === sessionId;
}
