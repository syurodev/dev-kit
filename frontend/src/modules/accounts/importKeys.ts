import type { AccountProvider, KeyFile } from "@/bridge/accounts";

/**
 * The name offered for an imported key: who the provider said it is when it
 * answered, else the file's own name. Never empty — an account needs a label,
 * and a blank field is a form the person has to fill in before the button works.
 */
export function suggestLabel(providerName: string, subject: string, fileName: string): string {
  if (subject) return `${providerName} · ${subject}`;
  return fileName ? `${providerName} · ${fileName}` : providerName;
}

/**
 * Which ssh config aliases start ticked. An alias is only worth binding to the
 * account when it points at the provider the key is for: `github-work` →
 * github.com belongs with a GitHub key, and tying it to a GitLab account would
 * send every `git@github-work:` remote to a key GitHub has never heard of.
 */
export function defaultAliasChecks(
  aliases: KeyFile["aliases"],
  provider: Pick<AccountProvider, "Hosts"> | undefined,
): Record<string, boolean> {
  const hosts = new Set((provider?.Hosts ?? []).map((host) => host.toLowerCase()));
  return Object.fromEntries(aliases.map((alias) => [alias.name, hosts.has(alias.hostName.toLowerCase())]));
}

/** The aliases to bind, in the order the file lists them. */
export function aliasesToBind(aliases: KeyFile["aliases"], checks: Record<string, boolean>): string[] {
  return aliases.filter((alias) => checks[alias.name]).map((alias) => alias.name);
}

/** One line saying where a key is already saved, empty when it is not. */
export function savedAsNote(file: Pick<KeyFile, "imported">): string {
  return file.imported.map((held) => `${held.label} (${held.provider})`).join(", ");
}
