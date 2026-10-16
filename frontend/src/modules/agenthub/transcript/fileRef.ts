export type FileRef = { path: string; line?: number };

// A stem is required before the extension: `.go` alone is a word, not a file.
const SOURCE_EXT = /(^|\/)[^/]+\.(go|ts|tsx|js|jsx|mjs|cjs|java|kt|lua|py|rb|rs|c|h|cpp|hpp|cs|swift|sql|json|ya?ml|toml|md|mdx|pdf|txt|csv|log|css|scss|html|sh|proto|gradle|xml|properties|env)$/i;
// A file name with any short extension: `docs/spec.pdf`, `/a/b/report.docx`.
const ANY_EXT = /(^|\/)[^/]+\.[A-Za-z0-9]{1,8}$/;
// Well-known names with no (or an unusual) extension; they count as files on their own.
const KNOWN_NAMES = /(^|\/)(Dockerfile|Makefile|Taskfile\.ya?ml|go\.mod|go\.sum|package\.json|tsconfig\.json|Cargo\.toml|pom\.xml|build\.gradle(\.kts)?|\.env(\.\w+)?)$/;
const SHAPE = /^(\/?[\w.@-]+(?:\/[\w.@-]+)*)(?::(\d+)(?:-\d+)?)?(?::\d+)?$/; // path[:line[-end]][:col]
// Libraries and frameworks that read like a file name in prose ("Node.js").
const PROSE_NAMES = /^(Node|Next|Vue|Nuxt|Express|Three|D3|Chart)\.js$/;
// GitHub-style line anchor: #L27 or #L27-L30 (the first number is the line).
const LINE_ANCHOR = /#L(\d+)(?:-L\d+)?$/;

/** True when `..` appears only as leading segments of a relative path, before any name. */
function staysInside(path: string): boolean {
  if (path.startsWith("/")) return !path.split("/").includes("..");
  const segments = path.split("/");
  let i = 0;
  while (segments[i] === "..") i++;
  // Nothing but `..`, or a `..` after a real name, escapes or hides a traversal.
  return i < segments.length && !segments.slice(i).includes("..");
}

/**
 * A file reference written as inline code — `auth/session.go:88` — or null.
 * Conservative on purpose: a wrong link is worse than plain text, so it must
 * look like a path AND end in a source extension or be a well-known name.
 * URLs, versions and identifiers stay code.
 */
export function fileRef(text: string, { anyExtension = false }: { anyExtension?: boolean } = {}): FileRef | null {
  if (text.includes("://") || /\s/.test(text)) return null;
  // A #L27 / #L27-L30 anchor gives the line and is not part of the path.
  const anchor = LINE_ANCHOR.exec(text);
  const body = (anchor ? text.slice(0, anchor.index) : text).replace(/^\.\//, ""); // a leading ./ is the same folder
  const match = SHAPE.exec(body);
  if (!match) return null;
  const path = match[1];
  if (!staysInside(path)) return null;
  if (PROSE_NAMES.test(path)) return null;
  // An explicit link is the author saying "this is a file", so any extension will do; an inline `span` has to look like one.
  if (!SOURCE_EXT.test(path) && !KNOWN_NAMES.test(path) && !(anyExtension && ANY_EXT.test(path))) return null;
  const line = anchor ? Number(anchor[1]) : match[2] ? Number(match[2]) : undefined;
  return line ? { path, line } : { path };
}

export type LinkTarget = { kind: "url"; url: string } | { kind: "file"; ref: FileRef } | { kind: "other" };

/** What a markdown link `href` points at: the web, a file in the project, or something DevKit will not open. */
export function linkTarget(href: string): LinkTarget {
  // The web and mail are handed to the browser.
  if (/^(https?:|mailto:)/i.test(href)) return { kind: "url", url: href };
  if (/^file:\/\//i.test(href)) {
    // A file URL is a path once the scheme is dropped and the escapes decoded.
    let path: string;
    try {
      path = decodeURIComponent(href.replace(/^file:\/\/(localhost(?=\/))?/i, "")); // file://localhost/a is /a
    } catch {
      return { kind: "other" };
    }
    const ref = fileRef(path, { anyExtension: true });
    return ref ? { kind: "file", ref } : { kind: "other" };
  }
  // Any other scheme (ftp:, javascript:, …) is never opened.
  if (/^[a-z][a-z0-9+.-]*:/i.test(href)) return { kind: "other" };
  // No scheme: a relative or absolute path, if it reads as one.
  const ref = fileRef(href, { anyExtension: true });
  return ref ? { kind: "file", ref } : { kind: "other" };
}
