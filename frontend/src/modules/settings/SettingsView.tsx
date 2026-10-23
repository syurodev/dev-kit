import { NumberStepper as SizeStepper } from "@/components/ui/number-stepper";
import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  createEnvTag,
  deleteEnvTag,
  listEnvTags,
  type EnvTag,
} from "../../bridge/environments";
import { errorMessage } from "../../bridge/errors";
import {
  authLogout,
  authStatus,
  syncConnect,
  syncNow,
  syncStatus,
} from "../../bridge/sync";
import { toast } from "sonner";
import { useTheme } from "@/theme/ThemeProvider";
import { FAMILIES } from "@/theme/families";
import { PaletteSwatch } from "./PaletteSwatch";
import type { ThemeFamilyPreference } from "@/bridge/preferences";
import { useShallow } from "zustand/react/shallow";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { RadioGroup as RadioGroupPrimitive } from "radix-ui";
import { useEditorPreferences } from "@/components/code-editor/preferences";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { SecurityNotice } from "@/components/ui/security-notice";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { ConsoleStorageSetting } from "./ConsoleStorageSetting";
import { RevealPasswordSetting } from "./RevealPasswordSetting";
import { UpdatesSetting } from "./UpdatesSetting";
import { LicensesSetting } from "./LicensesSetting";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  monoFontOptions,
  monoFontStack,
  useAppearanceStore,
  type MonoFont,
} from "@/stores/appearance";
import { codeFontWeightLabel, codeFontWeights, normalizeCodeFontWeight, type CodeFontWeight } from "@/stores/codeFontWeight";
import { TAB_WIDTH_LIMITS, useTabSettingsStore } from "@/stores/tabSettings";
import {
  User,
  LockKeyhole,
  Trash2,
  X,
  RefreshCw,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { chordLabel } from "@/lib/shortcut";
import { KeyboardTab } from "./KeyboardTab";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";

import { SettingsLayout } from "./SettingsLayout";
import { resolveSettingsTab, type SettingsPage, type SettingsTab } from "./settingsNavigation";
import { GroupLabel, SectionHeader, TabColumn } from "./SettingsPrimitives";
import {
  AgentAccountsPage,
  AgentMcpPage,
  AgentModelsPage,
  AgentNotificationsPage,
  AgentRuntimesPage,
  AgentSafetyPage,
  AgentSkillsPage,
} from "./AgentsPages";
export type { SettingsTab } from "./settingsNavigation";

export interface SettingsModalProps {
  open: boolean;
  onClose: () => void;
  /** The section to land on each time it opens — "Configure Tabs…" asks for Appearance. */
  initialTab?: SettingsTab;
}

/** Preferences keep a stable, viewport-bounded shell. Navigation is grouped
 * and searchable; only the selected section mounts its queries and controls.
 * Dialog owns focus trapping, Escape and the shared theme material. */
export function SettingsModal({ open, onClose, initialTab }: SettingsModalProps) {
  const [activeTab, setActiveTab] = useState<SettingsPage>(resolveSettingsTab(initialTab ?? "appearance"));

  // On every opening, not only the first: the modal stays mounted while
  // closed, so a useState initial value would be read once and never again.
  useEffect(() => {
    if (open && initialTab) setActiveTab(resolveSettingsTab(initialTab));
  }, [open, initialTab]);

  return (
    <TooltipProvider>
      <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
        <DialogContent
          showCloseButton={false}
          aria-describedby={undefined}
          // Focus the panel, not its first control: that is the close button,
          // and focusing it opens its tooltip the moment Settings appears.
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            (event.currentTarget as HTMLElement | null)?.focus();
          }}
          className="flex w-[calc(100vw-2rem)] max-w-235 flex-col gap-0 overflow-hidden p-0 text-text-primary select-none sm:max-w-235"
        >
          {/* The height lives here, not on DialogContent: the dialog's body
              is h-fit, so the nav and the scrolling content need a box of
              their own to fill. */}
          <div className="flex h-settings max-h-[90svh] flex-col">
          {/* Modal Header · 56px */}
          <div className="flex h-14 w-full shrink-0 items-center justify-between border-b border-border-subtle pr-3 pl-6">
            <DialogTitle className="text-text-primary">Settings</DialogTitle>

            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={onClose}
                  aria-label="Close settings"
                  className="size-8 rounded-md"
                >
                  <X className="size-4" strokeWidth={1.5} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Close settings (Esc)</TooltipContent>
            </Tooltip>
          </div>

          <SettingsLayout activeTab={activeTab} onSelect={setActiveTab}>
              {activeTab === "account" && <AccountTab />}
              {activeTab === "appearance" && <AppearanceTab />}
              {activeTab === "security" && <SecurityTab />}
              {activeTab === "updates" && <UpdatesTab />}
              {activeTab === "licenses" && <LicensesTab />}
              {activeTab === "sync" && <SyncDevicesTab />}
              {activeTab === "environments" && <EnvironmentsTab />}
              {activeTab === "database" && <DatabaseTab />}
              {activeTab === "agents-runtimes" && <AgentRuntimesPage />}
              {activeTab === "agents-models" && <AgentModelsPage />}
              {activeTab === "agents-accounts" && <AgentAccountsPage />}
              {activeTab === "agents-skills" && <AgentSkillsPage />}
              {activeTab === "agents-safety" && <AgentSafetyPage />}
              {activeTab === "agents-notifications" && <AgentNotificationsPage />}
              {activeTab === "agents-mcp" && <AgentMcpPage />}
              {activeTab === "keyboard" && (
                <KeyboardTab
                  intro={
                    <SectionHeader
                      title="Keyboard"
                      description={`Every shortcut in DevKit. The module you are in is listed first. Press ${chordLabel({ mod: true, key: "/" })} anywhere for a quick reference.`}
                    />
                  }
                />
              )}
          </SettingsLayout>
          </div>
        </DialogContent>
      </Dialog>
    </TooltipProvider>
  );
}

