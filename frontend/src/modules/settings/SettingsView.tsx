import { useEffect, useState } from "react";
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
  type AuthStatus,
} from "../../bridge/sync";
import { useTheme } from "@/theme/ThemeProvider";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { SecurityNotice } from "@/components/ui/security-notice";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { McpInstallPanel } from "./McpInstallPanel";
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
import {
  User,
  Sun,
  Code,
  Monitor,
  SlidersHorizontal,
  Laptop,
  LockKeyhole,
  LogOut,
  Trash2,
  Plus,
  X,
  RefreshCw,
} from "lucide-react";
import { cn } from "@/lib/utils";

type SettingsTab = "account" | "appearance" | "sync" | "environments";

export interface SettingsModalProps {
  open: boolean;
  onClose: () => void;
}

export function SettingsModal({ open, onClose }: SettingsModalProps) {
  const [activeTab, setActiveTab] = useState<SettingsTab>("account");

  // Handle ESC key press to close modal
  useEffect(() => {
    if (!open) return;
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") {
        onClose();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <TooltipProvider>
      {/* Blurred Scrim Backdrop Overlay */}
      <div
        data-slot="settings-scrim"
        onClick={(e) => {
          if (e.target === e.currentTarget) {
            onClose();
          }
        }}
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 backdrop-blur-[12px] p-4 select-none animate-in fade-in duration-200"
      >
        {/* Settings Modal Frame 940x620px */}
        <div
          data-slot="settings-modal"
          onClick={(e) => e.stopPropagation()}
          className="flex h-[620px] w-full max-w-[940px] flex-col overflow-hidden rounded-[18px] border border-border bg-surface-glass-modal shadow-[0px_20px_48px_-8px_rgba(0,0,0,0.56)] backdrop-blur-[16px] text-text-primary animate-in zoom-in-95 duration-200"
        >
          {/* Modal Header · 56px */}
          <div className="flex h-14 w-full shrink-0 items-center justify-between border-b border-border-subtle px-6">
            <span className="text-[15px] font-medium text-text-primary">Settings</span>

            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="secondary"
                  size="icon"
                  onClick={onClose}
                  aria-label="Close settings"
                  className="size-8 rounded-[8px]"
                >
                  <X className="size-4 text-text-secondary hover:text-text-primary" strokeWidth={1.5} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Close settings (Esc)</TooltipContent>
            </Tooltip>
          </div>

          {/* Modal Body: Sidebar (220px) + Content (656px) */}
          <div className="flex min-h-0 flex-1 overflow-hidden">
            {/* Sidebar Navigation */}
            <div className="flex w-[220px] shrink-0 flex-col gap-1 border-r border-border-subtle bg-surface-raised/80 p-3">
              <NavItem
                active={activeTab === "account"}
                icon={User}
                label="Account"
                onClick={() => setActiveTab("account")}
              />
              <NavItem
                active={activeTab === "appearance"}
                icon={Sun}
                label="Appearance"
                onClick={() => setActiveTab("appearance")}
              />
              <NavItem
                active={activeTab === "sync"}
                icon={Monitor}
                label="Sync & Devices"
                onClick={() => setActiveTab("sync")}
              />
              <NavItem
                active={activeTab === "environments"}
                icon={SlidersHorizontal}
                label="Environments"
                onClick={() => setActiveTab("environments")}
              />
            </div>

            {/* Content Panel Area */}
            <div className="flex flex-1 flex-col overflow-y-auto p-8 min-h-0">
              {activeTab === "account" && <AccountTab />}
              {activeTab === "appearance" && <AppearanceTab />}
              {activeTab === "sync" && <SyncDevicesTab />}
              {activeTab === "environments" && <EnvironmentsTab />}
            </div>
          </div>
        </div>
      </div>
    </TooltipProvider>
  );
}

export function SettingsView() {
  return <SettingsModal open={true} onClose={() => {}} />;
}

function NavItem({
  active,
  icon: Icon,
  label,
  onClick,
}: {
  active: boolean;
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "group relative flex w-full items-center gap-3 rounded-[8px] px-3 py-2 text-[13px] transition-colors outline-none",
        active
          ? "bg-action-primary/10 text-text-primary font-medium"
          : "text-text-secondary hover:bg-surface-base hover:text-text-primary font-normal"
      )}
    >
      {/* 3px Lime Accent line for active nav */}
      <div
        className={cn(
          "h-[18px] w-[3px] rounded-full transition-all shrink-0",
          active ? "bg-action-primary" : "bg-transparent"
        )}
      />

      <Icon
        className={cn("size-4 shrink-0 transition-opacity", active ? "opacity-100" : "opacity-60")}
        strokeWidth={1.5}
      />

      <span className="truncate">{label}</span>
    </button>
  );
}

