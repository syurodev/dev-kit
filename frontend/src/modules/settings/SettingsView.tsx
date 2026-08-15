import { useEffect, useState } from "react";
import {
  createEnvTag,
  deleteEnvTag,
  listEnvTags,
  updateEnvTag,
  type EnvTag,
} from "../../bridge/environments";
import { errorMessage } from "../../bridge/errors";
import { McpInstallPanel } from "./McpInstallPanel";
import { SyncView } from "../sync/SyncView";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Monitor, Moon, Sun } from "lucide-react";
import { cn } from "@/lib/utils";
import { useTheme } from "@/theme/ThemeProvider";
import type { ThemePreference } from "@/bridge/preferences";

export function SettingsView() {
  return (
    <Tabs defaultValue="appearance" className="gap-4">
      <TabsList>
        <TabsTrigger value="appearance">Appearance</TabsTrigger>
        <TabsTrigger value="environments">Environments</TabsTrigger>
        <TabsTrigger value="sync">Sync</TabsTrigger>
      </TabsList>
      <TabsContent value="appearance">
        <AppearancePanel />
      </TabsContent>
      <TabsContent value="environments">
        <EnvironmentsPanel />
      </TabsContent>
      <TabsContent value="sync">
        <SyncView />
      </TabsContent>
    </Tabs>
  );
}

const themeOptions = [
  { value: "system", label: "System", description: "Follow your operating system", icon: Monitor },
  { value: "light", label: "Light", description: "Always use the light appearance", icon: Sun },
  { value: "dark", label: "Dark", description: "Always use the dark appearance", icon: Moon },
] satisfies Array<{
  value: ThemePreference;
  label: string;
  description: string;
  icon: typeof Monitor;
}>;

function AppearancePanel() {
  const { theme, setTheme } = useTheme();
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function selectTheme(nextTheme: ThemePreference) {
    if (nextTheme === theme) {
      return;
    }
    setError("");
    setSaving(true);
    try {
      await setTheme(nextTheme);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="max-w-2xl space-y-4">
      <div>
        <h2 className="text-lg font-semibold">Appearance</h2>
        <p className="text-sm text-muted-foreground">
          Choose how DevKit looks. This also updates the native window frame.
        </p>
      </div>
      <div role="radiogroup" aria-label="Application appearance" className="grid gap-2 sm:grid-cols-3">
        {themeOptions.map((option) => {
          const Icon = option.icon;
          const selected = option.value === theme;
          return (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={selected}
              disabled={saving}
              onClick={() => void selectTheme(option.value)}
              className={cn(
                "flex min-h-24 flex-col items-start gap-2 rounded-lg border bg-card p-3 text-left transition-colors",
                selected
                  ? "border-ring ring-2 ring-ring/20"
                  : "border-border hover:bg-accent",
              )}
            >
              <Icon className="size-4" />
              <span>
                <span className="block text-sm font-medium">{option.label}</span>
                <span className="block text-xs text-muted-foreground">{option.description}</span>
              </span>
            </button>
          );
        })}
      </div>
      {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
    </section>
  );
}

/**
 * Sync's own module/manifest registration stays intact (backend still
 * reports a "sync" module — removing the local registration would trip
 * the metadata-parity check) — only the *navigation* is merged here. The
 * Sync module's command surface (e.g. "Sync now" in the command palette)
 * keeps working; its dock icon is what's gone.
 */
function EnvironmentsPanel() {
  const [tags, setTags] = useState<EnvTag[]>([]);
  const [newName, setNewName] = useState("");
  const [newRequiresConfirmation, setNewRequiresConfirmation] = useState(false);
  const [error, setError] = useState("");

  async function refresh() {
    try {
      setTags(await listEnvTags());
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function onCreate() {
    setError("");
    try {
      await createEnvTag(newName, newRequiresConfirmation);
      setNewName("");
      setNewRequiresConfirmation(false);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  async function onToggleRequiresConfirmation(tag: EnvTag) {
    setError("");
    try {
      await updateEnvTag(tag.id, !tag.requiresConfirmation);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  async function onDelete(tag: EnvTag) {
    setError("");
    try {
      await deleteEnvTag(tag.id);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  return (
    <section className="settings-view">
      <h2>Environments</h2>

      <div className="env-create">
        <input
          aria-label="env-name"
          placeholder="Tag name"
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
        />
        <label>
          <input
            aria-label="env-new-requires-confirmation"
            type="checkbox"
            checked={newRequiresConfirmation}
            onChange={(e) => setNewRequiresConfirmation(e.target.checked)}
          />
          Requires confirmation
        </label>
        <button type="button" onClick={onCreate}>
          Create tag
        </button>
      </div>

      <table aria-label="env-tags-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Requires confirmation</th>
            <th>Builtin</th>
            <th>Locked</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {tags.map((tag) => (
            <tr key={tag.id}>
              <td>{tag.name}</td>
              <td>
                <input
                  aria-label={`env-requires-confirmation-${tag.name}`}
                  type="checkbox"
                  checked={tag.requiresConfirmation}
                  disabled={tag.locked}
                  onChange={() => onToggleRequiresConfirmation(tag)}
                />
              </td>
              <td>{tag.isBuiltin ? "yes" : "no"}</td>
              <td>{tag.locked ? "yes" : "no"}</td>
              <td>
                <button
                  type="button"
                  aria-label={`env-delete-${tag.name}`}
                  disabled={tag.isBuiltin}
                  onClick={() => onDelete(tag)}
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {error ? <p role="alert">{error}</p> : null}

      <McpInstallPanel />
    </section>
  );
}
