import { Events } from "@wailsio/runtime";
import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

const OUTPUT_EVENT = "terminal:output";

/**
 * Go's []byte crosses the bridge as base64 in BOTH directions — the generated
 * binding types TerminalWrite's `data` as string, and main.go encodes PTY
 * output the same way. PTY traffic is arbitrary bytes and a multibyte rune can
 * straddle two chunks, so it never becomes a JS string in transit; xterm does
 * the UTF-8 reassembly across writes.
 */
function toBase64(bytes: Uint8Array): string {
  // Built one char at a time rather than String.fromCharCode(...bytes): the
  // spread form overflows the argument limit on a large paste.
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary);
}

function fromBase64(b64: string): Uint8Array {
  const binary = atob(b64);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}

/** Starts a shell rooted at the active CodeGraph project. Returns its session id. */
export async function startCodeGraphTerminal(rows: number, cols: number): Promise<string> {
  return callBridge(() => AppService.CodeGraphStartTerminal(rows, cols));
}

export async function writeTerminal(sessionId: string, data: string): Promise<void> {
  const bytes = new TextEncoder().encode(data);
  return callBridge(() => AppService.TerminalWrite(sessionId, toBase64(bytes)));
}

export async function resizeTerminal(sessionId: string, rows: number, cols: number): Promise<void> {
  return callBridge(() => AppService.TerminalResize(sessionId, rows, cols));
}

export async function closeTerminal(sessionId: string): Promise<void> {
  return callBridge(() => AppService.TerminalClose(sessionId));
}

/** Subscribes to one session's output. Returns an unsubscribe function. */
export function onTerminalOutput(
  sessionId: string,
  handler: (chunk: Uint8Array) => void,
): () => void {
  return Events.On(OUTPUT_EVENT, (ev) => {
    const data = ev?.data as { sessionId?: string; chunk?: string } | undefined;
    if (!data || data.sessionId !== sessionId || typeof data.chunk !== "string") return;
    handler(fromBase64(data.chunk));
  });
}
