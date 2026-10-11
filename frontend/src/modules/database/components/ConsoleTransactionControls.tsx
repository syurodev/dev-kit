import { useEffect, useState, useRef } from "react";
import { Check, Undo2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import {
  commitTransaction,
  rollbackTransaction,
  setTransactionMode,
  transactionState,
} from "@/bridge/database";
import { errorMessage } from "@/bridge/errors";

/** Transactions are profile-scoped; Explorer selection must not redirect these controls. */
export function ConsoleTransactionControls({
  profileId,
  running,
  runId,
  readState = transactionState,
  onBusyChange,
}: {
  profileId: string;
  running: boolean;
  runId: number;
  readState?: typeof transactionState;
  onBusyChange?: (busy: boolean) => void;
}) {
  const generation = useRef(0);
  const [tx, setTx] = useState({ manual: false, open: false });
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [error, setError] = useState("");
  useEffect(() => {
    onBusyChange?.(busy);
  }, [busy, onBusyChange]);
  useEffect(() => {
    let live = true;
    generation.current++;
    setTx({ manual: false, open: false });
    setLoaded(false);
    setError("");
    setBusy(false);
    if (profileId)
      void readState(profileId)
        .then((value) => {
          if (live) {
            setTx(value);
            setLoaded(true);
          }
        })
        .catch((e) => {
          if (live) setError(errorMessage(e));
        });
    return () => {
      live = false;
    };
  }, [profileId, running, runId, readState, refresh]);
  async function act(action: () => Promise<void>) {
    const token = generation.current;
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (e) {
      if (token === generation.current) setError(errorMessage(e));
    } finally {
      if (token === generation.current) {
        try {
          const value = await readState(profileId);
          if (token === generation.current) setTx(value);
        } catch (e) {
          if (token === generation.current) setError(errorMessage(e));
        }
        if (token === generation.current) setBusy(false);
      }
    }
  }
  const disabled = !profileId || running || busy || !loaded;
  return (
    <div
      className="flex flex-wrap items-center gap-1"
      aria-label="Console transaction"
    >
      <span className="text-body-12 text-text-tertiary">Tx:</span>
      <NativeSelect
        size="sm"
        aria-label="Console transaction mode"
        value={loaded ? (tx.manual ? "manual" : "auto") : "unknown"}
        disabled={disabled}
        onChange={(e) =>
          void act(() =>
            setTransactionMode(profileId, e.target.value === "manual"),
          )
        }
      >
        <NativeSelectOption value="unknown" disabled>
          {!profileId ? "—" : error ? "Unavailable" : "Checking…"}
        </NativeSelectOption>
        <NativeSelectOption value="auto">Auto</NativeSelectOption>
        <NativeSelectOption value="manual">Manual</NativeSelectOption>
      </NativeSelect>
      {tx.manual && (
        <>
          <Button
            size="xs"
            variant="ghost"
            disabled={disabled || !tx.open}
            onClick={() => void act(() => commitTransaction(profileId))}
          >
            <Check className="size-3.5" /> Commit
          </Button>
          <Button
            size="xs"
            variant="ghost"
            disabled={disabled || !tx.open}
            onClick={() => void act(() => rollbackTransaction(profileId))}
          >
            <Undo2 className="size-3.5" /> Roll back
          </Button>
        </>
      )}
      {error && (
        <Button
          size="xs"
          variant="ghost"
          onClick={() => setRefresh((value) => value + 1)}
        >
          Retry transaction state
        </Button>
      )}
      {error && (
        <span
          role="alert"
          className="max-w-60 truncate text-body-12 text-status-danger"
          title={error}
        >
          {error}
        </span>
      )}
    </div>
  );
}
