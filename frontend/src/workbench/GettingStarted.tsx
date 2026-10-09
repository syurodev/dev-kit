import { useEffect, useState } from "react";
import { Circle, CircleCheck, type LucideIcon } from "lucide-react";

import { listProfiles } from "@/bridge/database";
import { codeGraphProjectRoot } from "@/bridge/codegraph";
import { listWorkspaces } from "@/bridge/workspace";
import {
  FramedCard,
  FramedCardAction,
  FramedCardHeader,
  FramedCardPreview,
  FramedCardRow,
} from "@/components/ui/framed-card";
import { cn } from "@/lib/utils";
import { useCodeGraphStore } from "@/modules/codegraph/codegraphStore";
import { useDatabaseStore } from "@/modules/database/databaseStore";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { useGettingStartedStore } from "@/stores/gettingStarted";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";
import { showModuleHome } from "./showModule";
import { requestNewWorkspace } from "./workspaceDialogRequest";

export type GettingStartedStep = {
  id: "workspace" | "codegraph" | "database" | "agent";
  title: string;
  detail: string;
  /** What the row's click does, named on its right while the step is open. */
  action: string;
  done: boolean;
  run: () => void;
};

/**
 * Whether each first-run step has happened, read from what exists rather than
 * from a flag set when a button was pressed — a workspace made from the title
 * bar, or a connection saved from the Database rail, ticks its step the same.
 *
 * Each answer is the backend's on mount, then the live store where one exists:
 * the Database and Code Graph stores stay empty until their module is first
 * visited, so they alone would call a returning person's steps undone.
 */
export function useGettingStartedProgress() {
  const servicesVersion = useWorkspaceSwitcherStore((s) => s.servicesVersion);
  const hasFolders = useCodeGraphStore((s) => s.folders.length > 0);
  const profileCount = useDatabaseStore((s) => s.profiles.length);
  const sessionCount = useAgentSessionsStore((s) => s.sessionIds.length);

  const [workspaces, setWorkspaces] = useState<number | null>(null);
  const [restoredProject, setRestoredProject] = useState("");
  const [savedProfiles, setSavedProfiles] = useState(0);

  useEffect(() => {
    let cancelled = false;
    listWorkspaces()
      .then((list) => !cancelled && setWorkspaces(list.length))
      .catch(() => !cancelled && setWorkspaces(0));
    return () => {
      cancelled = true;
    };
  }, [servicesVersion]);

  useEffect(() => {
    let cancelled = false;
    codeGraphProjectRoot()
      .then((root) => !cancelled && setRestoredProject(root))
      .catch(() => {});
    listProfiles()
      .then((list) => !cancelled && setSavedProfiles(list.length))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  return {
    loaded: workspaces !== null,
    workspace: (workspaces ?? 0) > 0,
    codegraph: hasFolders || restoredProject !== "",
    database: profileCount > 0 || savedProfiles > 0,
    agent: sessionCount > 0,
  };
}

export type GettingStartedActions = {
  newWorkspace: () => void;
  openCodeGraph: () => void;
  addConnection: () => void;
  newSession: () => void;
};

/** What each step's click does in the app. Passed in, so the list can be tested without driving the shell. */
export function appActions(newSession: () => void): GettingStartedActions {
  return {
    newWorkspace: requestNewWorkspace,
    openCodeGraph: () => showModuleHome("codegraph"),
    addConnection: () => {
      useWorkbenchTabsStore.getState().focusModule("database");
      useDatabaseStore.getState().setShowNewConnectionDialog(true);
    },
    newSession,
  };
}

export function gettingStartedSteps(
  progress: Omit<ReturnType<typeof useGettingStartedProgress>, "loaded">,
  actions: GettingStartedActions,
): GettingStartedStep[] {
  return [
    {
      id: "workspace",
      title: "Create a workspace",
      detail: "The folders you work in. Agents and boards live inside one.",
      action: "Create",
      done: progress.workspace,
      run: actions.newWorkspace,
    },
    {
      id: "codegraph",
      title: "Open a project in Code Graph",
      detail: "Entry points, and the call chain behind each.",
      action: "Open",
      done: progress.codegraph,
      run: actions.openCodeGraph,
    },
    {
      id: "database",
      title: "Save a database connection",
      detail: "Encrypted in the vault; agents use it by name.",
      action: "Add",
      done: progress.database,
      run: actions.addConnection,
    },
    {
      id: "agent",
      title: "Start an agent session",
      detail: "Its own sandbox, its own folder, DevKit's tools.",
      action: "Start",
      done: progress.agent,
      run: actions.newSession,
    },
  ];
}

const Done: LucideIcon = (({ className, ...props }) => (
  <CircleCheck className={cn(className, "text-status-success")} {...props} />
)) as LucideIcon;

/**
 * The first-run checklist, under the landing module's empty state.
 *
 * A first launch used to open on whatever module the registry listed first —
 * Accounts — with nothing to press and no word on where to begin. This names
 * the four things that make the rest of the app useful, in the order each
 * one makes the next one better, and gets out of the way: it is gone once all
 * four have happened, or as soon as the person hides it.
 */
export function GettingStarted({ onNewSession }: { onNewSession: () => void }) {
  const hidden = useGettingStartedStore((s) => s.hidden);
  const hide = useGettingStartedStore((s) => s.hide);
  const { loaded, ...progress } = useGettingStartedProgress();

  const steps = gettingStartedSteps(progress, appActions(onNewSession));
  const done = steps.filter((step) => step.done).length;
  // Not before the first answer: a checklist that draws every step open and
  // then ticks two of them is a claim it takes back.
  if (hidden || !loaded || done === steps.length) return null;

  return (
    <FramedCard data-testid="getting-started" className="mt-4 w-105 max-w-full text-left">
      <FramedCardHeader
        title="Get started"
        meta={`${done} of ${steps.length}`}
        action={
          <FramedCardAction opens={false} onClick={hide}>
            Hide
          </FramedCardAction>
        }
      />
      <FramedCardPreview flush>
        {steps.map((step) => (
          <FramedCardRow
            key={step.id}
            icon={step.done ? Done : Circle}
            label={<span className={cn(step.done && "text-text-tertiary line-through")}>{step.title}</span>}
            detail={step.done ? undefined : step.detail}
            trailing={step.done ? "Done" : `${step.action} →`}
            onClick={step.done ? undefined : step.run}
          />
        ))}
      </FramedCardPreview>
    </FramedCard>
  );
}
