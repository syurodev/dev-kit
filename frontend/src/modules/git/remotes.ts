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
