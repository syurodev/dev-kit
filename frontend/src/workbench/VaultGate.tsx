import { useEffect, useState, type ReactNode } from "react";
import { Events, Window } from "@wailsio/runtime";
import {
  setupVault,
  unlockVault,
  unlockVaultWithRecovery,
  vaultStatus,
  type VaultState,
} from "../bridge/vault";
import { errorMessage } from "../bridge/errors";
import { onVaultUnlockRequested } from "./vaultUnlockRequest";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { RecoveryKeyField } from "@/components/ui/recovery-key-field";
import { Titlebar } from "@/components/ui/titlebar";
import { VaultStage, VaultPasswordField as PasswordField, VAULT_FIELD as FIELD } from "./VaultStage";
import { cn } from "@/lib/utils";
import { shellSquircleRef } from "@/lib/shellSquircle";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  KeyRound,
  Loader2,
  LockKeyhole,
  RotateCcw,
  TriangleAlert,
  Vault,
} from "lucide-react";

type GateStep =
  | "checking"
  | "setup_password"
  | "setup_recovery"
  | "setup_ready"
  | "unlock_password"
  | "unlock_recovery";

/** Figma `Vault Stage` (747:1385): the copy around each step's content slot. */
const COPY: Record<GateStep, { eyebrow: string; title: string; body: string; footnote: string; eyebrowClass?: string }> = {
  checking: {
    eyebrow: "Vault",
    title: "Checking secure storage",
    body: "DevKit is verifying the local vault state before the workbench is mounted.",
    footnote: "No workspace data is exposed during this check.",
  },
  setup_password: {
    eyebrow: "First-run setup",
    title: "Create a master password",
    body: "This password encrypts the local vault. It stays on this device and is never available to modules, agents, or MCP.",
    footnote: "Your password never leaves this device.",
  },
  setup_recovery: {
    eyebrow: "Shown exactly once",
    title: "Save your recovery key",
    body: "This key is not logged, audited, or persisted by DevKit. Store it offline before continuing.",
    footnote: "Losing both the password and this key means the vault cannot be opened.",
  },
  setup_ready: {
    eyebrow: "Vault",
    title: "Vault is ready",
    body: "Your local vault is unlocked for this session. It will start locked the next time DevKit launches.",
    footnote: "Locking the vault zeroizes the decrypted key held in memory.",
  },
  unlock_password: {
    eyebrow: "Vault",
    title: "Unlock your vault",
    body: "Every DevKit process starts locked. Unlocking keeps the decrypted key in memory for this session only.",
    footnote: "Your password never leaves this device.",
  },
  unlock_recovery: {
    eyebrow: "Recovery access",
    title: "Use your recovery key",
    body: "Recovery unlock opens the existing vault. It does not change your password or rotate the recovery key.",
    footnote: "Case does not matter. Spaces are ignored.",
    eyebrowClass: "text-status-recovery",
  },
};

const LABEL = "text-body-13-medium text-text-secondary";
const WIDE = "h-11 w-full text-body-14-medium [&_svg]:size-4";

/** Figma `alert` (752:1481): danger-soft, a 20px glyph, title over a note. */
function ErrorAlert({ title, note }: { title: string; note: string }) {
  return (
    <div id="vault-form-error" role="alert" className="flex w-full items-start gap-3 rounded-md border border-(--border-danger) bg-(--feedback-danger-soft) px-4 py-3.5">
      <TriangleAlert className="size-5 shrink-0 text-status-danger" strokeWidth={1.5} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-body-14-medium text-text-primary">{title}</span>
        <span className="text-body-12 text-text-tertiary">{note}</span>
      </span>
    </div>
  );
}

/**
 * The vault gate is not an overlay: this returns INSTEAD of the workbench, so
 * nothing renders behind it. The shared stage is opaque, bounded to 480px
 * and scrolls safely on short windows. Only the step content fades/translates;
 * credentials remain owned by this gate.
 */
