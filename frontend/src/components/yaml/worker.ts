/// <reference lib="webworker" />
import { handleYaml } from "./service";
import type { Request } from "./types";

// A single worker serializes parses/compiles, with no filesystem or network I/O.
self.onmessage = async (event: MessageEvent<{ id: number; request: Request }>) => {
  const { id, request } = event.data;
  try { self.postMessage({ id, result: await handleYaml(request) }); }
  catch (error) { self.postMessage({ id, error: error instanceof Error ? error.message : "YAML assistance failed." }); }
};
