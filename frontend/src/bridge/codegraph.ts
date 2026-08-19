import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type CodeGraphEndpoint = {
  id: string;
  httpMethod: string;
  path: string;
  entryNode: string;
};

export type CodeGraphFlowStep = {
  label: string;
  filePath: string;
  line: number;
};

export type CodeGraphApiFlow = {
  endpointId: string;
  steps: CodeGraphFlowStep[];
};

export type CodeGraphSymbolMatch = {
  nodeId: string;
  label: string;
  kind: string;
  filePath: string;
  line: number;
};

export async function openCodeGraphProject(path: string): Promise<void> {
  return callBridge(() => AppService.CodeGraphOpenProject(path));
}

export async function listCodeGraphEndpoints(): Promise<CodeGraphEndpoint[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphListEndpoints();
    return (list ?? []).map((e) => ({
      id: e.id,
      httpMethod: e.httpMethod,
      path: e.path,
      entryNode: e.entryNode,
    }));
  });
}

export async function getCodeGraphApiFlow(endpointId: string): Promise<CodeGraphApiFlow> {
  return callBridge(async () => {
    const flow = await AppService.CodeGraphGetApiFlow(endpointId);
    return {
      endpointId: flow.endpointId,
      steps: (flow.steps ?? []).map((s) => ({
        label: s.label,
        filePath: s.filePath,
        line: s.line,
      })),
    };
  });
}

export async function searchCodeGraphSymbol(query: string): Promise<CodeGraphSymbolMatch[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphSearchSymbol(query);
    return (list ?? []).map((s) => ({
      nodeId: s.nodeId,
      label: s.label,
      kind: s.kind,
      filePath: s.filePath,
      line: s.line,
    }));
  });
}

export type CodeGraphFileEntry = {
  path: string;
  isDir: boolean;
};

export async function listCodeGraphFiles(): Promise<CodeGraphFileEntry[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphListFiles();
    return (list ?? []).map((f) => ({ path: f.path, isDir: f.isDir }));
  });
}

export async function readCodeGraphFile(relativePath: string): Promise<string> {
  return callBridge(() => AppService.CodeGraphReadFile(relativePath));
}

export async function writeCodeGraphFile(relativePath: string, content: string): Promise<void> {
  return callBridge(() => AppService.CodeGraphWriteFile(relativePath, content));
}
