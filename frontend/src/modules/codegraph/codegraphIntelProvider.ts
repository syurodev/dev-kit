import type { CodeIntelProvider, CodeIntelRequest, CodeLocation } from "@/components/code-editor/codeIntel";
import {
  findCodeGraphDefinitions,
  findCodeGraphImplementations,
  findCodeGraphReferences,
  type CodeGraphDefinition,
  type CodeGraphReference,
} from "@/bridge/codegraph";
import { shortLabel } from "./graph/labels";

/**
 * A node's own label is fully qualified ("com.acme.OrderService.create"),
 * which reads fine as an identifier but is mostly noise in a picker meant to
 * be scanned at a glance — shortLabel is the graph canvas's own answer to
 * that same problem (long node labels next to a fixed-width card), reused
 * here rather than re-deriving the same rule a second way.
 */
function toCodeLocation(definition: CodeGraphDefinition): CodeLocation {
  return {
    path: definition.filePath,
    line: definition.line,
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
function referenceToCodeLocation(reference: CodeGraphReference): CodeLocation {
  return {
    path: reference.filePath,
    line: reference.line,
    label: shortLabel(reference.label, reference.kind === "inject" ? "class" : "method"),
    kind: reference.kind,
    // An unreadable file leaves code empty; no preview is better than a blank one.
    ...(reference.code ? { preview: reference.code } : {}),
  };
}

/**
 * codeIntel.ts's provider interface, backed by the codegraph index's
 * FindDefinitions (internal/codegraph/definitions.go). `request.line` is
 * passed straight through as the SAVED-file line codeIntel.ts already
 * resolved it to — the index has no notion of an unsaved buffer, so asking it
 * about the on-screen line would be asking about text it may never have seen.
 */
export function createCodeGraphIntelProvider(): CodeIntelProvider {
  return {
    async definition(request: CodeIntelRequest, signal: AbortSignal): Promise<CodeLocation[]> {
      const definitions = await findCodeGraphDefinitions(request.path, request.line, request.identifier);
      // findCodeGraphDefinitions wraps a Wails call that cannot itself be
      // cancelled (see callBridge) — checking here is the "as far as it can"
      // this provider can do: a result for a lookup nobody is waiting for
      // anymore is dropped instead of handed back to open a stale tooltip.
      return signal.aborted ? [] : definitions.map(toCodeLocation);
    },
    async implementation(request: CodeIntelRequest, signal: AbortSignal): Promise<CodeLocation[]> {
      const implementations = await findCodeGraphImplementations(request.path, request.line, request.identifier);
      return signal.aborted ? [] : implementations.map(toCodeLocation);
    },
    async references(request: CodeIntelRequest, signal: AbortSignal): Promise<CodeLocation[]> {
      const references = await findCodeGraphReferences(request.path, request.line, request.identifier);
      return signal.aborted ? [] : references.map(referenceToCodeLocation);
    },
  };
}
