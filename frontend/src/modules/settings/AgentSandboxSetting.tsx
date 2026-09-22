import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderPlus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { agentHubChooseDirectory } from "../../bridge/agenthub";
import { errorMessage } from "../../bridge/errors";
import { getAgentSandbox, saveAgentSandbox } from "../../bridge/preferences";

const QUERY_KEY = ["preferences", "agent-sandbox"] as const;

/**
 * How far Agent Hub's vendor jail (internal/sandbox) is widened beyond what a
 * session grants on its own — a Docker socket, extra read-only paths outside
 * the project. Both are opt-in and empty by default: a machine that never
 * opens this section runs exactly as restrictively as before it existed.
 *
 * One save writes both fields together (PreferencesSetAgentSandbox takes the
 * whole struct), so a field's own draft state is kept locally and committed
 * on blur/click rather than round-tripping the bridge per keystroke. Applies
 * to the next session opened, resumed or forked — not one already running
 * under the narrower jail (docs/dev-kit/sandbox.mdx § "User-configurable jail
 * widening").
 */
export function AgentSandboxSetting() {
  const client = useQueryClient();
  const [socketDraft, setSocketDraft] = useState<string | null>(null);
  const [pathsDraft, setPathsDraft] = useState<string[] | null>(null);
  const [newPath, setNewPath] = useState("");
  const [failed, setFailed] = useState("");

  const { data, error: readError } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: getAgentSandbox,
  });

  // A different value loading in from the backend replaces an untouched
  // draft; once the person has typed something, their edit wins until saved.
  useEffect(() => {
    if (data && socketDraft === null) setSocketDraft(data.dockerSocketPath ?? "");
  }, [data, socketDraft]);
  useEffect(() => {
    if (data && pathsDraft === null) setPathsDraft(data.extraReadOnlyPaths ?? []);
  }, [data, pathsDraft]);

  const save = useMutation({
    mutationFn: saveAgentSandbox,
    onSuccess: (_, settings) => {
      setFailed("");
      client.setQueryData(QUERY_KEY, settings);
      // The backend trims/dedupes (Normalized); reload the drafts from what
      // actually got stored rather than what was sent.
      setSocketDraft(settings.dockerSocketPath ?? "");
      setPathsDraft(settings.extraReadOnlyPaths ?? []);
    },
    onError: (cause) => setFailed(errorMessage(cause)),
  });

  // Guards every write: a draft is a fallback for render before the initial
  // load resolves, and committing then would save that empty fallback over
  // whatever is already stored — the field the person never touched loses
  // its value to the one they did.
  function commit(next: { dockerSocketPath: string; extraReadOnlyPaths: string[] }) {
    if (!data) return;
    save.mutate(next);
  }

  const socket = socketDraft ?? "";
  const paths = pathsDraft ?? [];
  const message = failed || (readError ? errorMessage(readError) : "");

  async function addPath() {
    if (!data) return;
    const typed = newPath.trim();
    const picked = typed || (await agentHubChooseDirectory().catch(() => ""));
    if (!picked || paths.includes(picked)) {
      setNewPath("");
      return;
    }
    const next = [...paths, picked];
    setPathsDraft(next);
    setNewPath("");
    commit({ dockerSocketPath: socket, extraReadOnlyPaths: next });
  }

  function removePath(path: string) {
    const next = paths.filter((p) => p !== path);
    setPathsDraft(next);
    commit({ dockerSocketPath: socket, extraReadOnlyPaths: next });
  }

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-2">
        <label className="flex flex-col gap-1.5">
          <span className="text-body-13 text-text-primary">Docker socket</span>
          <input
            value={socket}
            onChange={(e) => setSocketDraft(e.target.value)}
            onBlur={() => {
              if (socket.trim() !== (data?.dockerSocketPath ?? "")) {
                commit({ dockerSocketPath: socket.trim(), extraReadOnlyPaths: paths });
              }
            }}
            spellCheck={false}
            placeholder="/var/run/docker.sock"
            aria-label="Docker socket path"
            className="rounded-control border border-border-subtle bg-surface-raised px-3 py-2 text-code-12 text-text-primary placeholder:text-text-tertiary focus-visible:border-ring focus-visible:outline-none"
          />
        </label>
        <p className="text-body-12 text-text-tertiary">
          A single unix socket every agent session may connect to — never a general
          network grant. Empty means agents cannot reach Docker at all, which is the
          default.
        </p>
      </div>

      <div className="flex flex-col gap-2">
        <span className="text-body-13 text-text-primary">Extra read-only paths</span>
        <ul className="flex flex-col gap-1.5">
          {paths.map((path) => (
            <li
              key={path}
              className="flex items-center gap-2 rounded-control border border-border-subtle bg-surface-raised py-1.5 pr-1.5 pl-3"
            >
              <span title={path} className="min-w-0 flex-1 truncate text-code-12 text-text-secondary">
                {path}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={`Remove ${path}`}
                disabled={save.isPending}
                onClick={() => removePath(path)}
              >
                <X />
              </Button>
            </li>
          ))}
        </ul>
        <div className="flex items-center gap-2">
          <input
            value={newPath}
            onChange={(e) => setNewPath(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                void addPath();
              }
            }}
            spellCheck={false}
            placeholder="/path/to/shared/library"
            aria-label="Add a read-only path"
            className="min-w-0 flex-1 rounded-control border border-border-subtle bg-surface-raised px-3 py-2 text-code-12 text-text-primary placeholder:text-text-tertiary focus-visible:border-ring focus-visible:outline-none"
          />
          <Button type="button" variant="outline" size="sm" disabled={save.isPending} onClick={() => void addPath()}>
            <FolderPlus />
            Add
          </Button>
        </div>
        <p className="text-body-12 text-text-tertiary">
          Directories or files outside the project every agent session may read —
          a shared library or a monorepo sibling, not a substitute for opening the
          project itself higher up.
        </p>
      </div>

      {message ? <p className="text-body-12 text-status-danger">{message}</p> : null}
    </div>
  );
}
