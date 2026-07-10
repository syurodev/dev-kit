import { callBridge } from "./client";
import { AppService } from "../../bindings/changeme/internal/bridge";

export type ExecuteCommandRequest = {
  id: string;
  payload: Record<string, unknown>;
};

export type ModuleDTO = {
  id: string;
  title: string;
  commands: string[];
  views: string[];
};

export function listModules(): Promise<ModuleDTO[]> {
  return callBridge(async () => {
    const mods = await AppService.ListModules();
    return (mods ?? []).map((m) => ({
      id: m.id,
      title: m.title,
      commands: m.commands ?? [],
      views: m.views ?? [],
    }));
  });
}

export function executeCommand<T>(request: ExecuteCommandRequest): Promise<T> {
  return callBridge(() => AppService.ExecuteCommand(request) as Promise<T>);
}
