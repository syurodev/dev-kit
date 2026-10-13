export type FileRef = { path: string; line?: number };

const SOURCE_EXT = /\.(go|ts|tsx|js|jsx|mjs|cjs|java|kt|lua|py|rb|rs|c|h|cpp|hpp|cs|swift|sql|json|ya?ml|toml|md|mdx|css|scss|html|sh|proto|gradle|xml|properties|env)$/i;
const SHAPE = /^(\/?[\w.@-]+(?:\/[\w.@-]+)*)(?::(\d+))?(?::\d+)?$/;

/**
 * A file reference written as inline code — `auth/session.go:88` — or null.
 * Conservative on purpose: a wrong link is worse than plain text, so it must
 * look like a path AND end in a source extension. URLs, versions and
 * identifiers stay code.
 */
export function fileRef(text: string): FileRef | null {
  if (text.includes("://") || /\s/.test(text)) return null;
  const match = SHAPE.exec(text);
  if (!match) return null;
  const path = match[1];
  if (!SOURCE_EXT.test(path)) return null;
  const line = match[2] ? Number(match[2]) : undefined;
  return line ? { path, line } : { path };
}
