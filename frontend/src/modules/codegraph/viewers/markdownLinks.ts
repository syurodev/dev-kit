/**
 * Where a link in a rendered Markdown file goes. Pure: the view decides what to
 * do with the answer. Nothing here resolves to anything but the web (http,
 * https, mailto — handed to the system browser), a heading of the same file, or
 * a file of the same folder.
 */
export type DocLink =
  | { kind: "url"; url: string }
  | { kind: "anchor"; id: string }
  | { kind: "file"; path: string; anchor?: string }
  | { kind: "other" };

/** `dir/` of a folder-relative path ("" at the root). */
function dirOf(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? "" : path.slice(0, i);
}

/**
 * `href` as written in the file at `docPath` (folder-relative). A path that
 * would climb out of the folder is "other", as is any scheme but the web's.
 */
export function resolveDocLink(docPath: string, href: string): DocLink {
  const raw = href.trim();
  if (!raw) return { kind: "other" };
  if (/^(https?:|mailto:)/i.test(raw)) return { kind: "url", url: raw };
  if (raw.startsWith("#")) return raw.length > 1 ? { kind: "anchor", id: decodeFragment(raw.slice(1)) } : { kind: "other" };
  // //host/path, file:, javascript:, data:, ftp: … — never opened.
  if (raw.startsWith("//") || /^[a-z][a-z0-9+.-]*:/i.test(raw)) return { kind: "other" };

  const hash = raw.indexOf("#");
  const beforeHash = hash < 0 ? raw : raw.slice(0, hash);
  const anchor = hash < 0 || hash === raw.length - 1 ? undefined : decodeFragment(raw.slice(hash + 1));
  const query = beforeHash.indexOf("?");
  const target = decodeFragment(query < 0 ? beforeHash : beforeHash.slice(0, query));
  if (!target) return anchor ? { kind: "anchor", id: anchor } : { kind: "other" };

  // An absolute path is the folder's root, as GitHub reads it.
  const base = target.startsWith("/") ? [] : dirOf(docPath).split("/").filter(Boolean);
  for (const part of target.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      if (base.length === 0) return { kind: "other" };
      base.pop();
    } else {
      base.push(part);
    }
  }
  if (base.length === 0) return { kind: "other" };
  return { kind: "file", path: base.join("/"), ...(anchor ? { anchor } : {}) };
}

function decodeFragment(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

/**
 * The id GitHub gives a heading, so `[x](#my-heading)` works as it does there:
 * lower-cased, punctuation dropped, spaces to hyphens. Letters and digits of any
 * script stay (Vietnamese headings keep their marks).
 */
export function headingSlug(text: string): string {
  return text
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\p{M}\s_-]/gu, "")
    .replace(/\s/g, "-");
}

/** Slugs for headings in order; a repeat gets `-1`, `-2` like GitHub. */
export function uniqueSlugs(texts: string[]): string[] {
  const seen = new Map<string, number>();
  return texts.map((text) => {
    const base = headingSlug(text);
    const n = seen.get(base) ?? 0;
    seen.set(base, n + 1);
    return n === 0 ? base : `${base}-${n}`;
  });
}
