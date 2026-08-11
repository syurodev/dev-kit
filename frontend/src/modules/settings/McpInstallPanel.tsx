import { useEffect, useState } from "react";
import {
  mcpInstall,
  mcpListAgents,
  mcpUninstall,
} from "../../bridge/mcp";
import { errorMessage } from "../../bridge/errors";

type Scope = "global" | "project";

export function McpInstallPanel() {
  const [agents, setAgents] = useState<string[]>(["claude-code", "codex"]);
  const [agent, setAgent] = useState("claude-code");
  const [scope, setScope] = useState<Scope>("global");
  const [projectPath, setProjectPath] = useState("");
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

  return (
    <section className="mcp-install-panel" aria-label="mcp-install">
      <h2>MCP Install</h2>
      <p>Register the DevKit MCP proxy with Claude Code or Codex.</p>

      <div className="mcp-install-row">
        <label>
          Agent
          <select
            aria-label="mcp-agent"
            value={agent}
            onChange={(e) => setAgent(e.target.value)}
          >
            {agents.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
        </label>

        <fieldset aria-label="mcp-scope">
          <legend>Scope</legend>
          <label>
            <input
              type="radio"
              name="mcp-scope"
              value="global"
              checked={scope === "global"}
              onChange={() => setScope("global")}
            />
            global
          </label>
          <label>
            <input
              type="radio"
              name="mcp-scope"
              value="project"
              checked={scope === "project"}
              onChange={() => setScope("project")}
            />
            project
          </label>
        </fieldset>
      </div>

      {scope === "project" ? (
        <div className="mcp-install-row">
          <label>
            Project path
            <input
              aria-label="mcp-project-path"
              placeholder="/path/to/project"
              value={projectPath}
              onChange={(e) => setProjectPath(e.target.value)}
            />
          </label>
        </div>
      ) : null}

      <div className="mcp-install-actions">
        <button type="button" onClick={() => void onInstall()}>
          Install
        </button>
        <button type="button" onClick={() => void onUninstall()}>
          Uninstall
        </button>
      </div>

      {status ? <p role="status">{status}</p> : null}
      {error ? <p role="alert">{error}</p> : null}
    </section>
  );
}
