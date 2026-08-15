import type { ComponentType } from "react";
import type { LucideIcon } from "lucide-react";

export const CURRENT_MODULE_API_VERSION = "1";

export type ModuleView = {
  id: string;
  title: string;
  component: ComponentType<{ params?: Record<string, unknown> }>;
};

export type ModuleCommand = {
  id: string;
  title: string;
};

/**
 * The local module manifest is the UI-side representation of a built-in
 * module. Backend metadata is compared with it at runtime, but components
 * intentionally remain local and are never loaded from backend data.
 */
export type ModuleManifest = {
  id: string;
  title: string;
  version: string;
  apiVersion: string;
  /** Identifies the module in the Dock and on its tabs — each module owns its own icon. */
  icon: LucideIcon;
  commands: ModuleCommand[];
  views: ModuleView[];
  capabilities: string[];
};

export type ModuleMetadata = {
  id: string;
  title: string;
  version: string;
  apiVersion: string;
  commands: string[];
  views: string[];
  capabilities: string[];
};

export type ModuleParityIssue = {
  moduleId: string;
  message: string;
};

const identifierPattern = /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/;
const versionPattern = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/;
const apiVersionPattern = /^[1-9][0-9]*$/;
const capabilityPattern =
  /^[a-z][a-z0-9]*(?::[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*)+$/;

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function validateIdentifier(kind: string, value: unknown): void {
  if (!isNonEmptyString(value) || !identifierPattern.test(value)) {
    throw new Error(`Invalid ${kind}: ${String(value || "<empty>")}`);
  }
}

function validateUniqueIdentifiers(kind: string, ids: unknown[]): void {
  const seen = new Set<string>();
  for (const id of ids) {
    validateIdentifier(`${kind} id`, id);
    if (typeof id !== "string") {
      continue;
    }
    if (seen.has(id)) {
      throw new Error(`Duplicate ${kind} contribution: ${id}`);
    }
    seen.add(id);
  }
}

function validateCapabilities(capabilities: unknown[]): void {
  const seen = new Set<string>();
  for (const capability of capabilities) {
    if (!isNonEmptyString(capability) || !capabilityPattern.test(capability)) {
      throw new Error(
        `Invalid capability declaration: ${String(capability || "<empty>")}`,
      );
    }
    if (typeof capability !== "string") {
      continue;
    }
    if (seen.has(capability)) {
      throw new Error(`Duplicate capability declaration: ${capability}`);
    }
    seen.add(capability);
  }
}

export function validateModuleManifest(manifest: ModuleManifest): void {
  if (!manifest || typeof manifest !== "object") {
    throw new Error("Module manifest is required");
  }
  validateIdentifier("module id", manifest.id);
  if (!isNonEmptyString(manifest.title)) {
    throw new Error(`Module ${manifest.id} title is required`);
  }
  if (!isNonEmptyString(manifest.version) || !versionPattern.test(manifest.version)) {
    throw new Error(`Module ${manifest.id} version must be semantic version`);
  }
  if (
    !isNonEmptyString(manifest.apiVersion) ||
    !apiVersionPattern.test(manifest.apiVersion)
  ) {
    throw new Error(`Module ${manifest.id} apiVersion must be a positive integer`);
  }
  if (manifest.apiVersion !== CURRENT_MODULE_API_VERSION) {
    throw new Error(`Unsupported module API version: ${manifest.apiVersion}`);
  }
  // Not `typeof === "function"` — lucide-react icons are React.forwardRef
  // components, which are typeof "object" at runtime, not "function".
  if (!manifest.icon || (typeof manifest.icon !== "function" && typeof manifest.icon !== "object")) {
    throw new Error(`Module ${manifest.id} icon is required`);
  }
  if (!Array.isArray(manifest.commands) || !Array.isArray(manifest.views)) {
    throw new Error(`Module ${manifest.id} contributions must be arrays`);
  }
  if (!Array.isArray(manifest.capabilities)) {
    throw new Error(`Module ${manifest.id} capabilities must be an array`);
  }

  for (const command of manifest.commands) {
    if (
      !command ||
      typeof command !== "object" ||
      !isNonEmptyString(command.id) ||
      !isNonEmptyString(command.title)
    ) {
      throw new Error(`Module ${manifest.id} command title is required`);
    }
  }
  for (const view of manifest.views) {
    if (
      !view ||
      typeof view !== "object" ||
      !isNonEmptyString(view.id) ||
      !isNonEmptyString(view.title) ||
      typeof view.component !== "function"
    ) {
      throw new Error(`Module ${manifest.id} view must have a title and component`);
    }
  }
  validateUniqueIdentifiers(
    "command",
    manifest.commands.map((command) => command.id),
  );
  validateUniqueIdentifiers(
    "view",
    manifest.views.map((view) => view.id),
  );
  validateCapabilities(manifest.capabilities);
}

const modules = new Map<string, ModuleManifest>();
const commandOwners = new Map<string, string>();
const viewOwners = new Map<string, string>();

export function registerModule(manifest: ModuleManifest): void {
  validateModuleManifest(manifest);

  if (modules.has(manifest.id)) {
    throw new Error(`Module is already registered: ${manifest.id}`);
  }
  for (const command of manifest.commands) {
    const owner = commandOwners.get(command.id);
    if (owner) {
      throw new Error(`Command ${command.id} is already contributed by ${owner}`);
    }
  }
  for (const view of manifest.views) {
    const owner = viewOwners.get(view.id);
    if (owner) {
      throw new Error(`View ${view.id} is already contributed by ${owner}`);
    }
  }

  // All checks happen before mutating any registry map so a rejected module
  // cannot leave behind partial command or view registrations.
  modules.set(manifest.id, manifest);
  for (const command of manifest.commands) {
    commandOwners.set(command.id, manifest.id);
  }
  for (const view of manifest.views) {
    viewOwners.set(view.id, manifest.id);
  }
}

export function listRegisteredModules(): ModuleManifest[] {
  return Array.from(modules.values()).sort((left, right) =>
    left.id.localeCompare(right.id),
  );
}

export function getModule(id: string): ModuleManifest | undefined {
  return modules.get(id);
}

function sorted(values: readonly string[]): string[] {
  return [...values].sort();
}

function compareIDs(
  moduleId: string,
  kind: string,
  localIDs: readonly string[],
  backendIDs: readonly string[],
): ModuleParityIssue[] {
  const local = new Set(localIDs);
  const backend = new Set(backendIDs);
  const issues: ModuleParityIssue[] = [];

  for (const id of sorted(localIDs)) {
    if (!backend.has(id)) {
      issues.push({
        moduleId,
        message: `Backend is missing ${kind} ${id}`,
      });
    }
  }
  for (const id of sorted(backendIDs)) {
    if (!local.has(id)) {
      issues.push({
        moduleId,
        message: `Backend contributed unknown ${kind} ${id}`,
      });
    }
  }
  if (backend.size !== backendIDs.length) {
    issues.push({
      moduleId,
      message: `Backend returned duplicate ${kind} metadata`,
    });
  }
  return issues;
}

export function compareModuleParity(
  localModules: readonly ModuleManifest[],
  backendModules: readonly ModuleMetadata[],
): ModuleParityIssue[] {
  const issues: ModuleParityIssue[] = [];
  const localByID = new Map(localModules.map((module) => [module.id, module]));
  const backendByID = new Map<string, ModuleMetadata>();

  for (const backend of backendModules) {
    const backendID = typeof backend?.id === "string" ? backend.id : "";
    if (!backendID || !identifierPattern.test(backendID)) {
      issues.push({
        moduleId: backendID || "<unknown>",
        message: "Backend returned an invalid module identifier",
      });
      continue;
    }
    if (backendByID.has(backendID)) {
      issues.push({
        moduleId: backendID,
        message: "Backend returned duplicate module metadata",
      });
      continue;
    }
    backendByID.set(backendID, {
      id: backendID,
      title: typeof backend.title === "string" ? backend.title : "",
      version: typeof backend.version === "string" ? backend.version : "",
      apiVersion:
        typeof backend.apiVersion === "string" ? backend.apiVersion : "",
      commands: Array.isArray(backend.commands)
        ? backend.commands.filter((id): id is string => typeof id === "string")
        : [],
      views: Array.isArray(backend.views)
        ? backend.views.filter((id): id is string => typeof id === "string")
        : [],
      capabilities: Array.isArray(backend.capabilities)
        ? backend.capabilities.filter(
            (capability): capability is string => typeof capability === "string",
          )
        : [],
    });
  }

  for (const local of localModules) {
    const backend = backendByID.get(local.id);
    if (!backend) {
      issues.push({
        moduleId: local.id,
        message: "Backend is missing this built-in module",
      });
      continue;
    }
    if (backend.title !== local.title) {
      issues.push({ moduleId: local.id, message: "Module title differs" });
    }
    if (backend.version !== local.version) {
      issues.push({ moduleId: local.id, message: "Module version differs" });
    }
    if (backend.apiVersion !== local.apiVersion) {
      issues.push({ moduleId: local.id, message: "Module API version differs" });
    }
    issues.push(
      ...compareIDs(
        local.id,
        "command",
        local.commands.map((command) => command.id),
        backend.commands,
      ),
      ...compareIDs(
        local.id,
        "view",
        local.views.map((view) => view.id),
        backend.views,
      ),
      ...compareIDs(
        local.id,
        "capability",
        local.capabilities,
        backend.capabilities,
      ),
    );
  }

  for (const backend of backendByID.values()) {
    if (!localByID.has(backend.id)) {
      issues.push({
        moduleId: backend.id,
        message: "Backend returned an unknown built-in module",
      });
    }
  }

  return issues;
}
