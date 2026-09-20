import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";

/**
 * An account without its value. There is no field here for a credential and
 * no method below that returns one: the backend resolves a value inside Go at
 * the moment of use, so the frontend never holds it and cannot leak it.
 */
export type Account = {
  id: string;
  provider: string;
  kind: "token" | "basic" | "oauth" | "secret" | "external";
  label: string;
  subject: string;
  scopes: string[];
  env: string[];
  /** Empty for a shared account; the owning profile's id for a scoped one. */
  scopeOwner: string;
  state:
    | "valid"
    | "expiring"
    | "expired"
    | "revoked"
    | "unverified"
    | "needs_local_secret";
  expiresAt: string;
  verifiedAt: string;
  ref: string;
};

export type AccountProvider = {
  ID: string;
  Name: string;
  Kinds: string[];
  Hosts: string[] | null;
};

export function listAccounts(): Promise<Account[]> {
  return callBridge(async () => ((await AppService.AccountsList()) ?? []) as Account[]);
}

export function getAccount(id: string): Promise<Account> {
  return callBridge(async () => (await AppService.AccountsGet(id)) as Account);
}

export function accountProviders(): Promise<AccountProvider[]> {
  return callBridge(
    async () => ((await AppService.AccountsProviders()) ?? []) as AccountProvider[],
  );
}

export function createAccount(input: {
  provider: string;
  kind: string;
  label: string;
  user?: string;
  secret?: string;
  env?: string[];
  scopeOwner?: string;
  ref?: string;
}): Promise<Account> {
  return callBridge(
    async () =>
      (await AppService.AccountsCreate(
        input.provider,
        input.kind,
        input.label,
        input.user ?? "",
        input.secret ?? "",
        input.env ?? [],
        input.scopeOwner ?? "",
        input.ref ?? "",
      )) as Account,
  );
}

/** Replaces a value in place, so everything bound to the account keeps working. */
export function rotateAccount(id: string, user: string, secret: string): Promise<void> {
  return callBridge(async () => {
    await AppService.AccountsRotate(id, user, secret);
  });
}

/** Moves a scoped credential into the vault — the one way it starts syncing. */
export function promoteAccount(id: string, label: string): Promise<Account> {
  return callBridge(async () => (await AppService.AccountsPromote(id, label)) as Account);
}

/**
 * Asks the provider who this credential belongs to. It is what turns a saved
 * string into an account with a subject, scopes and an expiry — and it is not
 * on the MCP surface, because it spends a real request at a real provider.
 */
export function verifyAccount(id: string): Promise<Account> {
  return callBridge(async () => (await AppService.AccountsVerify(id)) as Account);
}

export function deleteAccount(id: string): Promise<void> {
  return callBridge(async () => {
    await AppService.AccountsDelete(id);
  });
}

export function accountUsedBy(id: string): Promise<string[]> {
  return callBridge(async () => (await AppService.AccountsUsedBy(id)) ?? []);
}

/** What the badge counts: anything that is not simply valid. */
export function needsAttention(accounts: Account[]): number {
  return accounts.filter((account) => account.state !== "valid").length;
}
