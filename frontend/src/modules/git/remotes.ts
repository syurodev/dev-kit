/**
 * The remote fetch, pull and push talk to: the one the branch tracks, else the
 * first there is. The upstream reads "origin/dev", so a remote is matched by
 * its name and a slash — longest first, since a remote's own name may hold one.
 * Empty when there are no remotes, which is when the buttons have nowhere to go.
 */
export function remoteFor(upstream: string, names: string[]): string {
  const tracked = names
    .filter((name) => upstream.startsWith(`${name}/`))
    .sort((a, b) => b.length - a.length)[0];
  return tracked ?? names[0] ?? "";
}

/**
 * The accounts that can authenticate a remote: an SSH remote is answered by a
 * key and an HTTPS one by a token or password, so offering the other kind would
 * bind something that does nothing. The account already bound stays in the list
 * even when it does not fit, or the select would show an empty value for a
 * binding that is still there.
 */
export function accountsForRemote<T extends { id: string; kind: string }>(
  accounts: T[],
  remote: { transport: string; accountId: string },
): T[] {
  const wantsKey = remote.transport === "ssh";
  return accounts.filter(
    (account) =>
      account.id === remote.accountId ||
      (wantsKey ? account.kind === "ssh-key" : account.kind !== "ssh-key" && account.kind !== "external"),
  );
}
