import { EnvironmentReportView } from "@/modules/agenthub/environment/AgentEnvironment";
import type { EnvironmentReport } from "@/bridge/agenthub";

export const ENVIRONMENT_PREVIEW: EnvironmentReport = {
  agent: "codex", scope: "session", checkedAt: 0,
  entries: [
    { name: "Personal environment", kind: "access", status: "configured", detail: "Personal configuration and CLI environment are inherited." },
    { name: "Working directory", kind: "access", status: "configured", source: "/Users/developer/work/a-project-with-a-long-name/services/billing", detail: "The session may write here and in its additional granted folders." },
    { name: "gh", kind: "tools", status: "found", source: "/opt/homebrew/bin/gh", detail: "Executable found. Execution and authentication have not been tested." },
    { name: "glab", kind: "tools", status: "missing", detail: "Not found on the agent's PATH. This tool is optional.", action: "Install it or add its directory to DevKit's launch PATH, then reopen the session." },
    { name: "frontend-review", kind: "skills", status: "loaded", source: "/Users/developer/.codex/skills/frontend-review/SKILL.md", detail: "Reported by this runtime." },
    { name: "project-workflow", kind: "skills", status: "unverified", detail: "SKILL.md has missing or invalid metadata.", action: "Add a valid name and description in the skill's YAML frontmatter." },
    { name: "docs", kind: "mcp", status: "reported", detail: "3 tools advertised by this runtime; individual calls have not been tested." },
    { name: "issue-tracker", kind: "mcp", status: "needs_auth", detail: "Runtime reports that this server is not signed in.", action: "Authenticate this MCP server in the agent's DevKit environment, then refresh." },
  ],
};

export function AgentEnvironmentPreview() {
  return (
    <section id="agent-environment" className="scroll-mt-6 px-6 py-8">
      <h2 className="mb-4 text-title-16">Agent environment</h2>
      <div className="grid gap-6 lg:grid-cols-2">
        {["Wide", "Narrow"].map((label) => (
          <div key={label} className="min-w-0">
            <h3 className="mb-2 text-body-13-medium">{label} session report</h3>
            <div className={`max-h-150 overflow-y-auto rounded-xl border border-border-subtle bg-surface-base p-5 ${label === "Narrow" ? "max-w-90" : "max-w-160"}`}>
              <EnvironmentReportView report={ENVIRONMENT_PREVIEW} />
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