export function VaultGate({ children }: { children: ReactNode }) {
  const [gateStep, setGateStep] = useState<GateStep>("checking");
  const [vaultState, setVaultState] = useState<VaultState | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);

  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [recoveryKeyInput, setRecoveryKeyInput] = useState("");
  const [isRecoverySaved, setIsRecoverySaved] = useState(false);
  const [generatedRecoveryKey, setGeneratedRecoveryKey] = useState("");

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState("");

  // A module that hits a locked vault mid-session asks for this rather than
  // rendering its own unlock form.
  useEffect(() => onVaultUnlockRequested(() => void checkStatus()), []);

  async function checkStatus() {
    setError("");
    try {
      const state = await vaultStatus();
      setVaultState(state);
      if (state === "not_configured") {
        setGateStep("setup_password");
      } else if (state === "locked") {
        setGateStep("unlock_password");
      } else if (state === "unlocked") {
        setGateStep("setup_ready");
      }
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  useEffect(() => {
    void checkStatus();
  }, []);

  useEffect(() => {
    let mounted = true;
    let receivedFullscreenEvent = false;

    const updateFullscreen = (fullscreen: boolean) => {
      receivedFullscreenEvent = true;
      setIsFullscreen(fullscreen);
    };

    const offWillFullscreen = Events.On(Events.Types.Mac.WindowWillEnterFullScreen, () => {
      updateFullscreen(true);
    });
    const offWillUnFullscreen = Events.On(Events.Types.Mac.WindowWillExitFullScreen, () => {
      updateFullscreen(false);
    });
    const offFullscreen = Events.On(Events.Types.Common.WindowFullscreen, () => {
      updateFullscreen(true);
    });
    const offUnFullscreen = Events.On(Events.Types.Common.WindowUnFullscreen, () => {
      updateFullscreen(false);
    });

    void Window.IsFullscreen()
      .then((fullscreen) => {
        if (mounted && !receivedFullscreenEvent) {
          setIsFullscreen(fullscreen);
        }
      })
      .catch(() => {});

    return () => {
      mounted = false;
      offWillFullscreen();
      offWillUnFullscreen();
      offFullscreen();
      offUnFullscreen();
    };
  }, []);

  async function handleSetupPassword(e?: React.FormEvent) {
    if (e) e.preventDefault();
    if (isSubmitting) return;
    setError("");

    if (password.length < 8) {
      setError("Master password must be at least 8 characters long.");
      return;
    }

    if (password !== confirmPassword) {
      setError("Passwords do not match. Please re-enter.");
      return;
    }

    setIsSubmitting(true);
    try {
      const key = await setupVault(password);
      setGeneratedRecoveryKey(key);
      setPassword("");
      setConfirmPassword("");
      setGateStep("setup_recovery");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setIsSubmitting(false);
    }
  }

  function handleConfirmRecovery() {
    if (!isRecoverySaved) return;
    setGateStep("setup_ready");
  }

  async function handleUnlockPassword(e?: React.FormEvent) {
    if (e) e.preventDefault();
    if (isSubmitting) return;
    if (!password) {
      setError("Please enter your master password.");
      return;
    }

    setError("");
    setIsSubmitting(true);
    try {
      await unlockVault(password);
      setPassword("");
      setVaultState("unlocked");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setIsSubmitting(false);
    }
  }

  async function handleUnlockRecovery(e?: React.FormEvent) {
    if (e) e.preventDefault();
    if (isSubmitting) return;
    const cleanKey = recoveryKeyInput.trim();
    if (!cleanKey) {
      setError("Please enter your recovery key.");
      return;
    }

    setError("");
    setIsSubmitting(true);
    try {
      await unlockVaultWithRecovery(cleanKey);
      setRecoveryKeyInput("");
      setVaultState("unlocked");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setIsSubmitting(false);
    }
  }

  // If already unlocked and not in middle of initial setup walkthrough, mount workbench
  if (vaultState === "unlocked" && gateStep !== "setup_ready" && gateStep !== "setup_recovery") {
    // One transition, not a cut (smoothness 8.1). `display: contents` on the
    // wrapper so the workbench stays the same flex child it was — the fade is
    // on the shell itself, which arrives whole rather than part by part.
    return <div className="shell-enter">{children}</div>;
  }

  const contextualLabel =
    gateStep === "setup_password"
      ? "Setup 1 of 3"
      : gateStep === "setup_recovery"
        ? "Setup 2 of 3"
        : gateStep === "setup_ready"
          ? "Setup 3 of 3"
          : "Secure access";

  const copy = COPY[gateStep];

  return (
    <div className="app-shell relative flex h-svh w-full flex-col overflow-hidden text-text-primary select-none">
      {/* Same 40px bar as the workbench, deliberately: the native traffic
          lights do not move when the gate hands over, so a 30px bar here and a
          40px bar there could not both keep them aligned. No app actions on
          the right (Figma 748:1391): there is no workbench yet to act on. */}
      <Titlebar fullscreen={isFullscreen}>
        <div className="flex min-w-0 items-center gap-2">
          <Vault className="size-4 text-text-secondary" strokeWidth={1.5} />
          <span className="text-label-12 text-text-secondary">Vault</span>
        </div>
        {/* Centred across the whole bar, not the padded content box — the
            84px traffic-light reservation would otherwise shift it 34px right
            of the window's true centre. pointer-events-none so it never eats
            a drag on the title bar. */}
        <div className="pointer-events-none absolute inset-x-0 flex justify-center">
          <span className="text-label-11-caps text-text-tertiary">{contextualLabel}</span>
        </div>
      </Titlebar>

      <div className="mx-1 mb-1 flex min-h-0 flex-1">
        <div ref={shellSquircleRef} className="shell-surface flex min-h-0 min-w-0 flex-1 overflow-hidden">
          <div className="flex min-h-0 min-w-0 flex-1 flex-col items-center overflow-auto px-5 py-8 sm:p-8">
          <VaultStage step={gateStep} {...copy} setupStep={gateStep === "setup_password" ? 1 : gateStep === "setup_recovery" ? 2 : gateStep === "setup_ready" ? 3 : undefined}>
              {gateStep === "checking" && (
                <div role="status" className="flex min-h-40 flex-col items-center justify-center gap-2">
                  {error ? (
                    <>
                      <ErrorAlert title="Could not read the vault state" note={error} />
                      <Button variant="outline" className={cn(WIDE, "mt-4 bg-surface-raised")} onClick={() => void checkStatus()}>
                        <RotateCcw />
                        Try again
                      </Button>
                    </>
                  ) : (
                    <>
                      <span className="flex items-center gap-3 text-body-16 text-text-secondary">
                        <Loader2 aria-hidden className="size-5 animate-spin motion-reduce:animate-none" />
                        Checking secure storage status…
                      </span>
                      <span className="text-caption-11 text-text-tertiary">
                        No workspace data is exposed during this check.
                      </span>
                    </>
                  )}
                </div>
              )}

              {gateStep === "setup_password" && (
                <form onSubmit={handleSetupPassword} className="flex w-full flex-col">
                  {error && (
                    <div className="mb-5 w-full">
                      <ErrorAlert title={error} note="Nothing was saved — fix the password and try again." />
                    </div>
                  )}
                  <PasswordField
                    label="Master password"
                    value={password}
                    onChange={setPassword}
                    creating
                    placeholder="At least 8 characters"
                    autoFocus
                    disabled={isSubmitting}
                    invalid={!!error}
                  />
                  <div className="mt-4">
                    <PasswordField
                      label="Confirm password"
                      value={confirmPassword}
                      onChange={setConfirmPassword}
                      creating
                      disabled={isSubmitting}
                      placeholder="Repeat your password"
                      invalid={!!error}
                    />
                  </div>
                  <p className="mt-3 text-body-12 text-text-tertiary">
                    At least 8 characters. This password is never stored — losing it means using the
                    recovery key.
                  </p>
                  <Button type="submit" className={cn(WIDE, "mt-6")} disabled={isSubmitting || !password || !confirmPassword}>
                    <LockKeyhole />
                    {isSubmitting ? "Creating vault…" : "Create vault"}
                  </Button>
                </form>
              )}

              {gateStep === "setup_recovery" && (
                <div className="flex w-full flex-col">
                  <RecoveryKeyField recoveryKey={generatedRecoveryKey} defaultRevealed />
                  <label className="mt-4 flex cursor-pointer items-center gap-2.5 select-none">
                    <Checkbox
                      checked={isRecoverySaved}
                      onCheckedChange={(checked) => setIsRecoverySaved(Boolean(checked))}
                    />
                    <span className="text-body-13 text-text-secondary">
                      I have saved this key somewhere I can reach without DevKit
                    </span>
                  </label>
                  <Button type="button" className={cn(WIDE, "mt-5")} onClick={handleConfirmRecovery} disabled={!isRecoverySaved}>
                    <ArrowRight />
                    Continue
                  </Button>
                </div>
              )}

              {gateStep === "setup_ready" && (
                <div className="flex w-full flex-col items-center">
                  <span className="flex items-center gap-2.5 text-body-16 text-text-primary">
                    <Check className="size-5 text-status-success" strokeWidth={2} />
                    Vault created and unlocked for this session
                  </span>
                  <Button
                    type="button"
                    className={cn(WIDE, "mt-6")}
                    onClick={() => {
                      setGeneratedRecoveryKey("");
                      setIsRecoverySaved(false);
                      setVaultState("unlocked");
                      setGateStep("checking");
                    }}
                  >
                    <ArrowRight />
                    Open workbench
                  </Button>
                </div>
              )}

              {gateStep === "unlock_password" && (
                <form onSubmit={handleUnlockPassword} className="flex w-full flex-col">
                  {error && (
                    <div className="mb-5 w-full">
                      <ErrorAlert
                        title="That password did not unlock the vault"
                        note="Nothing was sent anywhere — the check runs on this device."
                      />
                    </div>
                  )}
                  <PasswordField
                    label="Master password"
                    value={password}
                    onChange={setPassword}
                    placeholder="Enter master password"
                    autoFocus
                    disabled={isSubmitting}
                    invalid={!!error}
                  />
                  <Button type="submit" className={cn(WIDE, "mt-6")} disabled={isSubmitting || !password}>
                    {error ? <RotateCcw /> : <LockKeyhole />}
                    {isSubmitting ? "Unlocking…" : error ? "Try again" : "Unlock vault"}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={isSubmitting}
                    className={cn(WIDE, "mt-2 bg-surface-raised")}
                    onClick={() => {
                      setError("");
                      setPassword("");
                      setGateStep("unlock_recovery");
                    }}
                  >
                    <KeyRound />
                    Use recovery key instead
                  </Button>
                </form>
              )}

              {gateStep === "unlock_recovery" && (
                <form onSubmit={handleUnlockRecovery} className="flex w-full flex-col">
                  {error && (
                    <div className="mb-5 w-full">
                      <ErrorAlert title="That key did not unlock the vault" note={error} />
                    </div>
                  )}
                  <label className="flex w-full flex-col gap-1.5">
                    <span className={LABEL}>Recovery key</span>
                    <span className={cn(FIELD, "pr-4", error ? "border-(--border-danger)" : "border-border")}>
                      <input
                        type="text"
                        value={recoveryKeyInput}
                        onChange={(e) => setRecoveryKeyInput(e.target.value)}
                        placeholder="DVK7-XXXX-XXXX-XXXX-XXXX-XXXX"
                        autoFocus
                        required
                        disabled={isSubmitting}
                        autoComplete="off"
                        aria-invalid={!!error || undefined}
                        aria-describedby={error ? "vault-form-error" : undefined}
                        spellCheck={false}
                        className="min-w-0 flex-1 bg-transparent outline-none"
                      />
                    </span>
                  </label>
                  <p className="mt-3 text-body-12 text-text-tertiary">
                    Enter the 29-character recovery key saved during first-run setup.
                  </p>
                  <Button type="submit" className={cn(WIDE, "mt-6")} disabled={isSubmitting || !recoveryKeyInput.trim()}>
                    <KeyRound />
                    {isSubmitting ? "Unlocking…" : "Unlock with recovery key"}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={isSubmitting}
                    className={cn(WIDE, "mt-2 bg-surface-raised")}
                    onClick={() => {
                      setError("");
                      setRecoveryKeyInput("");
                      setGateStep("unlock_password");
                    }}
                  >
                    <ArrowLeft />
                    Back to password
                  </Button>
                </form>
              )}
          </VaultStage>
          </div>
        </div>
      </div>
    </div>
  );
}