export function SettingsView() {
  return <SettingsModal open={true} onClose={() => {}} />;
}

function sessionLine(expiresAt: string): string {
  const at = expiresAt ? new Date(expiresAt) : null;
  if (!at || Number.isNaN(at.getTime())) return "Session active";
  return `Session active · until ${at.toLocaleString()}`;
}

// ----------------------------------------------------------------------
// 1. Account Tab — Figma `Settings Content / Account` (765:1999)
// ----------------------------------------------------------------------
function AccountTab() {
  const queryClient = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const { data: auth = { authenticated: false, expiresAt: "" }, isPending: accountLoading, error: accountError } = useQuery({
    queryKey: ["auth", "status"],
    queryFn: () => authStatus(),
  });

  const logoutMutation = useMutation({
    mutationFn: () => authLogout(),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["auth", "status"] });
    },
    onError: (e) => {
      setError(errorMessage(e));
    },
  });

  async function handleSignOut() {
    setError("");
    await logoutMutation.mutateAsync();
  }

  async function handleSignIn() {
    setBusy(true);
    setError("");
    try {
      await syncConnect();
      void queryClient.invalidateQueries({ queryKey: ["auth", "status"] });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <TabColumn gap={7}>
      <SectionHeader
        title="Account"
        description="Identity used for sync. Managed by your identity provider — not stored locally."
      />

      {/* identity (765:2004): a raised card — 40px avatar on surface/well,
          the address in Body/14 Medium, the session line in Code/12, and the
          sign-out as a destructive control on the right. */}
      <div className="flex items-center gap-4 rounded-control border border-border-subtle bg-surface-raised px-4 py-4">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-full border border-border-subtle bg-surface-well text-code-12-medium text-text-secondary select-none">
          <User className="size-4.5" strokeWidth={1.5} />
        </div>

        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="truncate text-body-14-medium text-text-primary">
            {accountLoading ? "Checking account…" : auth.authenticated ? "Signed in" : "Not signed in"}
          </span>
          <span className="text-code-12 text-text-tertiary">
            {accountLoading ? "Reading saved identity…" : auth.authenticated ? sessionLine(auth.expiresAt) : "Offline · local only"}
          </span>
        </div>

        {auth.authenticated ? (
          <Button
            type="button"
            variant="destructive"
            size="default"
            disabled={logoutMutation.isPending}
            onClick={() => void handleSignOut().catch(() => {})}
          >
            {logoutMutation.isPending ? "Signing out…" : "Sign out"}
          </Button>
        ) : (
          <Button
            type="button"
            variant="default"
            size="default"
            disabled={busy || accountLoading}
            onClick={handleSignIn}
          >
            {accountLoading ? "Checking…" : busy ? "Connecting browser…" : "Sign in with browser"}
          </Button>
        )}
      </div>

      {error || accountError ? <p role="alert" className="text-body-12 text-status-danger">{error || errorMessage(accountError)}</p> : null}

      <SecurityNotice
        type="secure"
        title="Your password never leaves your browser"
        description="DevKit signs you in through your system browser using PKCE. The password is never sent to or stored by DevKit."
      />
    </TabColumn>
  );
}

