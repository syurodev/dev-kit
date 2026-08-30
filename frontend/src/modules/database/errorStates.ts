/**
 * Which designed state an error message belongs to.
 *
 * The bridge hands back a message, not a typed error, so this is string
 * matching — and it was already being done inline in two views with slightly
 * different spellings. One place, so a wording change in Go breaks one test
 * rather than silently dropping a state in half the module.
 */

const VAULT_LOCKED = ["vault locked", "locked vault", "vault is locked"];

export function isVaultLockedError(message: string): boolean {
  const text = message.toLowerCase();
  return VAULT_LOCKED.some((phrase) => text.includes(phrase));
}

/**
 * ErrInsecureTLSForRemoteHost — "insecure TLS is only allowed for loopback
 * database hosts". The connection is refused before it is attempted, which is
 * why this deserves its own state instead of a driver error nobody can act on.
 */
export function isInsecureTLSError(message: string): boolean {
  const text = message.toLowerCase();
  return text.includes("insecure tls") && text.includes("loopback");
}
