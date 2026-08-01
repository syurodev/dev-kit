import { callBridge } from "./client";
import { AppService } from "../../bindings/changeme/internal/bridge";
import type { ModuleMetadata } from "../modules/registry";

export type ExecuteCommandRequest = {
  id: string;
  payload: Record<string, unknown>;
};

export type ModuleDTO = ModuleMetadata;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((item): item is string => typeof item === "string");
}

export function listModules(): Promise<ModuleDTO[]> {
  return callBridge(async () => {
    const mods = await AppService.ListModules();
    if (!Array.isArray(mods)) {
      return [];
    }

    // Normalize bridge data at the boundary. A malformed metadata response
    // may create a parity warning, but it must not break the existing UI.
    return mods.flatMap((module): ModuleDTO[] => {
      if (!isRecord(module)) {
        return [];
      }
      return [
        {
          id: stringValue(module.id),
          title: stringValue(module.title),
          version: stringValue(module.version),
          apiVersion: stringValue(module.api_version),
          commands: stringArray(module.commands),
          views: stringArray(module.views),
          capabilities: stringArray(module.capabilities),
        },
      ];
    });
  });
}

export function executeCommand<T>(request: ExecuteCommandRequest): Promise<T> {
  return callBridge(() => AppService.ExecuteCommand(request) as Promise<T>);
}
