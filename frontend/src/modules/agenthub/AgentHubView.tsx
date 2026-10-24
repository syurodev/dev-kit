import { AgentConversationSurface } from "./surfaces";
import { useAgentHubController } from "./session/useAgentHubController";

/**
 * One Agent Hub tab. The controller owns what the tab reads and can do; the
 * surface draws it. Nothing is decided here.
 */
export function AgentHubView({ params }: { params?: Record<string, unknown> } = {}) {
  const tabSessionId = typeof params?.sessionId === "string" ? params.sessionId : "";
  const hub = useAgentHubController(tabSessionId);
  return <AgentConversationSurface hub={hub} />;
}
