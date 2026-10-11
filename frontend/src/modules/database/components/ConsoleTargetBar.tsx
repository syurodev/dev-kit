import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import { getSchemaCache, openDatabase } from "@/bridge/database";
import { errorMessage } from "@/bridge/errors";
import { useDatabaseStore } from "../databaseStore";
import { EngineMark } from "./EngineBadge";

/** The target belongs to this document, independently of Explorer selection. */
export function ConsoleTargetBar({
  consoleId,
  disabled = false,
}: {
  consoleId: string;
  disabled?: boolean;
}) {
  const state = useDatabaseStore();
  const slice = state.consoleState(consoleId);
  const id = slice.targetProfileId ?? state.selectedProfileId;
  const profile = state.profiles.find((p) => p.id === id);
  const rootId = profile?.parentProfileId || id;
  const request = useRef(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (slice.targetProfileId === null && id) state.bindConsole(consoleId, id);
  }, [consoleId, id, slice.targetProfileId, state.bindConsole]);
  useEffect(() => {
    setBusy(false);
    setError("");
    if (profile && (!profile.engine || profile.engine === "postgres"))
      void state.discoverDatabases(rootId);
    return () => {
      request.current++;
    };
  }, [rootId, profile?.engine, state.discoverDatabases]);
  const schema = useQuery({
    queryKey: ["database", "console-schema", id],
    queryFn: () => getSchemaCache(id),
    enabled: Boolean(profile),
    retry: false,
  });
  const names = Object.keys(schema.data?.namespace ?? {});
  const databaseNames = [
    ...new Set([
      profile?.database || "postgres",
      ...(state.serverDatabases[rootId] ?? []),
    ]),
  ];
  async function chooseDatabase(name: string) {
    const token = ++request.current;
    setBusy(true);
    setError("");
    try {
      const child = await openDatabase(rootId, name);
      if (
        request.current !== token ||
        useDatabaseStore.getState().consoleState(consoleId).targetProfileId !==
          id
      )
        return;
      useDatabaseStore.setState((s) => ({
        profiles: [...s.profiles.filter((p) => p.id !== child.id), child],
      }));
      state.bindConsole(consoleId, child.id);
    } catch (e) {
      if (request.current === token) setError(errorMessage(e));
    } finally {
      if (request.current === token) setBusy(false);
    }
  }
  return (
    <div className="shrink-0 border-b border-border-subtle px-3 py-2">
      <div
        className="flex min-w-0 flex-wrap items-center gap-2 text-body-12"
        aria-label="Console target"
      >
        <EngineMark engine={profile?.engine} />
        <NativeSelect
          size="sm"
          aria-label="Console connection"
          value={rootId}
          disabled={disabled || slice.runningQuery || busy}
          onChange={(e) => {
            request.current++;
            setError("");
            state.bindConsole(consoleId, e.target.value);
          }}
        >
          <NativeSelectOption value="">Choose connection</NativeSelectOption>
          {id && !profile && (
            <NativeSelectOption value={id}>
              Connection unavailable
            </NativeSelectOption>
          )}
          {state.profiles
            .filter((p) => !p.parentProfileId)
            .map((p) => (
              <NativeSelectOption key={p.id} value={p.id}>
                {p.name}
              </NativeSelectOption>
            ))}
        </NativeSelect>
        {profile && (!profile.engine || profile.engine === "postgres") ? (
          <NativeSelect
            size="sm"
            aria-label="Console database"
            value={profile.database || "postgres"}
            disabled={disabled || slice.runningQuery || busy}
            onChange={(e) => void chooseDatabase(e.target.value)}
          >
            {databaseNames.map((name) => (
              <NativeSelectOption key={name} value={name}>
                {name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        ) : (
          profile?.database && (
            <span className="text-code-11">{profile.database}</span>
          )
        )}
        {names.length > 0 && (
          <NativeSelect
            size="sm"
            aria-label="Completion schema"
            title="Schema used for SQL completion. Use qualified names or SET search_path to change query resolution."
            value={slice.completionSchema}
            onChange={(e) =>
              state.setCompletionSchema(consoleId, e.target.value)
            }
          >
            <NativeSelectOption value="">Completion schema</NativeSelectOption>
            {names.map((name) => (
              <NativeSelectOption key={name} value={name}>
                {name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        )}
        {profile?.env && (
          <span
            className={
              profile.env.toLowerCase() === "prod"
                ? "rounded bg-status-warning/10 px-2 py-0.5 text-status-warning"
                : "text-text-tertiary"
            }
          >
            {profile.env.toUpperCase()}
          </span>
        )}
        {profile?.readOnly && (
          <span className="rounded bg-state-soft px-2 py-0.5 text-text-secondary">
            Read-only
          </span>
        )}
        {busy && <span role="status">Opening database…</span>}
      </div>
      {error && (
        <p role="alert" className="mt-1 text-body-12 text-status-danger">
          {error}
        </p>
      )}
    </div>
  );
}