// ----------------------------------------------------------------------
// 1. Account Tab
// ----------------------------------------------------------------------
function AccountTab() {
  const [auth, setAuth] = useState<AuthStatus>({ authenticated: false, expiresAt: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function loadAuth() {
    try {
      const s = await authStatus();
      setAuth(s);
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  useEffect(() => {
    void loadAuth();
  }, []);

  async function handleSignOut() {
    setBusy(true);
    setError("");
    try {
      await authLogout();
      await loadAuth();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function handleSignIn() {
    setBusy(true);
    setError("");
    try {
      await syncConnect();
      await loadAuth();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-6 max-w-[560px]">
      <div className="flex flex-col gap-2">
        <h2 className="text-[28px] font-semibold tracking-tight text-text-primary">Account</h2>
        <p className="text-[14px] leading-relaxed text-text-secondary">
          Identity used for sync. Managed by your identity provider — not stored locally.
        </p>
      </div>

      {/* Identity Card */}
      <div className="flex items-center gap-4 py-2">
        <div className="flex size-12 shrink-0 items-center justify-center rounded-full bg-action-primary text-action-primary-foreground font-semibold text-base select-none">
          SY
        </div>

        <div className="flex flex-col gap-1 min-w-0">
          <span className="text-[14px] font-medium text-text-primary truncate">
            {auth.authenticated ? "syuro.dev@gmail.com" : "Not signed in"}
          </span>

          <div className="flex items-center gap-2">
            <div
              className={cn(
                "size-1.5 rounded-full shrink-0",
                auth.authenticated ? "bg-status-success" : "bg-text-tertiary"
              )}
            />
            <span className="text-[13px] text-text-secondary">
              {auth.authenticated ? "Signed in — session active" : "Offline / Local only"}
            </span>
          </div>
        </div>
      </div>

      {/* Action Button */}
      <div>
        {auth.authenticated ? (
          <Button
            type="button"
            variant="secondary"
            size="default"
            disabled={busy}
            onClick={handleSignOut}
          >
            <LogOut className="size-3.5" strokeWidth={1.5} />
            <span>{busy ? "Signing out…" : "Sign out"}</span>
          </Button>
        ) : (
          <Button
            type="button"
            variant="default"
            size="default"
            disabled={busy}
            onClick={handleSignIn}
          >
            <span>{busy ? "Connecting browser…" : "Sign in with browser"}</span>
          </Button>
        )}
      </div>

      {error ? <p className="text-[12px] text-status-danger">{error}</p> : null}

      {/* Security Notice */}
      <SecurityNotice
        type="secure"
        title="Password never leaves your browser"
        description="DevKit signs you in through your system browser (PKCE). Your password is never sent to or stored by DevKit."
      />
    </div>
  );
}

// ----------------------------------------------------------------------
// 2. Appearance Tab
// ----------------------------------------------------------------------
function FontSettingRow({
  label,
  font,
  size,
  onFont,
  onSize,
}: {
  label: string;
  font: MonoFont;
  size: number;
  onFont: (font: MonoFont) => void;
  onSize: (size: number) => void;
}) {
  return (
    <div className="flex items-end gap-3">
      <div className="flex flex-col gap-1.5">
        <span className="text-[12px] text-text-secondary">{label}</span>
        <Select value={font} onValueChange={(value) => onFont(value as MonoFont)}>
          <SelectTrigger className="w-[220px] font-mono-devkit text-[13px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {monoFontOptions.map((option) => (
              <SelectItem
                key={option}
                value={option}
                className="font-mono-devkit text-[13px]"
                style={{ fontFamily: monoFontStack(option) }}
              >
                {option}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-1.5">
        <span className="text-[12px] text-text-secondary">Size</span>
        <Input
          type="number"
          min={10}
          max={24}
          value={size}
          onChange={(e) => {
            const next = Number(e.target.value);
            if (Number.isFinite(next)) onSize(Math.min(24, Math.max(10, next)));
          }}
          className="w-[72px] font-mono-devkit text-[13px]"
        />
      </div>
    </div>
  );
}

function AppearanceTab() {
  const { theme, setTheme } = useTheme();
  const { codeFont, codeSize, terminalFont, terminalSize, setAppearance } = useAppearanceStore();

  return (
    <div className="flex flex-col gap-6 max-w-[560px]">
      <div className="flex flex-col gap-2">
        <h2 className="text-[28px] font-semibold tracking-tight text-text-primary">Appearance</h2>
        <p className="text-[14px] leading-relaxed text-text-secondary">
          Theme follows your explicit choice, or the OS when set to System.
        </p>
      </div>

      <div className="flex flex-col gap-3 pt-2">
        <div className="flex items-center gap-2 text-action-primary">
          <Sun className="size-3.5" strokeWidth={1.5} />
          <span className="text-[10px] font-medium tracking-wider uppercase">THEME</span>
        </div>

        <div className="flex items-center gap-3">
          {(["dark", "light", "system"] as const).map((option) => (
            <Button
              key={option}
              type="button"
              variant={theme === option ? "default" : "secondary"}
              size="default"
              onClick={() => void setTheme(option)}
              className="w-20 capitalize"
            >
              {option}
            </Button>
          ))}
        </div>
      </div>

      <div className="flex flex-col gap-4 pt-2">
        <div className="flex items-center gap-2 text-action-primary">
          <Code className="size-3.5" strokeWidth={1.5} />
          <span className="text-[10px] font-medium tracking-wider uppercase">CODE & TERMINAL</span>
        </div>

        <FontSettingRow
          label="Code font"
          font={codeFont}
          size={codeSize}
          onFont={(font) => setAppearance({ codeFont: font })}
          onSize={(size) => setAppearance({ codeSize: size })}
        />
        <FontSettingRow
          label="Terminal font"
          font={terminalFont}
          size={terminalSize}
          onFont={(font) => setAppearance({ terminalFont: font })}
          onSize={(size) => setAppearance({ terminalSize: size })}
        />

        <p
          className="text-text-tertiary"
          style={{ fontFamily: monoFontStack(codeFont), fontSize: codeSize }}
        >
          {`const vault = unlock(masterKey); // ${codeSize}px preview`}
        </p>
      </div>
    </div>
  );
}

// ----------------------------------------------------------------------
// 3. Sync & Devices Tab
// ----------------------------------------------------------------------
function SyncDevicesTab() {
  const [isSyncing, setIsSyncing] = useState(false);
  const [error, setError] = useState("");

  async function loadSync() {
    try {
      await syncStatus();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  useEffect(() => {
    void loadSync();
  }, []);

  async function handleSyncNow() {
    setIsSyncing(true);
    setError("");
    try {
      await syncNow();
      await loadSync();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setIsSyncing(false);
    }
  }

  return (
    <div className="flex flex-col gap-6 w-full max-w-[656px]">
      <div className="flex items-start justify-between">
        <div className="flex flex-col gap-2 max-w-[500px]">
          <h2 className="text-[28px] font-semibold tracking-tight text-text-primary">
            Sync & Devices
          </h2>
          <p className="text-[14px] leading-relaxed text-text-secondary">
            Devices with access to your synced vault. Revoking a device blocks it until it signs in again.
          </p>
        </div>

        <Button
          type="button"
          variant="secondary"
          size="sm"
          disabled={isSyncing}
          onClick={handleSyncNow}
          className="gap-1.5"
        >
          <RefreshCw className={cn("size-3.5", isSyncing && "animate-spin")} strokeWidth={1.5} />
          <span>Sync now</span>
        </Button>
      </div>

      {/* Device List */}
      <div className="flex flex-col divide-y divide-border-subtle rounded-[14px] border border-border bg-surface-base/60 overflow-hidden">
        {/* Device 1: Current MacBook Pro */}
        <div className="flex items-center justify-between p-4">
          <div className="flex items-center gap-3.5">
            <div className="flex size-9 items-center justify-center rounded-[8px] bg-surface-raised border border-border-subtle">
              <Laptop className="size-4 text-text-primary" strokeWidth={1.5} />
            </div>
            <div className="flex flex-col gap-0.5">
              <span className="text-[14px] font-medium text-text-primary">MacBook Pro</span>
              <span className="text-[12px] text-status-success font-medium">
                This device · Active now
              </span>
            </div>
          </div>
          <span className="rounded-full bg-status-success/15 px-2.5 py-1 text-[11px] font-medium text-status-success">
            Current
          </span>
        </div>

        {/* Device 2: Mac mini */}
        <div className="flex items-center justify-between p-4">
          <div className="flex items-center gap-3.5">
            <div className="flex size-9 items-center justify-center rounded-[8px] bg-surface-raised border border-border-subtle">
              <Monitor className="size-4 text-text-secondary" strokeWidth={1.5} />
            </div>
            <div className="flex flex-col gap-0.5">
              <span className="text-[14px] font-medium text-text-primary">Mac mini (office)</span>
              <span className="text-[12px] text-text-tertiary">Last active 2 days ago</span>
            </div>
          </div>
          <Button type="button" variant="secondary" size="xs">
            Revoke
          </Button>
        </div>

        {/* Device 3: Studio setup */}
        <div className="flex items-center justify-between p-4">
          <div className="flex items-center gap-3.5">
            <div className="flex size-9 items-center justify-center rounded-[8px] bg-surface-raised border border-border-subtle">
              <Monitor className="size-4 text-text-secondary" strokeWidth={1.5} />
            </div>
            <div className="flex flex-col gap-0.5">
              <span className="text-[14px] font-medium text-text-primary">Studio setup</span>
              <span className="text-[12px] text-text-tertiary">Last active 19 days ago</span>
            </div>
          </div>
          <Button type="button" variant="secondary" size="xs">
            Revoke
          </Button>
        </div>
      </div>

      {error ? <p className="text-[12px] text-status-danger">{error}</p> : null}

      {/* Security Notice */}
      <SecurityNotice
        type="irreversible"
        title="Revoking a device invalidates its session"
        description="A revoked device is immediately blocked from synchronizing or reading newly updated vault items."
      />
    </div>
  );
}

// ----------------------------------------------------------------------
// 4. Environments Tab
// ----------------------------------------------------------------------
function EnvironmentsTab() {
  const [tags, setTags] = useState<EnvTag[]>([]);
  const [showNewForm, setShowNewForm] = useState(false);
  const [newName, setNewName] = useState("");
  const [newRequiresConfirmation, setNewRequiresConfirmation] = useState(false);
  const [busy, setBusy] = useState(false);
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

  async function handleCreateTag(e?: React.FormEvent) {
    if (e) e.preventDefault();
    if (!newName.trim()) return;

    setBusy(true);
    setError("");
    try {
      await createEnvTag(newName.trim().toLowerCase(), newRequiresConfirmation);
      setNewName("");
      setNewRequiresConfirmation(false);
      setShowNewForm(false);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function handleDeleteTag(tag: EnvTag) {
    setError("");
    try {
      await deleteEnvTag(tag.id);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  return (
    <div className="flex flex-col gap-6 w-full max-w-[656px]">
      <div className="flex flex-col gap-2">
        <h2 className="text-[28px] font-semibold tracking-tight text-text-primary">
          Environments
        </h2>
        <p className="text-[14px] leading-relaxed text-text-secondary">
          Env tags required by SSH, Database, and Kafka profiles. prod is locked and always requires confirmation.
        </p>
      </div>

      {/* Tag List */}
      <div className="flex flex-col divide-y divide-border-subtle rounded-[14px] border border-border bg-surface-base/60 overflow-hidden">
        {tags.map((tag) => {
          const isProd = tag.name === "prod";

          return (
            <div key={tag.id} className="flex h-[48px] items-center justify-between px-4">
              <div className="flex items-center gap-3">
                <span className="font-mono text-[14px] font-medium text-text-primary">
                  {tag.name}
                </span>

                {isProd && (
                  <div className="flex items-center gap-1.5 rounded-md bg-status-warning/10 border border-status-warning/30 px-2 py-0.5 text-[11px] font-medium text-status-warning">
                    <LockKeyhole className="size-3" strokeWidth={1.5} />
                    <span>REQUIRES CONFIRMATION</span>
                  </div>
                )}
              </div>

              {!tag.isBuiltin && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-xs"
                      onClick={() => void handleDeleteTag(tag)}
                      aria-label={`Delete ${tag.name}`}
                    >
                      <Trash2 className="size-3.5 text-text-tertiary hover:text-status-danger" strokeWidth={1.5} />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>Delete environment tag</TooltipContent>
                </Tooltip>
              )}
            </div>
          );
        })}
      </div>

      {/* New Tag Form */}
      {showNewForm ? (
        <form
          onSubmit={handleCreateTag}
          className="flex flex-col gap-4 rounded-[14px] border border-border bg-surface-base p-4.5"
        >
          <span className="text-[14px] font-semibold text-text-primary">
            New environment tag
          </span>

          <div className="flex flex-col gap-1.5 max-w-[360px]">
            <label className="text-[12px] font-medium text-text-secondary">Tag name</label>
            <Input
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="e.g. canary, uat, sandbox"
              autoFocus
              required
              className="font-mono text-[13px]"
            />
          </div>

          <label className="flex items-center gap-2.5 cursor-pointer py-1">
            <Checkbox
              checked={newRequiresConfirmation}
              onCheckedChange={(checked) => setNewRequiresConfirmation(Boolean(checked))}
            />
            <span className="text-[13px] text-text-primary">
              Require confirmation before executing actions
            </span>
          </label>

          <div className="flex items-center gap-2.5 pt-1">
            <Button
              type="button"
              variant="secondary"
              size="default"
              onClick={() => setShowNewForm(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !newName.trim()} size="default">
              <span>{busy ? "Adding…" : "Add environment"}</span>
            </Button>
          </div>
        </form>
      ) : (
        <div>
          <Button
            type="button"
            variant="default"
            size="default"
            onClick={() => setShowNewForm(true)}
            className="gap-2"
          >
            <Plus className="size-3.5" strokeWidth={2} />
            <span>Add tag</span>
          </Button>
        </div>
      )}

      {error ? <p className="text-[12px] text-status-danger">{error}</p> : null}

      {/* MCP Proxy Install section */}
      <div className="pt-4 border-t border-border-subtle">
        <McpInstallPanel />
      </div>
    </div>
  );
}
