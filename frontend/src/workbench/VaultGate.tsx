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
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Alert, AlertTitle, AlertDescription, AlertContent } from "@/components/ui/alert";
import { StepProgress } from "@/components/ui/step-progress";
import { LoadingStatus } from "@/components/ui/loading-status";
import { SecurityNotice } from "@/components/ui/security-notice";
import { RecoveryKeyField } from "@/components/ui/recovery-key-field";
import {
  Vault,
  ShieldCheck,
  ArrowRight,
  Eye,
  EyeOff,
  Check,
  KeyRound,
  LockKeyhole,
} from "lucide-react";
import { cn } from "@/lib/utils";

type GateStep =
  | "checking"
  | "setup_password"
  | "setup_recovery"
  | "setup_ready"
  | "unlock_password"
  | "unlock_recovery";

export function VaultGate({ children }: { children: ReactNode }) {
  const [gateStep, setGateStep] = useState<GateStep>("checking");
  const [vaultState, setVaultState] = useState<VaultState | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);

  // Form states
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [recoveryKeyInput, setRecoveryKeyInput] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [showConfirmPassword, setShowConfirmPassword] = useState(false);
  const [isRecoverySaved, setIsRecoverySaved] = useState(false);
  const [generatedRecoveryKey, setGeneratedRecoveryKey] = useState("");

  // Loading & error states
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState("");

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

  // Step 1: Create Master Password
  async function handleSetupPassword(e?: React.FormEvent) {
    if (e) e.preventDefault();
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

  // Step 2: Acknowledge Recovery Key Saved
  function handleConfirmRecovery() {
    if (!isRecoverySaved) return;
    setGateStep("setup_ready");
  }

  // Returning user: Unlock with password
  async function handleUnlockPassword(e?: React.FormEvent) {
    if (e) e.preventDefault();
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

  // Returning user: Unlock with recovery key
  async function handleUnlockRecovery(e?: React.FormEvent) {
    if (e) e.preventDefault();
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
    return <>{children}</>;
  }

  // Top contextual label for title bar
  const getContextualLabel = () => {
    switch (gateStep) {
      case "setup_password":
        return "SETUP 1 OF 4";
      case "setup_recovery":
        return "SETUP 2 OF 4";
      case "setup_ready":
        return "SETUP 4 OF 4";
      default:
        return "SECURE ACCESS";
    }
  };

  return (
    <div className="relative flex h-svh w-full flex-col overflow-hidden bg-transparent text-text-primary select-none">
      {/* 30px Title Bar · Full Drag Region · Padded for macOS Traffic Lights */}
      <header
        data-slot="title-bar"
        className={cn(
          "relative flex h-[30px] shrink-0 items-center pt-0.5 pr-4 transition-[padding-left] duration-300 ease-out motion-reduce:transition-none [--wails-draggable:drag]",
          isFullscreen ? "pl-1" : "pl-20",
        )}
      >
        <div className="grid h-full w-full grid-cols-[minmax(0,1fr)_minmax(0,5fr)_minmax(0,1fr)] items-center gap-2">
          {/* Left · Status */}
          <div className="flex min-w-0 items-center gap-2">
            <Vault className="size-3.5 text-text-secondary" strokeWidth={1.5} />
            <span className="text-[11px] font-medium text-text-secondary">Vault</span>
          </div>

          {/* Center · Contextual Status */}
          <div className="flex min-w-0 items-center justify-center">
            <span className="text-[10px] font-medium tracking-[0.6px] text-text-tertiary uppercase">
              {getContextualLabel()}
            </span>
          </div>

          {/* Right · Global Actions */}
          <div className="flex min-w-0 items-center justify-end gap-2">
            <span className="text-[10px] font-medium tracking-wide text-status-success uppercase">
              LOCAL ONLY
            </span>
            <ShieldCheck className="size-3.5 text-status-success" strokeWidth={1.5} />
          </div>
        </div>
      </header>

      {/* Main Viewport Container with rounded border matching Workbench layout */}
      <div className="mx-1 mb-1 flex min-h-0 flex-1">
        <div className="relative flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto rounded-xl border border-border bg-card p-4 md:p-8">
          {/* State 1: Checking status */}
        {gateStep === "checking" && (
          <div className="flex w-full max-w-[640px] flex-col gap-6 rounded-[18px] border border-border bg-surface-glass-modal p-8 md:p-12 shadow-[0px_20px_48px_-8px_rgba(0,0,0,0.48)] backdrop-blur-[14px]">
            <div className="flex flex-col gap-2">
              <span className="text-[11px] font-medium tracking-[0.77px] text-action-primary uppercase">
                VAULT
              </span>
              <h1 className="text-[28px] font-semibold tracking-tight text-text-primary">
                Checking secure storage
              </h1>
              <p className="text-[14px] font-normal leading-relaxed text-text-secondary">
                DevKit is verifying the local vault state before the workbench is mounted.
              </p>
            </div>

            <LoadingStatus text="Checking secure storage status…" />

            <p className="text-[12px] font-normal text-text-tertiary">
              No workspace data is exposed during this check.
            </p>
          </div>
        )}

        {/* State 2: First-run Setup - Step 1: Create Master Password */}
        {gateStep === "setup_password" && (
          <div className="flex w-full max-w-[760px] flex-col gap-6 rounded-[18px] border border-border bg-surface-glass-modal p-8 md:p-12 shadow-[0px_20px_48px_-8px_rgba(0,0,0,0.48)] backdrop-blur-[14px]">
            <div className="flex flex-col gap-2">
              <span className="text-[11px] font-medium tracking-[0.77px] text-action-primary uppercase">
                FIRST-RUN SETUP
              </span>
              <h1 className="text-[28px] font-semibold tracking-tight text-text-primary">
                Create a master password
              </h1>
              <p className="text-[14px] font-normal leading-relaxed text-text-secondary">
                This password encrypts the local vault. It stays on this device and is never
                available to modules, agents, or MCP.
              </p>
            </div>

            <div className="flex w-full justify-center">
              <StepProgress currentStep={1} />
            </div>

            <form onSubmit={handleSetupPassword} className="flex flex-col gap-5">
              <div className="flex flex-col gap-4 max-w-[400px]">
                {/* Master Password Field */}
                <div className="flex flex-col gap-1">
                  <label className="text-[12px] font-medium text-text-secondary">
                    Master password
                  </label>
                  <div className="relative flex items-center">
                    <Input
                      type={showPassword ? "text" : "password"}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      placeholder="At least 8 characters"
                      autoFocus
                      required
                      className="pr-9"
                    />
                    <button
                      type="button"
                      tabIndex={-1}
                      onClick={() => setShowPassword(!showPassword)}
                      className="absolute right-2.5 text-text-tertiary hover:text-text-primary transition-colors"
                      aria-label="Toggle password visibility"
                    >
                      {showPassword ? (
                        <EyeOff className="size-4" strokeWidth={1.5} />
                      ) : (
                        <Eye className="size-4" strokeWidth={1.5} />
                      )}
                    </button>
                  </div>
                  <span className="text-[11px] text-text-tertiary">
                    Use at least 8 characters. A longer passphrase is strongly recommended.
                  </span>
                </div>

                {/* Confirm Password Field */}
                <div className="flex flex-col gap-1">
                  <label className="text-[12px] font-medium text-text-secondary">
                    Confirm master password
                  </label>
                  <div className="relative flex items-center">
                    <Input
                      type={showConfirmPassword ? "text" : "password"}
                      value={confirmPassword}
                      onChange={(e) => setConfirmPassword(e.target.value)}
                      placeholder="Repeat your password"
                      required
                      className="pr-9"
                    />
                    <button
                      type="button"
                      tabIndex={-1}
                      onClick={() => setShowConfirmPassword(!showConfirmPassword)}
                      className="absolute right-2.5 text-text-tertiary hover:text-text-primary transition-colors"
                      aria-label="Toggle confirm password visibility"
                    >
                      {showConfirmPassword ? (
                        <EyeOff className="size-4" strokeWidth={1.5} />
                      ) : (
                        <Eye className="size-4" strokeWidth={1.5} />
                      )}
                    </button>
                  </div>
                  <span className="text-[11px] text-text-tertiary">
                    Enter the same password again.
                  </span>
                </div>
              </div>

              {/* Error banner if any */}
              {error && (
                <Alert variant="destructive" className="max-w-[560px]">
                  <AlertContent>
                    <AlertTitle>Validation Error</AlertTitle>
                    <AlertDescription>{error}</AlertDescription>
                  </AlertContent>
                </Alert>
              )}

              {/* Security Notice */}
              <div className="max-w-[560px]">
                <SecurityNotice type="irreversible" />
              </div>

              {/* Actions */}
              <div className="flex items-center justify-end gap-3 pt-2">
                <Button
                  type="submit"
                  disabled={isSubmitting || !password || !confirmPassword}
                  size="default"
                >
                  <span>{isSubmitting ? "Creating vault…" : "Create vault"}</span>
                  <ArrowRight className="size-4" strokeWidth={1.5} />
                </Button>
              </div>
            </form>
          </div>
        )}

        {/* State 3: First-run Setup - Step 2: Save Recovery Key */}
        {gateStep === "setup_recovery" && (
          <div className="flex w-full max-w-[820px] flex-col gap-6 rounded-[18px] border border-border bg-surface-glass-modal p-8 md:p-12 shadow-[0px_20px_48px_-8px_rgba(0,0,0,0.48)] backdrop-blur-[14px]">
            <div className="flex flex-col gap-2">
              <span className="text-[11px] font-medium tracking-[0.77px] text-action-primary uppercase">
                SHOWN EXACTLY ONCE
              </span>
              <h1 className="text-[28px] font-semibold tracking-tight text-text-primary">
                Save your recovery key
              </h1>
              <p className="text-[14px] font-normal leading-relaxed text-text-secondary">
                This key is not logged, audited, or persisted by DevKit. Store it offline before continuing.
              </p>
            </div>

            <div className="flex w-full justify-center">
              <StepProgress currentStep={2} />
            </div>

            {/* Recovery Key Specimen */}
            <div className="flex flex-col gap-5">
              <RecoveryKeyField recoveryKey={generatedRecoveryKey} defaultRevealed={true} />

              <div className="max-w-[640px]">
                <SecurityNotice type="irreversible" />
              </div>

              {/* Confirmation Checkbox */}
              <label className="flex items-center gap-3 cursor-pointer select-none py-1">
                <Checkbox
                  checked={isRecoverySaved}
                  onCheckedChange={(checked) => setIsRecoverySaved(Boolean(checked))}
                />
                <span className="text-[13px] font-normal text-text-primary">
                  I have saved the recovery key in a safe offline location
                </span>
              </label>

              {/* Actions */}
              <div className="flex items-center justify-end gap-3 pt-2">
                <Button
                  type="button"
                  onClick={handleConfirmRecovery}
                  disabled={!isRecoverySaved}
                  size="default"
                >
                  <span>Continue</span>
                  <ArrowRight className="size-4" strokeWidth={1.5} />
                </Button>
              </div>
            </div>
          </div>
        )}

        {/* State 4: First-run Setup - Step 4: Ready */}
        {gateStep === "setup_ready" && (
          <div className="flex w-full max-w-[696px] flex-col items-center gap-6 text-center rounded-[18px] border border-border bg-surface-glass-modal p-8 md:p-12 shadow-[0px_20px_48px_-8px_rgba(0,0,0,0.48)] backdrop-blur-[14px]">
            <div className="flex flex-col items-center gap-3">
              <div className="flex size-12 items-center justify-center rounded-full bg-action-primary/15 border border-action-primary/30">
                <Check className="size-6 text-action-primary" strokeWidth={2.5} />
              </div>
              <h1 className="text-[28px] font-semibold tracking-tight text-text-primary">
                Vault is ready
              </h1>
              <p className="text-[14px] font-normal leading-relaxed text-text-secondary max-w-[420px]">
                Your local vault is unlocked for this session. It will start locked the next time
                DevKit launches.
              </p>
            </div>

            <div className="flex w-full justify-center">
              <StepProgress currentStep={4} />
            </div>

            <div className="w-full max-w-[560px] text-left">
              <SecurityNotice type="secure" />
            </div>

            <div className="pt-2">
              <Button
                type="button"
                onClick={() => {
                  setVaultState("unlocked");
                  setGateStep("checking");
                }}
                size="lg"
                className="px-8"
              >
                <span>Enter DevKit</span>
                <ArrowRight className="size-4" strokeWidth={1.5} />
              </Button>
            </div>
          </div>
        )}

        {/* State 5: Returning User - Unlock with Password */}
        {gateStep === "unlock_password" && (
          <div className="flex w-full max-w-[680px] flex-col gap-6 rounded-[18px] border border-border bg-surface-glass-modal p-8 md:p-12 shadow-[0px_20px_48px_-8px_rgba(0,0,0,0.48)] backdrop-blur-[14px]">
            <div className="flex flex-col gap-2">
              <span className="text-[11px] font-medium tracking-[0.77px] text-action-primary uppercase">
                WELCOME BACK
              </span>
              <h1 className="text-[28px] font-semibold tracking-tight text-text-primary">
                Unlock your vault
              </h1>
              <p className="text-[14px] font-normal leading-relaxed text-text-secondary">
                Every DevKit process starts locked. Unlocking keeps the decrypted key in memory
                only for this session.
              </p>
            </div>

            <form onSubmit={handleUnlockPassword} className="flex flex-col gap-4">
              <div className="flex flex-col gap-1.5 max-w-[380px]">
                <label className="text-[12px] font-medium text-text-secondary">
                  Master password
                </label>
                <div className="relative flex items-center">
                  <Input
                    type={showPassword ? "text" : "password"}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="Enter Master Password"
                    autoFocus
                    required
                    className="pr-9"
                  />
                  <button
                    type="button"
                    tabIndex={-1}
                    onClick={() => setShowPassword(!showPassword)}
                    className="absolute right-2.5 text-text-tertiary hover:text-text-primary transition-colors"
                    aria-label="Toggle password visibility"
                  >
                    {showPassword ? (
                      <EyeOff className="size-4" strokeWidth={1.5} />
                    ) : (
                      <Eye className="size-4" strokeWidth={1.5} />
                    )}
                  </button>
                </div>
                <span className="text-[11px] text-text-tertiary">
                  Your password never leaves this device.
                </span>
              </div>

              {/* Error Alert */}
              {error && (
                <Alert variant="destructive" className="max-w-[520px]">
                  <AlertContent>
                    <AlertTitle>Unable to unlock vault</AlertTitle>
                    <AlertDescription>
                      Check the password and try again. No data was changed.
                    </AlertDescription>
                  </AlertContent>
                </Alert>
              )}

              {/* Action Buttons */}
              <div className="flex items-center justify-end gap-3 pt-4">
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => {
                    setError("");
                    setGateStep("unlock_recovery");
                  }}
                  size="default"
                >
                  <KeyRound className="size-3.5" strokeWidth={1.5} />
                  <span>Use recovery key</span>
                </Button>

                <Button type="submit" disabled={isSubmitting || !password} size="default">
                  <LockKeyhole className="size-3.5" strokeWidth={1.5} />
                  <span>{isSubmitting ? "Unlocking…" : "Unlock vault"}</span>
                </Button>
              </div>

              <p className="text-[11px] text-text-tertiary pt-2">
                Locking the vault zeroizes the decrypted key held in memory.
              </p>
            </form>
          </div>
        )}

        {/* State 6: Returning User - Unlock with Recovery Key */}
        {gateStep === "unlock_recovery" && (
          <div className="flex w-full max-w-[720px] flex-col gap-6 rounded-[18px] border border-border bg-surface-glass-modal p-8 md:p-12 shadow-[0px_20px_48px_-8px_rgba(0,0,0,0.48)] backdrop-blur-[14px]">
            <div className="flex flex-col gap-2">
              <span className="text-[11px] font-medium tracking-[0.77px] text-status-recovery uppercase">
                RECOVERY ACCESS
              </span>
              <h1 className="text-[28px] font-semibold tracking-tight text-text-primary">
                Use your recovery key
              </h1>
              <p className="text-[14px] font-normal leading-relaxed text-text-secondary">
                Recovery unlock opens the existing vault. It does not change your password or rotate
                the recovery key.
              </p>
            </div>

            <form onSubmit={handleUnlockRecovery} className="flex flex-col gap-4">
              <div className="flex flex-col gap-1.5 max-w-[440px]">
                <label className="text-[12px] font-medium text-text-secondary">
                  Recovery key
                </label>
                <Input
                  type="text"
                  value={recoveryKeyInput}
                  onChange={(e) => setRecoveryKeyInput(e.target.value)}
                  placeholder="DVK7-XXXX-XXXX-XXXX-XXXX-XXXX"
                  autoFocus
                  required
                  className="font-mono text-[13px]"
                />
                <span className="text-[11px] text-text-tertiary">
                  Enter the 29-character recovery key saved during first-run setup.
                </span>
              </div>

              {/* Error or Warning Alert */}
              {error ? (
                <Alert variant="destructive" className="max-w-[560px]">
                  <AlertContent>
                    <AlertTitle>Recovery Key Invalid</AlertTitle>
                    <AlertDescription>{error}</AlertDescription>
                  </AlertContent>
                </Alert>
              ) : (
                <Alert variant="warning" className="max-w-[560px]">
                  <AlertContent>
                    <AlertTitle>Emergency Access</AlertTitle>
                    <AlertDescription>
                      Once unlocked, you will gain access to all encrypted notes and profiles for
                      this session.
                    </AlertDescription>
                  </AlertContent>
                </Alert>
              )}

              {/* Action Buttons */}
              <div className="flex items-center justify-end gap-3 pt-4">
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => {
                    setError("");
                    setGateStep("unlock_password");
                  }}
                  size="default"
                >
                  <span>Use master password</span>
                </Button>

                <Button
                  type="submit"
                  disabled={isSubmitting || !recoveryKeyInput.trim()}
                  size="default"
                >
                  <KeyRound className="size-3.5" strokeWidth={1.5} />
                  <span>{isSubmitting ? "Unlocking…" : "Unlock with key"}</span>
                </Button>
              </div>
            </form>
          </div>
        )}
        </div>
      </div>
    </div>
  );
}
