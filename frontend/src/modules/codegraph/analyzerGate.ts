import type { CodeGraphToolchainStatus } from "@/bridge/codegraph";

/**
 * Which unbuilt analyzers should take over CodeGraph's canvas.
 *
 * Not simply "every analyzer that is not ready", which is what this was when
 * there was one of them. Analyzers are per language now, and one of them is
 * built into DevKit: a machine with no JDK can still read every Lua project
 * perfectly well, and putting a Java setup panel in front of that would block
 * a module that works.
 *
 * So the open project's language decides. With nothing open the question is
 * only whether ANY analyzer is ready — if one is, there is a project the user
 * can open and the panel would be in the way; if none is, there is nothing
 * they can do but build one. A language we do not know yet blocks nothing: the
 * open's own error covers that case, and a moment later the index info says
 * what the folder was.
 *
 * Null statuses (not probed, or the probe failed) count as ready throughout: a
 * status call that cannot answer must not hide a working module.
 */
export function blockingAnalyzers(
  statuses: CodeGraphToolchainStatus[] | null,
  projectPath: string,
  projectLanguage: string,
): CodeGraphToolchainStatus[] {
  const all = statuses ?? [];
  const missing = all.filter((s) => !s.ready);
  if (missing.length === 0) return [];
  if (projectPath) {
    return projectLanguage ? missing.filter((s) => s.language === projectLanguage) : [];
  }
  return missing.length === all.length ? missing : [];
}
