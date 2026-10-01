import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { AgentSession } from "@/bridge/agenthub";
import { AgentSidebarTree, groupByProject, type SidebarWorkspace } from "./surfaces/AgentSidebarTree";
import { currentFirst } from "./sidebarWorkspaces";

const session = (id: string, projectPath: string, workspaceId = ""): AgentSession => ({
  id,
  agentType: "claude",
  projectPath,
  workspaceId,
  status: "idle",
  parentSessionId: null,
  depth: 0,
  title: id,
  acpSessionId: "",
});

const platform = "/home/me/Workspace/g-platform";
const security: SidebarWorkspace = {
  id: "ws-security",
  name: "fsap-security",
  paths: [`${platform}/kong-plugin`, `${platform}/fsap-authentication-service`, `${platform}/fsap-config-service`],
};

describe("the rail groups sessions by workspace", () => {
  it("every service of a workspace is one group, named for the workspace", () => {
    const groups = groupByProject(
      [
        session("kong", `${platform}/kong-plugin`, security.id),
        session("auth", `${platform}/fsap-authentication-service`, security.id),
        session("deep", `${platform}/fsap-config-service/src/main`, security.id),
      ],
      [],
      "",
      [security],
    );
    expect(groups).toHaveLength(1);
    expect(groups[0].label).toBe("fsap-security");
    // The primary: what "+" opens a session in.
    expect(groups[0].path).toBe(`${platform}/kong-plugin`);
    expect(groups[0].workspace?.id).toBe("ws-security");
    expect(groups[0].total).toBe(3);
  });

  it("a folder outside every workspace is its own group, and a name two folders share says where each is", () => {
    const groups = groupByProject(
      [
        session("in", `${platform}/kong-plugin`, security.id),
        session("old", "/home/me/Workspace/fsap-authentication-service"),
        session("other", "/home/me/Other/fsap-authentication-service"),
      ],
      [],
      "",
      [security],
    );
    expect(groups.map((g) => g.label)).toEqual([
      "fsap-security",
      "Other/fsap-authentication-service",
      "Workspace/fsap-authentication-service",
    ]);
  });

  it("a prefix is not a folder: kong-plugin-v2 is not inside kong-plugin", () => {
    const groups = groupByProject([session("v2", `${platform}/kong-plugin-v2`)], [], "", [security]);
    expect(groups[0].workspace).toBeUndefined();
    expect(groups[0].label).toBe("kong-plugin-v2");
  });

  it("a folder in two workspaces stays with the session's chosen workspace", () => {
    const other: SidebarWorkspace = { id: "ws-kong", name: "kong only", paths: [`${platform}/kong-plugin`] };
    const listed = currentFirst([other, security], "ws-security");
    expect(listed.map((w) => w.id)).toEqual(["ws-security", "ws-kong"]);
    const groups = groupByProject([session("kong", `${platform}/kong-plugin`, other.id)], [], "", listed);
    expect(groups[0].label).toBe("kong only");
    const selected = groupByProject([session("kong", `${platform}/kong-plugin`, security.id)], [], "", listed);
    expect(selected[0].label).toBe("fsap-security");
    expect(currentFirst([other, security], null).map((w) => w.id)).toEqual(["ws-kong", "ws-security"]);
  });

  it("draws the workspace row with its name", () => {
    const html = renderToString(
      <TooltipProvider>
        <AgentSidebarTree
          sessions={[session("auth", `${platform}/fsap-authentication-service`, security.id)]}
          workspaces={[security]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />
      </TooltipProvider>,
    );
    expect(html).toContain(">fsap-security<");
    expect(html).not.toContain(">fsap-authentication-service<");
  });
});
