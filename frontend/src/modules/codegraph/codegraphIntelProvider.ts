import type { CodeIntelProvider, CodeIntelRequest, CodeLocation } from "@/components/code-editor/codeIntel";
import { findCodeGraphDefinitions, type CodeGraphDefinition } from "@/bridge/codegraph";
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
  };
}
