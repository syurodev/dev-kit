import { Events } from "@wailsio/runtime";
import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

const OUTPUT_EVENT = "terminal:output";
const EXIT_EVENT = "terminal:exit";

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

/** Starts a terminal session sized rows × cols and resolves to its id. */
export type TerminalStart = (rows: number, cols: number) => Promise<string>;

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

/**
 * Starts a session and hands over its output and exit code from the first byte.
 *
 * Subscribing once `start` resolved — what this replaced — dropped whatever the
 * process wrote, or its exit, before the bridge reply arrived. A login prints
 * its URL straight away, and a command that fails at once exits before anyone
 * knows its id. So both events are listened to first and held until it is.
 */
export async function attachTerminal(
  start: () => Promise<string>,
  onOutput: (chunk: Uint8Array) => void,
  onExit?: (code: number) => void,
): Promise<{ sessionId: string; detach: () => void }> {
  let sessionId = "";
  const held: Array<[id: string, deliver: () => void]> = [];
  const route = (id: string | undefined, deliver: () => void) => {
    if (!id) return;
    if (!sessionId) held.push([id, deliver]);
    else if (id === sessionId) deliver();
  };

  const offOutput = Events.On(OUTPUT_EVENT, (ev) => {
    const data = ev?.data as { sessionId?: string; chunk?: string } | undefined;
    const chunk = data?.chunk;
    if (typeof chunk !== "string") return;
    route(data?.sessionId, () => onOutput(fromBase64(chunk)));
  });
  const offExit = Events.On(EXIT_EVENT, (ev) => {
    const data = ev?.data as { sessionId?: string; code?: number } | undefined;
    const code = typeof data?.code === "number" ? data.code : -1;
    route(data?.sessionId, () => onExit?.(code));
  });
  const detach = () => {
    offOutput();
    offExit();
  };

  try {
    sessionId = await start();
  } catch (e) {
    detach();
    throw e;
  }
  for (const [id, deliver] of held.splice(0)) {
    if (id === sessionId) deliver();
  }
  return { sessionId, detach };
}
