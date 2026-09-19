import { useMemo, useState } from "react";
import { java } from "@codemirror/lang-java";
import { CodeEditor } from "@/components/code-editor/CodeEditor";

// Deliberately the shape that wraps worst: deep indentation, and long tokens
// with no space in them (an annotation argument, a call chain).
const SAMPLE = `    @GetMapping
    public DeviceListResponse list(
            @AuthenticationPrincipal Jwt jwt,
            @RequestHeader(SyncHeaderResolver.DEVICE_HEADER) String deviceId,
            @RequestHeader(SyncHeaderResolver.PROTOCOL_HEADER) String protocolText) {
        var context = authorizer.authorize(jwt, deviceId, protocolText);
        var summaries = listDevices.list(new ListDevicesCommand(context));
        return new DeviceListResponse(summaries.stream().map(DeviceController::toResponse).toList());
    }
`;

const WIDTHS = [280, 420, 900];

/**
 * The code editor at the widths a split pane actually reaches. wrap="auto", so
 * the two narrow ones wrap and the wide one scrolls; ⌥Z in any of them flips it.
 */
export function CodeEditorWrapSection() {
  const [source, setSource] = useState(SAMPLE);
  const extensions = useMemo(() => [java()], []);
  return (
    <section id="code-editor-wrap" className="flex flex-col gap-3 px-6 py-5">
      <h2 className="text-title-16 text-text-primary">Code editor — wrapping</h2>
      <div className="flex flex-wrap items-start gap-4">
        {WIDTHS.map((width) => (
          <div key={width} className="flex flex-col gap-1" style={{ width }}>
            <span className="text-label-11-caps text-text-tertiary">{width}px</span>
            <div className="h-80 overflow-hidden rounded-md border border-border-subtle bg-surface-well">
              <CodeEditor value={source} onChange={setSource} extensions={extensions} wrap="auto" />
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
