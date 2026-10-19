import { isLocalAccount, type Account } from "@/bridge/accounts";

/**
 * One row of the Accounts list: an account and, under an SSH key, the API token
 * the person linked to it. A key and its token are one login to the person and
 * two credentials to DevKit, so the list draws them together.
 */
export type AccountGroup = {
  account: Account;
  /** The token drawn under a key; absent for everything else. */
  token?: Account;
};

/** An account the person owns: shared, or kept on this machine. A connection's password is neither. */
const owned = (account: Pick<Account, "scopeOwner">) => !account.scopeOwner || isLocalAccount(account);

/**
 * The list as rows. A token linked to a key is drawn under that key (under each
 * key it serves) and not again on its own; a token linked to nothing, and every
 * other account, stays a row of its own. Order is the list's own.
 */
export function groupAccounts(accounts: readonly Account[]): AccountGroup[] {
  const byId = new Map(accounts.map((account) => [account.id, account]));
  const underAKey = new Set<string>();
  const rows: AccountGroup[] = [];
  for (const account of accounts) {
    const token = account.kind === "ssh-key" && account.linkedTokenId ? byId.get(account.linkedTokenId) : undefined;
    if (token && token.kind === "token") {
      underAKey.add(token.id);
      rows.push({ account, token });
    } else {
      rows.push({ account });
    }
  }
  return rows.filter((row) => !(row.account.kind === "token" && underAKey.has(row.account.id)));
}

/**
 * The tokens a key could take: the person's own tokens of the same provider that
 * no key has yet, so attaching one never silently takes it from another key.
 */
export function linkableTokens(key: Account, accounts: readonly Account[]): Account[] {
  return accounts.filter(
    (account) =>
      account.kind === "token" &&
      account.provider === key.provider &&
      owned(account) &&
      (account.linkedKeyIds ?? []).length === 0,
  );
}

/**
 * Words for a key and a token that the provider says belong to different logins
 * (both verified, names differ), or "" when they agree or either is unverified.
 * Linking them is still allowed — a bot's token with a person's key is a choice —
 * but the person should see it.
 */
export function loginMismatch(key: Pick<Account, "subject">, token: Pick<Account, "subject">): string {
  if (!key.subject || !token.subject || key.subject.toLowerCase() === token.subject.toLowerCase()) return "";
  return `The key is ${key.subject} and the token is ${token.subject}.`;
}

/** A label for the token a key brings, when the person gave none. */
export function defaultTokenLabel(key: Pick<Account, "label">): string {
  return `${key.label} · API token`;
}

/** Whether an account can be renamed here: not an external sign-in, not a connection's password. */
export function canRename(account: Pick<Account, "kind" | "scopeOwner">): boolean {
  return account.kind !== "external" && (!account.scopeOwner || isLocalAccount(account));
}

/**
 * The providers whose pull requests DevKit reads (internal/forge): a token for
 * one of them is what a key of the same provider cannot supply by itself.
 */
export const FORGE_PROVIDERS = ["github", "gitlab"];

/** Whether the API-token section applies: the person's own SSH key of a provider DevKit reads pull requests from. */
export function takesLinkedToken(account: Pick<Account, "kind" | "scopeOwner" | "provider">): boolean {
  return account.kind === "ssh-key" && FORGE_PROVIDERS.includes(account.provider) && owned(account);
}
