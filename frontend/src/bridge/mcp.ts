import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export function mcpListAgents(): Promise<string[]> {
  return callBridge(async () => {
    const list = await AppService.McpListAgents();
    return list ?? [];
  });
}

export function mcpInstall(
  agent: string,
  scope: string,
  projectPath: string,
): Promise<string> {
  return callBridge(() => AppService.McpInstall(agent, scope, projectPath));
}

export function mcpUninstall(
  agent: string,
  scope: string,
  projectPath: string,
): Promise<string> {
  return callBridge(() => AppService.McpUninstall(agent, scope, projectPath));
}
