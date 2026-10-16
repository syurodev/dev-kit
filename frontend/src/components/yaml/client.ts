import type { Analysis, Edit, Request, Response, Suggestions } from "./types";

let worker: Worker | null = null, sequence = 0;
const pending = new Map<number, { resolve: (value: Response) => void; reject: (error: Error) => void }>();
function getWorker(): Worker {
  if (worker) return worker;
  worker = new Worker(new URL("./worker.ts", import.meta.url), { type: "module" });
  worker.onmessage = (event: MessageEvent<{ id: number; result: Response; error?: string }>) => {
    const task = pending.get(event.data.id);
    pending.delete(event.data.id);
    if (event.data.error) task?.reject(new Error(event.data.error));
    else task?.resolve(event.data.result);
  };
  worker.onerror = () => {
    for (const task of pending.values()) task.reject(new Error("YAML assistance stopped. Reopen this file to retry."));
    pending.clear();
    worker?.terminate();
    worker = null;
  };
  return worker;
}
export function requestYaml(request: Extract<Request, { type: "analyze" }>): Promise<Analysis>;
export function requestYaml(request: Extract<Request, { type: "complete" }>): Promise<Suggestions>;
export function requestYaml(request: Extract<Request, { type: "format" | "applySql" }>): Promise<Edit>;
export function requestYaml(request: Request): Promise<Response> {
  return new Promise((resolve, reject) => {
    const id = ++sequence;
    try {
      const target = getWorker();
      pending.set(id, { resolve, reject });
      target.postMessage({ id, request });
    } catch (error) { pending.delete(id); reject(error); }
  });
}
