import { useState } from "react";
import { Boxes, GitBranch, ListTodo } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AgentComposer } from "@/modules/agenthub/surfaces/AgentComposer";
import { TrayChip } from "@/modules/agenthub/surfaces/composer/ComposerTray";
import {
  TrayTaskList,
  TrayRepoList,
} from "@/modules/agenthub/surfaces/composer/agentTray";
import type { TrayTask } from "@/modules/agenthub/surfaces/composer/useTrayStatus";
import type { Board, Task } from "@/bridge/tasks";
import type { QueuedAgentPrompt } from "@/stores/agentSessions";

const board = { id: "preview", name: "Platform" } as Board;
function row(
  id: string,
  title: string,
  statusName: string,
  active = false,
  done = false,
  held = false,
): TrayTask {
  return {
    task: { id, key: `DK-${id}`, title } as Task,
    board,
    statusName,
    active,
    done,
    held,
  };
}
const tasks = [
  row(
    "241",
    "Redesign the composer task panel",
    "In progress",
    true,
    false,
    true,
  ),
  row("242", "Keep workspace navigation consistent", "To do"),
  row("240", "Verify light and dark surfaces", "Done", false, true),
];
const others = [
  row("198", "Improve API collection import", "To do"),
  row(
    "203",
    "Audit long task titles in the narrow companion panel",
    "In progress",
    true,
  ),
];
const repos = [
  {
    path: "/preview/api",
    name: "api-service",
    branch: "codex/composer-design",
    own: true,
    ahead: 2,
    behind: 0,
    changed: 4,
    conflicted: 0,
    detached: false,
    error: "",
  },
  {
    path: "/preview/web",
    name: "frontend",
    branch: "main",
    own: false,
    ahead: 0,
    behind: 1,
    changed: 0,
    conflicted: 0,
    detached: false,
    error: "",
  },
];

/** The real composer and tray surfaces, with local data and no agent process. */
export function AgentComposerDesignPreview() {
  const [panel, setPanel] = useState<"tasks" | "repos" | null>("tasks");
  const [rail, setRail] = useState(false);
  const [value, setValue] = useState("");
  const [queued, setQueued] = useState<QueuedAgentPrompt[]>([]);
  const toggle = (next: "tasks" | "repos") =>
    setPanel((current) => (current === next ? null : next));
  return (
    <div className="w-full space-y-4">
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" onClick={() => setRail((v) => !v)}>
          {rail ? "Wide composer" : "Narrow companion"}
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            setQueued((v) =>
              v.length
                ? []
                : [
                    {
                      id: "queued",
                      text: "Also check the empty state after this turn",
                      attachments: [],
                      images: [],
                      refs: [],
                    },
                  ],
            )
          }
        >
          Toggle queued sample
        </Button>
      </div>
      <div className={rail ? "mx-auto w-full max-w-80" : "w-full"}>
        <AgentComposer
          key={rail ? "rail" : "wide"}
        size={rail ? "rail" : "wide"}
          value={value}
          onChange={setValue}
          onSend={() => setValue("")}
          onCancel={() => {}}
          vendor="codex"
          queuedPrompts={queued}
          onCancelQueued={(id) =>
            setQueued((current) => current.filter((p) => p.id !== id))
          }
          onEditQueued={(p) => {
            setValue(p.text);
            setQueued([]);
          }}
          trayStrip={
            <>
              <TrayChip icon={<Boxes />} label="Platform" />
              <TrayChip
                icon={<GitBranch />}
                label="Repositories"
                count={2}
                active={panel === "repos"}
                onClick={() => toggle("repos")}
              />
              <TrayChip
                icon={<ListTodo />}
                label="Tasks"
                count={3}
                active={panel === "tasks"}
                onClick={() => toggle("tasks")}
              />
            </>
          }
          trayPanel={
            panel
              ? {
                  key: panel,
                  content:
                    panel === "tasks" ? (
                      <TrayTaskList
                        tasks={tasks}
                        others={others}
                        session
                        onClose={() => setPanel(null)}
                      />
                    ) : (
                      <TrayRepoList
                        repos={repos}
                        onClose={() => setPanel(null)}
                      />
                    ),
                }
              : null
          }
        />
      </div>
    </div>
  );
}
