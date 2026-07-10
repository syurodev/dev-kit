import type { ComponentType } from "react";

export type ModuleView = {
  id: string;
  title: string;
  component: ComponentType;
};

export type ModuleCommand = {
  id: string;
  title: string;
};

export type ModuleDefinition = {
  id: string;
  title: string;
  commands: ModuleCommand[];
  views: ModuleView[];
};

const modules = new Map<string, ModuleDefinition>();

export function registerModule(module: ModuleDefinition): void {
  if (modules.has(module.id)) {
    return;
  }
  modules.set(module.id, module);
}

export function listRegisteredModules(): ModuleDefinition[] {
  return Array.from(modules.values());
}

export function getModule(id: string): ModuleDefinition | undefined {
  return modules.get(id);
}
