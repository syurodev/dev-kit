import { lazy, type ComponentType, type LazyExoticComponent } from "react";
import type { LucideIcon } from "lucide-react";
import type { WorkbenchTab } from "../stores/workbenchTabs";

/**
 * One entry a module adds to its tabs' right-click menu. The shell draws them
 * below its own Close and Pin items, with the menu's icon / label / shortcut
 * columns.
 */
export type TabAction =
  | {
      type: "item";
      id: string;
      label: string;
      icon?: LucideIcon;
      /** A label such as "⌘D"; lib/shortcut makes it per platform. */
      shortcut?: string;
      disabled?: boolean;
      run: () => void;
    }
  | { type: "submenu"; id: string; label: string; icon?: LucideIcon; items: TabAction[] }
  | { type: "separator"; id: string };

export type ModulePeekProps = { focusModule: () => void };

export const CURRENT_MODULE_API_VERSION = "1";

export type ModuleViewComponent = ComponentType<{
  params?: Record<string, unknown>;
}>;

export type ModuleView = {
  id: string;
  title: string;
  component: ModuleViewComponent | LazyExoticComponent<ModuleViewComponent>;
  /** Starts fetching a lazy view before React needs to render it. */
  preload?: () => Promise<void>;
  /**
   * The glyph a tab of this view carries.
   *
   * Here rather than passed at the call site, because a tab outlives the call:
   * open tabs are restored from storage on launch, and an icon is a function —
   * it cannot be written to disk and come back. A view id can, and this is
   * what turns it back into a glyph.
   */
  icon?: LucideIcon;
};

/** The glyph for a view's tab, falling back to the module's own. */
export function viewIcon(moduleId: string, viewId: string): LucideIcon | undefined {
  const module = getModule(moduleId);
  return module?.views.find((view) => view.id === viewId)?.icon ?? module?.icon;
}

type LazyViewModule = { default: ModuleViewComponent };

/**
 * Keeps module metadata eager and tiny while moving the actual UI tree into a
 * separate chunk. The cached loader is shared by hover/click preloading and
 * React.lazy, so a view is never requested twice.
 */
export function lazyModuleView(
  loader: () => Promise<LazyViewModule>,
): Pick<ModuleView, "component" | "preload"> {
  let pending: Promise<LazyViewModule> | undefined;
  const load = () => (pending ??= loader());

  return {
    component: lazy(load),
    preload: () => load().then(() => undefined),
  };
}