// ----------------------------------------------------------------------
// 2. Appearance Tab — Figma `Settings Content / Appearance` (763:1792)
// ----------------------------------------------------------------------

function FontSettingRow({
  label,
  font,
  size,
  onFont,
  onSize,
  weight,
  onWeight,
}: {
  label: string;
  font: MonoFont;
  size: number;
  onFont: (font: MonoFont) => void;
  onSize: (size: number) => void;
  /** Offered only where the surface follows it (the code view). */
  weight?: CodeFontWeight;
  onWeight?: (weight: CodeFontWeight) => void;
}) {
  return (
    <div className="flex flex-wrap items-end gap-4">
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <span className="text-label-12 text-text-secondary">{label}</span>
        <Select value={font} onValueChange={(value) => onFont(value as MonoFont)}>
          <SelectTrigger aria-label={label} className="h-9 w-full border-border bg-surface-raised text-code-12">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {monoFontOptions.map((option) => (
              <SelectItem
                key={option}
                value={option}
                className="text-code-12"
                style={{ fontFamily: monoFontStack(option) }}
              >
                {option}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      {weight !== undefined && onWeight ? (
        <div className="flex flex-col gap-1.5">
          <span className="text-label-12 text-text-secondary">Weight</span>
          <Select value={String(weight)} onValueChange={(value) => onWeight(normalizeCodeFontWeight(Number(value)))}>
            <SelectTrigger aria-label={`${label} weight`} className="h-9 w-32 border-border bg-surface-raised text-code-12">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {codeFontWeights.map((option) => (
                <SelectItem key={option} value={String(option)} className="text-code-12" style={{ fontWeight: option }}>
                  {codeFontWeightLabel[option]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : null}
      <div className="flex flex-col gap-1.5">
        <span className="text-label-12 text-text-secondary">Size</span>
        <SizeStepper value={size} min={10} max={24} label={`${label} size`} onChange={onSize} />
      </div>
    </div>
  );
}

const THEME_OPTIONS = ["system", "light", "dark"] as const;

function AppearanceTab() {
  const showIndentGuides = useEditorPreferences((s) => s.showIndentGuides);
  const { theme, setTheme, resolvedTheme, family, setFamily } = useTheme();
  const { codeFont, codeSize, codeWeight, terminalFont, terminalSize, setAppearance } = useAppearanceStore(
    useShallow((s) => ({
      codeFont: s.codeFont,
      codeSize: s.codeSize,
      codeWeight: s.codeWeight,
      terminalFont: s.terminalFont,
      terminalSize: s.terminalSize,
      setAppearance: s.setAppearance,
    }))
  );
  const { tabMinWidth, tabMaxWidth, setTabWidths } = useTabSettingsStore(
    useShallow((s) => ({ tabMinWidth: s.minWidth, tabMaxWidth: s.maxWidth, setTabWidths: s.setTabWidths })),
  );

  return (
    <TabColumn gap={7}>
      <SectionHeader
        title="Appearance"
        description="Theme follows your explicit choice, or the OS when set to System."
      />

      <div className="flex flex-col gap-3">
        <GroupLabel>Theme</GroupLabel>
        {/* segmented/theme (763:1799): a surface/well trough with 4px of
            padding; the chosen segment is raised with a subtle stroke. */}
        <RadioGroupPrimitive.Root value={theme} onValueChange={(value) => {
          setTheme(value as typeof theme).catch((error: unknown) => toast.error(`Could not save the theme: ${errorMessage(error)}`));
        }}
          aria-label="Theme"
          className="flex w-fit gap-1 rounded-control border border-border-subtle bg-surface-well p-1"
        >
          {THEME_OPTIONS.map((option) => {
            const active = theme === option;
            return (
              <RadioGroupPrimitive.Item asChild key={option} value={option}>
              <button type="button"
                className={cn(
                  "h-8 rounded-sm border px-4.5 capitalize transition-control focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus/50",
                  active
                    ? "border-border-subtle bg-surface-raised text-body-14-medium text-text-primary"
                    : "border-transparent text-body-14 text-text-secondary hover:text-text-primary",
                )}
              >
                {option}
              </button></RadioGroupPrimitive.Item>
            );
          })}
        </RadioGroupPrimitive.Root>
      </div>

      <div className="flex flex-col gap-3">
        <GroupLabel>Color palette</GroupLabel>
        {/* Swatches, not a dropdown: a palette is the one setting where the
            value IS what it looks like. The chrome stays Mono whichever you
            pick; this changes code, the terminal and coloured details. */}
        <RadioGroupPrimitive.Root value={family} onValueChange={(value) => {
          setFamily(value as ThemeFamilyPreference).catch((error: unknown) => toast.error(`Could not save the color palette: ${errorMessage(error)}`));
        }} aria-label="Color palette" className="flex flex-wrap gap-2">
          {Object.values(FAMILIES).map((definition) => {
            const active = family === definition.id;
            return (
              <RadioGroupPrimitive.Item asChild key={definition.id} value={definition.id}>
              <button type="button" aria-label={definition.label}
                className={cn(
                  "flex w-33 flex-col gap-2 rounded-control border p-2 text-left transition-control focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus/50",
                  active
                    ? "border-border-focus bg-surface-raised"
                    : "border-border-subtle hover:bg-state-soft",
                )}
              >
                <PaletteSwatch family={definition.id} mode={resolvedTheme} />
                <span
                  className={cn(
                    "px-0.5",
                    active ? "text-body-12-medium text-text-primary" : "text-body-12 text-text-secondary",
                  )}
                >
                  {definition.label}
                </span>
              </button></RadioGroupPrimitive.Item>
            );
          })}
        </RadioGroupPrimitive.Root>
      </div>

      <div className="flex flex-col gap-3">
        <GroupLabel>Code &amp; terminal</GroupLabel>
        <div className="flex flex-col gap-4">
          <FontSettingRow
            label="Code font"
            font={codeFont}
            size={codeSize}
            onFont={(font) => setAppearance({ codeFont: font })}
            onSize={(size) => setAppearance({ codeSize: size })}
            weight={normalizeCodeFontWeight(codeWeight)}
            onWeight={(weight) => setAppearance({ codeWeight: weight })}
          />
          <FontSettingRow
            label="Terminal font"
            font={terminalFont}
            size={terminalSize}
            onFont={(font) => setAppearance({ terminalFont: font })}
            onSize={(size) => setAppearance({ terminalSize: size })}
          />
        </div>
        <label className="flex items-center gap-2 text-body-12 text-text-secondary">
          <Checkbox checked={showIndentGuides} onCheckedChange={(checked) => useEditorPreferences.getState().setShowIndentGuides(checked === true)} />
          Show indent guides in code editors
        </label>
        {/* preview (763:1843): the chosen face at the chosen size, on a well. */}
        <p
          className="mt-1.5 rounded-md border border-border-subtle bg-surface-well px-4 py-3 text-text-secondary"
          style={{ fontFamily: monoFontStack(codeFont), fontSize: codeSize, fontWeight: normalizeCodeFontWeight(codeWeight) }}
        >
          {`const vault = unlock(masterKey); // ${codeSize}px ${codeFontWeightLabel[normalizeCodeFontWeight(codeWeight)].toLowerCase()} preview`}
        </p>
      </div>

      {/* Where the tab strip's "Configure Tabs…" lands. */}
      <div className="flex flex-col gap-3">
        <GroupLabel>Tabs</GroupLabel>
        <div className="flex flex-wrap items-end gap-4">
          <div className="flex flex-col gap-1.5">
            <span className="text-label-12 text-text-secondary">Minimum width</span>
            <SizeStepper
              value={tabMinWidth}
              min={TAB_WIDTH_LIMITS.min}
              max={TAB_WIDTH_LIMITS.max}
              step={10}
              label="Minimum tab width"
              onChange={(minWidth) => setTabWidths({ minWidth })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-label-12 text-text-secondary">Maximum width</span>
            <SizeStepper
              value={tabMaxWidth}
              min={TAB_WIDTH_LIMITS.min}
              max={TAB_WIDTH_LIMITS.max}
              step={10}
              label="Maximum tab width"
              onChange={(maxWidth) => setTabWidths({ maxWidth })}
            />
          </div>
        </div>
        <p className="text-body-12 text-text-tertiary">
          Tabs share the strip equally between these widths. Past the minimum, the strip scrolls.
        </p>
      </div>
    </TabColumn>
  );
}

// ----------------------------------------------------------------------
// 3. Sync Tab
// ----------------------------------------------------------------------

function SyncDevicesTab() {
  const queryClient = useQueryClient();
  const [isSyncing, setIsSyncing] = useState(false);
  const [error, setError] = useState("");

  const { data: status, isPending: statusLoading } = useQuery({
    queryKey: ["sync", "status"],
    queryFn: () => syncStatus(),
  });

  async function handleSyncNow() {
    setIsSyncing(true);
    setError("");
    try {
      await syncNow();
      void queryClient.invalidateQueries({ queryKey: ["sync", "status"] });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setIsSyncing(false);
    }
  }

  return (
    <TabColumn gap={7}>
      <div className="flex items-start justify-between gap-4">
        <SectionHeader
          title="Sync"
          description="Where your vault syncs to and which device this is. Items are encrypted here before they leave."
        />
        {/* Not in the Figma section, kept: it is the one way to force a sync
            from here, and dropping a working action to match a mock is a
            regression dressed as fidelity. */}
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={isSyncing}
          onClick={handleSyncNow}
          className="shrink-0"
        >
          <RefreshCw className={cn("size-3.5", isSyncing && "animate-spin")} strokeWidth={1.5} />
          Sync now
        </Button>
      </div>

      <div className="flex flex-col gap-2 rounded-control border border-border-subtle bg-surface-raised px-4 py-3">
        <StatusRow label="Status" value={statusLoading ? "Checking…" : status?.connected ? "Connected" : "Not connected"} />
        <StatusRow label="Endpoint" value={status?.endpoint || "—"} />
        <StatusRow label="This device" value={status?.deviceId || "—"} />
      </div>

      {error ? <p className="text-body-12 text-status-danger">{error}</p> : null}
    </TabColumn>
  );
}

function StatusRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-4">
      <span className="w-24 shrink-0 text-body-12 text-text-tertiary">{label}</span>
      <span className="min-w-0 truncate text-code-12 text-text-secondary" title={value}>
        {value}
      </span>
    </div>
  );
}

// ----------------------------------------------------------------------
// Database Tab — where the module keeps what it writes to disk
// ----------------------------------------------------------------------
function DatabaseTab() {
  return (
    <TabColumn>
      <SectionHeader
        title="Database"
        description="Where the Database module keeps the files it writes. Connections and saved queries are not files — they live in the vault and sync."
      />
      <div className="flex flex-col gap-3">
        <GroupLabel>QUERY CONSOLES</GroupLabel>
        <ConsoleStorageSetting />
      </div>
    </TabColumn>
  );
}

function SecurityTab() {
  return <TabColumn><SectionHeader title="Security" description="How DevKit protects saved credentials on this device." />
    <div className="rounded-xl border border-border-subtle bg-surface-raised p-4"><GroupLabel>Saved credentials</GroupLabel><div className="mt-3"><RevealPasswordSetting /></div></div>
    <SecurityNotice type="secure" title="Your vault stays local" description="DevKit starts locked. Unlocking keeps the decrypted key in memory for the current session." />
  </TabColumn>;
}

function LicensesTab() {
  return <TabColumn><SectionHeader title="Open source licenses" description="Third-party notices for software included in DevKit." /><LicensesSetting /></TabColumn>;
}

function UpdatesTab() {
  return <TabColumn><SectionHeader title="Updates" description="Installed version and release checks. Choose when DevKit checks for new releases." /><UpdatesSetting /></TabColumn>;
}

// ----------------------------------------------------------------------
// 4. Environments Tab — Figma `Settings Content / Environments` (766:1999)
// ----------------------------------------------------------------------
function EnvironmentsTab() {
  const queryClient = useQueryClient();
  const [newOpen, setNewOpen] = useState(false);
  const [newName, setNewName] = useState("");
  const [newRequiresConfirmation, setNewRequiresConfirmation] = useState(false);
  const [busy, setBusy] = useState(false);
  const [mutationError, setMutationError] = useState("");

  const { data: tags = EMPTY_ARRAY, error: queryError } = useQuery({
    queryKey: ["environments", "tags"],
    queryFn: () => listEnvTags(),
  });

  const error = mutationError || (queryError ? errorMessage(queryError) : "");

  async function handleCreateTag(e?: React.FormEvent) {
    if (e) e.preventDefault();
    if (!newName.trim()) return;

    setBusy(true);
    setMutationError("");
    try {
      await createEnvTag(newName.trim().toLowerCase(), newRequiresConfirmation);
      setNewName("");
      setNewRequiresConfirmation(false);
      setNewOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["environments", "tags"] });
    } catch (e) {
      setMutationError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function handleDeleteTag(tag: EnvTag) {
    setMutationError("");
    try {
      await deleteEnvTag(tag.id);
      void queryClient.invalidateQueries({ queryKey: ["environments", "tags"] });
    } catch (e) {
      setMutationError(errorMessage(e));
    }
  }

  return (
    <TabColumn>
      <SectionHeader
        title="Environments"
        description="Env tags required by SSH, Database and Kafka profiles. prod is locked and always requires confirmation."
      />

      {/* tag/* (766:2004): one raised card per tag — the name in Code/12
          Medium, a danger-soft "requires confirmation" chip where it applies,
          and the delete glyph on the right for tags that are not built in. */}
      <div className="flex flex-col gap-2">
        {tags.map((tag) => (
          <div
            key={tag.id}
            className="flex items-center gap-3 rounded-control border border-border-subtle bg-surface-raised py-3 pr-3 pl-3.5"
          >
            <span className="text-code-12-medium text-text-primary">{tag.name}</span>
            <span className="flex-1" />
            {tag.requiresConfirmation ? (
              <span className="flex h-6 items-center gap-1.5 rounded-sm border border-(--border-danger) bg-(--feedback-danger-soft) pr-2.5 pl-2 text-code-11 text-status-danger">
                <LockKeyhole className="size-3.5" strokeWidth={1.5} />
                requires confirmation
              </span>
            ) : null}
            {tag.isBuiltin ? null : (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => void handleDeleteTag(tag)}
                    aria-label={`Delete ${tag.name}`}
                  >
                    <Trash2 className="size-4 text-text-tertiary hover:text-status-danger" strokeWidth={1.5} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Delete environment tag</TooltipContent>
              </Tooltip>
            )}
          </div>
        ))}
      </div>

      <div>
        <Button
          type="button"
          variant="outline"
          size="default"
          className="bg-surface-raised"
          onClick={() => setNewOpen(true)}
        >
          New tag
        </Button>
      </div>

      {error ? <p className="text-body-12 text-status-danger">{error}</p> : null}

      {/* Settings / New Tag Dialog (766:2045): a dialog over the shell, not a
          form unfolding inside the list. */}
      <Dialog open={newOpen} onOpenChange={setNewOpen}>
        <DialogContent className="max-w-114 px-6 pt-5.5 pb-5 sm:max-w-114">
          <form onSubmit={handleCreateTag} className="flex flex-col">
            <DialogHeader>
              <DialogTitle className="text-title-16">New environment tag</DialogTitle>
            </DialogHeader>
            <div className="mt-5 flex flex-col gap-1.5">
              <label className="text-label-12 text-text-secondary">Tag name</label>
              <Input
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="e.g. canary"
                autoFocus
                required
                className="h-9 border-border bg-surface-well text-code-12"
              />
              <span className="mt-0.5 text-body-12 text-text-tertiary">
                Lowercase, no spaces. Must be unique.
              </span>
            </div>
            <label className="mt-4.5 flex cursor-pointer items-center gap-3">
              <Checkbox
                checked={newRequiresConfirmation}
                onCheckedChange={(checked) => setNewRequiresConfirmation(Boolean(checked))}
              />
              <span className="text-body-14 text-text-secondary">
                Requires confirmation before MCP tools can use this tag
              </span>
            </label>
            <DialogFooter className="mt-5.5 gap-2">
              <Button type="button" variant="outline" className="bg-surface-raised" onClick={() => setNewOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={busy || !newName.trim()}>
                {busy ? "Creating…" : "Create tag"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </TabColumn>
  );
}
