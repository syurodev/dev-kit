import type { CodeIntelProvider, CodeIntelRequest, CodeLocation } from "@/components/code-editor/codeIntel";
import {
  findCodeGraphDefinitions,
  findCodeGraphImplementations,
  findCodeGraphReferences,
  type CodeGraphDefinition,
  type CodeGraphReference,
} from "@/bridge/codegraph";
import { shortLabel } from "./graph/labels";
import { isUnsupportedProjectError } from "./codegraphErrors";

/**
 * A node's own label is fully qualified ("com.acme.OrderService.create"),
 * which reads fine as an identifier but is mostly noise in a picker meant to
 * be scanned at a glance — shortLabel is the graph canvas's own answer to
 * that same problem (long node labels next to a fixed-width card), reused
 * here rather than re-deriving the same rule a second way.
 */
function toCodeLocation(definition: CodeGraphDefinition, owner: string): CodeLocation {
  return {
    path: definition.filePath,
    line: definition.line,
    owner,
    label: shortLabel(definition.label, definition.kind),
    kind: definition.kind,
  };
}

/**
 * A reference's label is the method the use sits in, and its kind is how the
 * symbol is used there ("call", "read", "write", "inject") — not what the
 * enclosing node is, which shortLabel still needs to be told: everything that
 * can hold a use is a method, except the class an injection is declared on.
 */
function referenceToCodeLocation(reference: CodeGraphReference, owner: string): CodeLocation {
  return {
    path: reference.filePath,
    line: reference.line,
    owner,
    label: shortLabel(reference.label, reference.kind === "inject" ? "class" : "method"),
    kind: reference.kind,
    // An unreadable file leaves code empty; no preview is better than a blank one.
    ...(reference.code ? { preview: reference.code } : {}),
  };
}

/**
 * A folder no analyzer reads has no index to look anything up in. That is the
 * same answer as "nothing found", not "the lookup failed": an editor tooltip
 * saying "Could not look up" on every Terraform file would be a warning about
 * nothing.
 */
async function orNothing<T>(lookup: Promise<T[]>): Promise<T[]> {
  try {
    return await lookup;
  } catch (e) {
    if (isUnsupportedProjectError(e)) return [];
    throw e;
  }
}

/**
 * codeIntel.ts's provider interface, backed by the index of ONE folder — the
 * file's owner (FindDefinitions, internal/codegraph/definitions.go).
 *
 * Every question names that folder by its registered path, so it is answered
 * from that folder's index whichever folder the app happens to have focused,
 * and every answer carries that folder as its `owner`: a location is never a
 * bare path. Nothing here matches a name across folders. A symbol defined in
 * another folder is found only if this folder's index resolved it — which the
 * Java analyzer does for modules of one folder, and nothing does between
 * folders — and the UI says "in <folder>" rather than implying a workspace-wide
 * answer (docs/design/codegraph-workspace.md § 7).
 *
 * `request.line` is the SAVED-file line codeIntel.ts already resolved it to —
 * the index has no notion of an unsaved buffer.
 */
export function createCodeGraphIntelProvider(folder: { id: string; path: string }): CodeIntelProvider {
  return {
    async definition(request: CodeIntelRequest, signal: AbortSignal): Promise<CodeLocation[]> {
      const definitions = await orNothing(findCodeGraphDefinitions(request.path, request.line, request.identifier, folder.path));
      // The bridge call cannot itself be cancelled (see callBridge) — checking
      // here is the "as far as it can": a result for a lookup nobody is
      // waiting for anymore is dropped instead of opening a stale tooltip.
      return signal.aborted ? [] : definitions.map((d) => toCodeLocation(d, folder.id));
    },
    async implementation(request: CodeIntelRequest, signal: AbortSignal): Promise<CodeLocation[]> {
      const implementations = await orNothing(findCodeGraphImplementations(request.path, request.line, request.identifier, folder.path));
      return signal.aborted ? [] : implementations.map((d) => toCodeLocation(d, folder.id));
    },
    async references(request: CodeIntelRequest, signal: AbortSignal): Promise<CodeLocation[]> {
      const references = await orNothing(findCodeGraphReferences(request.path, request.line, request.identifier, folder.path));
      return signal.aborted ? [] : references.map((r) => referenceToCodeLocation(r, folder.id));
    },
  };
}
