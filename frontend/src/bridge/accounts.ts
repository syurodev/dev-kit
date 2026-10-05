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
  kind: "token" | "basic" | "oauth" | "secret" | "ssh-key" | "external";
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
  /** Where the provider answers SSH. Empty when it accepts no SSH keys. */
  SSHHost: string;
};

/**
 * A scoped account that belongs to no connection: it simply stays on this
 * machine. An SSH key the person did not choose to sync is one.
 */
export function isLocalAccount(account: Pick<Account, "scopeOwner">): boolean {
  return account.scopeOwner.startsWith("local:");
}

/** What a key looks like without being the key: safe to show and to copy. */
export type SSHKeyInfo = {
  type: string;
  fingerprint: string;
  publicKey: string;
  encrypted: boolean;
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

/**
 * Saves a private key that authenticates git over SSH. The key and passphrase
 * cross once, here, and are checked before anything is stored. It stays on this
 * machine unless the person asks it to sync: a private key leaving is a decision,
 * not a default.
 */
export function createSSHKeyAccount(input: {
  provider: string;
  label: string;
  privateKey: string;
  passphrase?: string;
  syncAcrossDevices?: boolean;
}): Promise<Account> {
  return callBridge(
    async () =>
      (await AppService.AccountsCreateSSHKey(
        input.provider,
        input.label,
        input.privateKey,
        input.passphrase ?? "",
        input.syncAcrossDevices ?? false,
      )) as Account,
  );
}

/** The fingerprint and public line of a key account — never anything private. */
export function sshKeyInfo(id: string): Promise<SSHKeyInfo> {
  return callBridge(async () => (await AppService.AccountsSSHKeyInfo(id)) as SSHKeyInfo);
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

/** What a migration pass did, so the UI can report it rather than claim success. */
export type MigrationReport = {
  copied: number;
  alreadyThere: number;
  cleared: number;
};

/**
 * Copies database, SSH and Kafka secrets out of their profile payloads into
 * Accounts, leaving the payloads alone.
 *
 * Copying is what makes it safe to run on every device in any order: a payload
 * syncs and a scoped account does not, so emptying one here would leave the
 * others holding a profile they cannot use.
 */
export function migrateProfileCredentials(): Promise<MigrationReport> {
  return callBridge(
    async () => (await AppService.AccountsMigrateProfiles()) as MigrationReport,
  );
}

/**
 * Empties the profile payloads whose secret is already here. This is the
 * moment connection passwords stop syncing — every other device will ask for
 * them again — which is why it is a separate, deliberate action.
 */
export function stopSyncingProfileSecrets(): Promise<MigrationReport> {
  return callBridge(
    async () => (await AppService.AccountsStopSyncingProfileSecrets()) as MigrationReport,
  );
}

/** What the badge counts: anything that is not simply valid. */
export function needsAttention(accounts: Account[]): number {
  return accounts.filter((account) => account.state !== "valid").length;
}

/**
 * The one credential shape that crosses this bridge.
 *
 * Every other path resolves inside Go at the moment of use. This one exists
 * because the alternative is worse: somebody who cannot read back the token
 * they saved keeps a second copy of it in a note somewhere DevKit did not
 * seal. `accounts:reveal` is a capability the MCP subject is never granted, so
 * an agent cannot reach this — there is no door, rather than a check saying no.
 */
export type RevealedCredential = { user: string; secret: string; kind: string };

/** Whether revealing asks for the Master Password again. Defaults to on. */
export function revealRequiresPassword(): Promise<boolean> {
  return callBridge(async () => await AppService.AccountsRevealRequiresPassword());
}

export function setRevealRequiresPassword(required: boolean): Promise<void> {
  return callBridge(async () => {
    await AppService.AccountsSetRevealRequiresPassword(required);
  });
}

/**
 * Shows one account's value. Pass "" for the password when the preference is
 * off; the backend is what decides, not this argument.
 */
export function revealAccount(id: string, masterPassword: string): Promise<RevealedCredential> {
  return callBridge(
    async () => (await AppService.AccountsReveal(id, masterPassword)) as RevealedCredential,
  );
}

/**
 * Which account authenticates to a host on an environment.
 *
 * `decided` false with two or more options is a question, never a guess: two
 * GitHub accounts are usually a personal one and a company one, and sending a
 * request as the wrong identity is worse than asking once.
 */
export type HostMatch = {
  decided: boolean;
  account: Account;
  options: Account[] | null;
  /** The decision came from a stored answer rather than from being the only one. */
  remembered: boolean;
};

export function accountsForHost(host: string, env: string): Promise<HostMatch> {
  return callBridge(async () => (await AppService.AccountsForHost(host, env)) as HostMatch);
}

export function chooseAccountForHost(host: string, env: string, accountId: string): Promise<void> {
  return callBridge(async () => {
    await AppService.AccountsChooseForHost(host, env, accountId);
  });
}

export function forgetHostChoice(host: string, env: string): Promise<void> {
  return callBridge(async () => {
    await AppService.AccountsForgetHostChoice(host, env);
  });
}

/**
 * The host a request URL authenticates to, for asking which account covers it.
 *
 * A URL still holding unfilled `{{variables}}` has no host to ask about yet,
 * and guessing one would match an account to a template rather than to a
 * server. Returns "" rather than throwing: not knowing yet is the normal state
 * while someone is typing.
 */
export function hostOf(url: string): string {
  if (!url || url.includes("{{")) return "";
  try {
    return new URL(url).hostname;
  } catch {
    return "";
  }
}
