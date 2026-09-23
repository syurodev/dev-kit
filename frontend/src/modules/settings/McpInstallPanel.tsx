import { useEffect, useState } from "react";
import {
  mcpInstall,
  mcpListAgents,
  mcpUninstall,
} from "../../bridge/mcp";
import { errorMessage } from "../../bridge/errors";
import { useCurrentFolder } from "../../stores/workFolder";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

type Scope = "global" | "project";

export function McpInstallPanel() {
  const [agents, setAgents] = useState<string[]>(["claude-code", "codex"]);
  const [agent, setAgent] = useState("claude-code");
  const [scope, setScope] = useState<Scope>("global");
  // What was typed, until something is: the field starts on the folder the app
  // is working in rather than empty, and typing over it is the person's own.
  const [typedPath, setProjectPath] = useState<string | null>(null);
  const currentFolder = useCurrentFolder();
  const projectPath = typedPath ?? currentFolder;
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    void (async () => {
      try {
        const list = await mcpListAgents();
        if (list.length > 0) {
          setAgents(list);
          setAgent(list[0]);
        }
      } catch (e) {
        setError(errorMessage(e));
      }
    })();
  }, []);

  async function onInstall() {
    setError("");
    setStatus("");
    try {
      const path = await mcpInstall(
        agent,
        scope,
        scope === "project" ? projectPath : "",
      );
      setStatus(`Installed → ${path}`);
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  async function onUninstall() {
    setError("");
    setStatus("");
    try {
      const path = await mcpUninstall(
        agent,
        scope,
        scope === "project" ? projectPath : "",
      );
      setStatus(`Uninstalled from ${path}`);
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  const labels: Record<string, string> = { "claude-code": "Claude Code", codex: "Codex" };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex gap-3">
        <label className="flex flex-1 flex-col gap-1">
          <span className="text-caption-11 text-text-tertiary">Agent</span>
          <Select value={agent} onValueChange={setAgent}>
            <SelectTrigger aria-label="mcp-agent" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {agents.map((a) => (
                <SelectItem key={a} value={a}>
                  {labels[a] ?? a}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </label>
        <label className="flex flex-1 flex-col gap-1">
          <span className="text-caption-11 text-text-tertiary">Scope</span>
          <Select value={scope} onValueChange={(v) => setScope(v as Scope)}>
            <SelectTrigger aria-label="mcp-scope" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="global">Global — every project</SelectItem>
              <SelectItem value="project">One project</SelectItem>
            </SelectContent>
          </Select>
        </label>
      </div>

      {scope === "project" ? (
        <label className="flex flex-col gap-1">
          <span className="text-caption-11 text-text-tertiary">Project path</span>
          <Input
            aria-label="mcp-project-path"
            placeholder="/path/to/project"
            value={projectPath}
            onChange={(e) => setProjectPath(e.target.value)}
          />
        </label>
      ) : null}

      <div className="flex gap-2">
        <Button type="button" size="sm" onClick={() => void onInstall()}>
          Install
        </Button>
        <Button type="button" size="sm" variant="outline" onClick={() => void onUninstall()}>
          Uninstall
        </Button>
      </div>

      {status ? (
        <p role="status" className="text-code-12 text-text-tertiary break-all">
          {status}
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="text-body-12 text-status-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}
