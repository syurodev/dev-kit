import { isLocalAccount, type Account, type AccountProvider } from "@/bridge/accounts";

/**
 * The providers that host repositories. The backend marks them the same way (a
 * provider with an SSH host), so a token for Anthropic is not offered where a git
 * token is asked for, and the list is not a second copy of which providers those are.
 */
export function gitHostProviders(providers: AccountProvider[]): AccountProvider[] {
  return providers.filter((provider) => provider.SSHHost);
}

/** Bitbucket authenticates an app password with a username and refuses it without one. */
export function usernameRequired(providerId: string): boolean {
  return providerId === "bitbucket";
}

/**
 * What to tell the person after a token was saved (or replaced) and the provider
 * was asked about it. `failure` is why verifying did not happen, when it did not.
 *
 * A classic GitHub token lists its scopes; one without `repo` verifies fine and
 * is then refused on every private repository, which is a confusing place to find
 * out. A fine-grained token lists no scopes at all, so an empty list says nothing
 * and is not warned about.
 */
export function tokenNote(
  account: Pick<Account, "provider" | "subject" | "scopes">,
  failure = "",
  replaced = false,
): string {
  const done = replaced ? "Token replaced" : "Saved";
  if (failure) {
    return `${done}, but ${failure}. Check the token, then verify it.`;
  }
  if (!account.subject) {
    return `${done}. This provider cannot check a token, so it is used as it is.`;
  }
  const scopes = account.scopes ?? [];
  const base = `${replaced ? "Token replaced. " : ""}Verified as ${account.subject}.`;
  if (account.provider === "github" && scopes.length > 0 && !scopes.includes("repo")) {
    return `${base} It has no “repo” scope, so private repositories will be refused.`;
  }
  return base;
}

/**
 * Whether a token can be replaced in place: a token or an app password the
 * person owns. A key is replaced by adding the new one, an external identity
 * holds no value, and a connection's password is changed through its connection.
 */
export function canRotateToken(account: Pick<Account, "kind" | "scopeOwner" | "state">): boolean {
  return (
    (account.kind === "token" || account.kind === "basic") &&
    (account.scopeOwner === "" || isLocalAccount(account)) &&
    account.state !== "needs_local_secret"
  );
}
