import { useEffect, useState, type ReactNode } from "react";
import { setupVault, unlockVault, vaultStatus, type VaultState } from "../bridge/vault";

type Status = "loading" | VaultState;

export function VaultGate({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Status>("loading");
  const [password, setPassword] = useState("");
  const [recoveryKey, setRecoveryKey] = useState("");
  const [error, setError] = useState("");

  async function refresh() {
    setStatus(await vaultStatus());
  }

  useEffect(() => {
    refresh().catch((e) => setError(String(e)));
  }, []);

  async function onSetup() {
    setError("");
    try {
      const recovery = await setupVault(password);
      setRecoveryKey(recovery);
      setPassword("");
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function onUnlock() {
    setError("");
    try {
      await unlockVault(password);
      setPassword("");
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  if (status === "loading") {
    return <div className="vault-gate">Loading…</div>;
  }

  if (status === "unlocked") {
    return (
      <>
        {recoveryKey ? (
          <div className="vault-recovery" role="alert">
            <p>
              Save your recovery key now — it is shown only once and cannot be
              recovered later:
            </p>
            <code>{recoveryKey}</code>
            <button type="button" onClick={() => setRecoveryKey("")}>
              I saved it
            </button>
          </div>
        ) : null}
        {children}
      </>
    );
  }

  const isSetup = status === "not_configured";
  return (
    <div className="vault-gate">
      <h1>{isSetup ? "Create Master Password" : "Unlock DevKit"}</h1>
      <p>
        {isSetup
          ? "A Master Password is required to protect your vault. Without your recovery key it cannot be recovered."
          : "Enter your Master Password to unlock the vault."}
      </p>
      <input
        type="password"
        aria-label="master-password"
        value={password}
        placeholder="Master Password"
        onChange={(e) => setPassword(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            void (isSetup ? onSetup() : onUnlock());
          }
        }}
      />
      <button type="button" onClick={isSetup ? onSetup : onUnlock}>
        {isSetup ? "Create" : "Unlock"}
      </button>
      {error ? <p role="alert">{error}</p> : null}
    </div>
  );
}
