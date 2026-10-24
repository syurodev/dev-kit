import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderPlus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { agentHubChooseDirectory } from "../../bridge/agenthub";
import { errorMessage } from "../../bridge/errors";
import { getAgentSandbox, saveAgentSandbox } from "../../bridge/preferences";
import type { AgentSandboxSettings } from "../../../bindings/devkit/internal/preferences/models";

const QUERY_KEY = ["preferences", "agent-sandbox"] as const;

/** The switches are on unless stored as false — unset is on. */
function on(value: boolean | null | undefined): boolean {
  return value !== false;
}

/**
 * How Agent Hub's vendor jail (internal/sandbox) is shaped beyond what a
 * session grants on its own. By default a session sees the real home folder
 * and Docker, found on its own (Docker Desktop, OrbStack, Colima, …); each is
 * one switch away. ~/.ssh, ~/.gnupg, ~/.aws, ~/.config/gcloud and DevKit's own
 * data stay hidden whatever these say.
 *
 * One save writes every field together (PreferencesSetAgentSandbox takes the
 * whole struct), so the text fields keep a local draft committed on
 * blur/click rather than round-tripping the bridge per keystroke. Applies to
 * the next session opened, resumed or forked — not one already running
 * (docs/dev-kit/sandbox.mdx).
 */
export function AgentSandboxSetting() {
  const client = useQueryClient();
  const [socketDraft, setSocketDraft] = useState<string | null>(null);
  const [newPath, setNewPath] = useState("");
  const [failed, setFailed] = useState("");

  const { data, error: readError } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: getAgentSandbox,
  });

  // A value loading in from the backend replaces an untouched draft; once
  // the person has typed something, their edit wins until saved.
  useEffect(() => {
    if (data && socketDraft === null) setSocketDraft(data.dockerSocketPath ?? "");
  }, [data, socketDraft]);

  const save = useMutation({
    mutationFn: saveAgentSandbox,
    onSuccess: async (_, settings) => {
      setFailed("");
      await client.invalidateQueries({ queryKey: QUERY_KEY });
      // The backend trims/dedupes (Normalized); reload what got stored.
      setSocketDraft(settings.dockerSocketPath ?? "");
    },
    onError: (cause) => {
      setFailed(errorMessage(cause));
      void client.invalidateQueries({ queryKey: QUERY_KEY });
    },
  });

  // Guards every write: before the initial load resolves there is nothing
  // to merge a change into, and saving then would write defaults over
  // whatever is stored.
  function commit(patch: Partial<AgentSandboxSettings>) {
    if (!data) return;
    save.mutate({ ...data, dockerSocketPath: socket.trim(), ...patch });
  }

  const socket = socketDraft ?? "";
  const paths = data?.extraReadOnlyPaths ?? [];
  const shareHome = on(data?.shareHome);
  const allowDocker = on(data?.allowDocker);
  const disabled = data === undefined || save.isPending;
  const message = failed || (readError ? errorMessage(readError) : "");

  async function addPath() {
    if (!data) return;
    const typed = newPath.trim();
    const picked = typed || (await agentHubChooseDirectory().catch(() => ""));
    setNewPath("");
    if (!picked || paths.includes(picked)) return;
    commit({ extraReadOnlyPaths: [...paths, picked] });
  }

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-2">
        <SwitchRow
          label="Share home folder"
          checked={shareHome}
          disabled={disabled}
          onChange={(checked) => commit({ shareHome: checked })}
        />
        <p className="text-body-12 text-text-tertiary">
          Agents see your real home folder, so tools that keep config there
          (kubeconfig, npmrc, gh, Docker contexts) just work. Off, a session
          sees only the project, the agent's own login and the paths below.{" "}
          <code>~/.ssh</code>, <code>~/.gnupg</code>, <code>~/.aws</code>,{" "}
          <code>~/.config/gcloud</code> and DevKit's own data stay hidden either way.
        </p>
      </div>

      <div className="flex flex-col gap-2">
        <SwitchRow
          label="Allow developer toolchains"
          checked={on(data?.allowDeveloperToolchains)}
          disabled={disabled}
          onChange={(checked) => commit({ allowDeveloperToolchains: checked })}
        />
        <p className="text-body-12 text-text-tertiary">
          Read-only access to macOS Command Line Tools and Xcode for native builds,
          cgo, git and xcrun. Applies when a session next starts or resumes.
        </p>
      </div>

      <div className="flex flex-col gap-2">
        <SwitchRow
          label="Docker"
          checked={allowDocker}
          disabled={disabled}
          onChange={(checked) => commit({ allowDocker: checked })}
        />
        {allowDocker ? (
          <input
            value={socket}
            onChange={(e) => setSocketDraft(e.target.value)}
            onBlur={() => {
              if (socket.trim() !== (data?.dockerSocketPath ?? "")) commit({});
            }}
            spellCheck={false}
            placeholder="Found automatically — Docker Desktop, OrbStack, Colima, Podman…"
            aria-label="Docker socket path"
            className="rounded-control border border-border-subtle bg-surface-raised px-3 py-2 text-code-12 text-text-primary placeholder:text-text-tertiary focus-visible:border-ring focus-visible:outline-none"
          />
        ) : null}
        <p className="text-body-12 text-text-tertiary">
          Agents can build images, run compose and read container logs. The
          socket is whatever your own <code>docker</code> uses — its current
          context, DOCKER_HOST, or the desktop app's — unless you name one
          here. Access to Docker is effectively root on this machine.
        </p>
      </div>

      <div className="flex flex-col gap-2">
        <span className="text-body-13 text-text-primary">Extra read-only paths</span>
        {paths.length > 0 ? (
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
                  onClick={() => commit({ extraReadOnlyPaths: paths.filter((p) => p !== path) })}
                >
                  <X />
                </Button>
              </li>
            ))}
          </ul>
        ) : null}
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
          Outside the project and your home folder — a shared library under{" "}
          <code>/opt</code>, say. Anything under a shared home is readable already.
        </p>
      </div>

      {message ? <p className="text-body-12 text-status-danger">{message}</p> : null}
    </div>
  );
}

function SwitchRow({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-2.5 rounded-control border border-border-subtle bg-surface-raised py-2.5 pr-3 pl-3.5">
      <span className="min-w-0 flex-1 text-body-13 text-text-primary">{label}</span>
      <Switch checked={checked} disabled={disabled} onCheckedChange={onChange} />
    </label>
  );
}