export type ModuleCommand = {
  id: string;
  title: string;
  /**
   * What choosing it does.
   *
   * Optional only because the backend's own view of a module is a list of ids
   * and parity compares those. A command without one is a name in the palette
   * that does nothing when picked — which is what every command in the app was
   * until the menu bar needed somewhere to send its clicks.
   */
  run?: () => void;
  /** Shown beside the title in the palette. The menu bar binds its own. */
  shortcut?: string;
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
  /** Identifies the module in the title bar and on its tabs — each module owns its own icon. */
  icon: LucideIcon;
  commands: ModuleCommand[];
  views: ModuleView[];
  capabilities: string[];
  /**
   * Ask the shell for padding and an outer scroll container.
   *
   * The default is bleed: a module fills the content card and draws its own
   * edges. That is the shape any module grows into once it has a header, a
   * toolbar or its own scroll regions — padding then insets it from the card
   * and a full-width divider stops short of both sides, reading as a floating
   * box instead of a divider.
   *
   * Presentational and frontend-only: the backend has no opinion, so module
   * parity ignores it.
   */
  padded?: boolean;
  /**
   * Show a tab strip at the top of this module's content area.
   *
   * Opt-in: a strip earns its 48px only where a module has several items open
   * at once — Notes (a tab per note), Agent Hub (per session), Database (per
   * table). With one view there is nothing to switch between, and the shell
   * already reads 40px of title bar before any content. Tab STATE exists for
   * every module regardless (stores/workbenchTabs.ts); this only decides
   * whether the strip is drawn.
   *
   * Presentational and frontend-only: the backend has no opinion, so module
   * parity ignores it.
   */
  tabs?: boolean;
  /**
   * What the strip's + button does. Without it the button is drawn and does
   * nothing, which is where it stood: the shell cannot know what a new tab
   * means — a note, a console, a session — so the module says.
   *
   * Presentational and frontend-only: the backend has no opinion, so module
   * parity ignores it.
   */
  newTab?: () => void;
  /**
   * What a tab of this module adds to its right-click menu.
   *
   * The shell knows a tab's id and view and nothing about what it holds; only
   * Database knows a `database.console` tab is a file with SQL in it. Called
   * when the menu opens, so it can read the module's current state.
   *
   * Presentational and frontend-only: module parity ignores it.
   */
  tabActions?: (tab: WorkbenchTab) => TabAction[];
  /**
   * What hovering the module's title-bar button shows, under its name.
   *
   * A module with `tabs` gets its open tabs without asking; set this to show
   * something else — Agent Hub lists its sessions. Call `focusModule` before
   * navigating: it switches to the module and closes the card.
   *
   * Presentational and frontend-only: module parity ignores it.
   */
  peek?: ComponentType<ModulePeekProps>;
  /**
   * Drawn over the module's title-bar button: something that happened while
   * you were elsewhere. Positioned absolutely inside the button.
   *
   * Presentational and frontend-only: module parity ignores it.
   */
  badge?: ComponentType;
  /**
   * The module's rail, as the MODULE's, not a document's.
   *
   * Documents may still register one through useSidebarContent — a rail that
   * changes with what is open needs that. This is the floor underneath it: the
   * rail a module has whenever it is the module on screen, including when
   * nothing is open in it.
   *
   * It had to exist the moment tabs stopped being invented on launch. Database
   * registered its rail from each of its documents, so a module with no
   * document open had no rail at all — the same failure useDatabaseChrome's
   * comment describes one level down ("closing that one emptied the rail"),
   * one level up.
   *
   * Lazy, like a view: the manifest stays eager and tiny, and `lazy()` does not
   * run the import until something renders it.
   */
  sidebar?: ModuleViewComponent | LazyExoticComponent<ModuleViewComponent>;
  /**
   * What the command palette can find INSIDE this module while it is the one
   * in front — a table, a file, a note — beyond the commands every module has.
   */
  paletteSearch?: ModulePaletteSearch;
};

/** One thing a module's palette search found, and what picking it does. */
export type PaletteSearchItem = {
  /** Unique within the module. */
  id: string;
  title: string;
  /** Where it lives or what it is, shown dimmer beside the title: a schema, a path. */
  subtitle?: string;
  icon?: LucideIcon;
  run: () => void;
};

export type PaletteSearchGroup = { heading: string; items: PaletteSearchItem[] };

export type ModulePaletteSearch = {
  /** Completes "Search …" in the placeholder: "tables", "endpoints, files and symbols". */
  noun: string;
  /**
   * Best matches first, already limited to what is worth a row each. The
   * module does its own matching because only it knows its data — a symbol
   * search is a query to Go, a table search is a walk of the schema — and
   * shares workbench/palette/match.ts so the ranking reads the same everywhere.
   *
   * An empty query asks what is worth offering unprompted, which may be
   * nothing. It must not throw for "this module has nothing loaded": a locked
   * vault or no open project is an empty answer.
   *
   * Imported when called, like a command's run: the manifest is in the
   * initial bundle and a module's store and bridge are not.
   */
  search: (query: string, signal: AbortSignal) => Promise<PaletteSearchGroup[]>;
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
      !view.component ||
      (typeof view.component !== "function" && typeof view.component !== "object") ||
      (view.preload !== undefined && typeof view.preload !== "function")
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

/**
 * Runs a command by id, wherever it is registered.
 *
 * One entry point, because two things dispatch commands and they must not grow
 * separate tables: the command palette, and the native menu bar (which knows
 * ids and nothing else — see internal/appmenu). Returns whether anything ran,
 * so a menu item pointing at an id no module declares is visible in a test
 * rather than silent at runtime.
 */
export function runCommand(id: string): boolean {
  for (const module of listRegisteredModules()) {
    const command = module.commands.find((candidate) => candidate.id === id);
    if (command?.run) {
      command.run();
      return true;
    }
  }
  return false;
}
