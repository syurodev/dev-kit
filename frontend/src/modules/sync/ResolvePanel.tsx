import { useEffect, useState } from "react";
import {
  syncConflictDetail,
  syncResolveConflict,
  type ResolveStrategy,
  type SyncConflictDetail,
} from "../../bridge/sync";
import { errorMessage } from "../../bridge/errors";

type Props = {
  recordId: string;
  onClose: () => void;
  onResolved: () => void;
};

export function ResolvePanel({ recordId, onClose, onResolved }: Props) {
  const [detail, setDetail] = useState<SyncConflictDetail | null>(null);
  const [merged, setMerged] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    syncConflictDetail(recordId)
      .then((loaded) => {
        if (!active) return;
        setDetail(loaded);
        setMerged(loaded.oursText);
      })
      .catch((caught) => active && setError(errorMessage(caught)));
    return () => {
      active = false;
    };
  }, [recordId]);

  async function resolve(strategy: ResolveStrategy) {
    setBusy(true);
    setError("");
    try {
      await syncResolveConflict(recordId, strategy, strategy === "merge" ? merged : "");
      onResolved();
    } catch (caught) {
      setError(errorMessage(caught));
      setBusy(false);
    }
  }

  if (!detail) {
    return (
      <div className="resolve-panel">
        <div className="resolve-header">
          <h3>Resolve conflict</h3>
          <button type="button" onClick={onClose} disabled={busy}>
            Close
          </button>
        </div>
        {error ? <p role="alert">{error}</p> : <p>Loading…</p>}
      </div>
    );
  }

  const disabled = busy || !detail.theirsCaptured;

  return (
    <div className="resolve-panel">
      <div className="resolve-header">
        <h3>
          Resolve <code>{detail.recordId}</code>
          <span className="sync-conflict-type">{detail.recordType}</span>
        </h3>
        <button type="button" onClick={onClose} disabled={busy}>
          Close
        </button>
      </div>

      {!detail.theirsCaptured ? (
        <p className="resolve-waiting">
          The other version has not been pulled yet. Run <strong>Sync now</strong>{" "}
          to fetch “theirs”, then resolve.
        </p>
      ) : detail.mergeable && !detail.oursDeleted && !detail.theirsDeleted ? (
        <>
          <pre className="resolve-markers">{`<<<<<<< ours\n${detail.oursText}\n=======\n${detail.theirsText}\n>>>>>>> theirs`}</pre>
          <label className="resolve-merge">
            Merged result
            <textarea
              value={merged}
              onChange={(event) => setMerged(event.target.value)}
              rows={5}
              disabled={busy}
            />
          </label>
        </>
      ) : detail.oursDeleted || detail.theirsDeleted ? (
        <p className="resolve-secret">
          {detail.oursDeleted && detail.theirsDeleted
            ? "Both sides deleted this record."
            : detail.oursDeleted
              ? "This side deleted the record locally; the remote side kept editing it."
              : "The remote side deleted this record; this side kept editing it."}{" "}
          Choose which side to keep — a deleted side is not line-merged.
        </p>
      ) : (
        <p className="resolve-secret">
          This is a secret item. Choose which encrypted version to keep — secrets
          are never line-merged.
        </p>
      )}

      <div className="resolve-actions">
        <button type="button" onClick={() => resolve("ours")} disabled={disabled}>
          Use ours
        </button>
        <button type="button" onClick={() => resolve("theirs")} disabled={disabled}>
          Use theirs
        </button>
        {detail.mergeable && !detail.oursDeleted && !detail.theirsDeleted ? (
          <button
            type="button"
            className="resolve-merge-save"
            onClick={() => resolve("merge")}
            disabled={disabled}
          >
            Save merge
          </button>
        ) : null}
      </div>

      {error ? <p role="alert">{error}</p> : null}
    </div>
  );
}
