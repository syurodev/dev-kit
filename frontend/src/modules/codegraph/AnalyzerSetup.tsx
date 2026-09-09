import { useEffect, useRef, useState } from "react";
import { Check, Hammer, Loader2, TriangleAlert, Wrench } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { errorMessage } from "@/bridge/errors";
import {
  buildCodeGraphAnalyzer,
  onCodeGraphAnalyzerBuildOutput,
  type CodeGraphRequirement,
  type CodeGraphToolchainStatus,
} from "@/bridge/codegraph";

/**
 * What CodeGraph shows when its analyzer has not been built.
 *
 * This replaces a red line reading "connection_failed: CodeGraph analyzer not
 * configured." — true, unactionable, and shown in a sidebar the user was not
 * looking at. Nothing in the module works without an analyzer, so the setup
 * takes the whole canvas rather than hiding in a toast.
 *
 * It is per-language by construction: CodeGraph analyzes Java today and is
 * meant to grow, and a machine can be ready for one language and not another.
 * Each toolchain gets its own card, its own requirement list and its own build.
 */
export function AnalyzerSetup({
  statuses,
  onBuilt,
}: {
  statuses: CodeGraphToolchainStatus[];
  onBuilt: () => void;
}) {
  return (
    <div className="flex h-full flex-col items-center overflow-auto p-8">
      <div className="flex w-full max-w-180 flex-col items-center gap-6">
        <span className="flex size-16 items-center justify-center rounded-full border border-glass-border bg-state-soft">
          <Wrench size={26} className="text-text-secondary" />
        </span>
        <div className="flex flex-col items-center gap-2 text-center">
          <h3 className="text-title-16 text-text-primary">CodeGraph needs its analyzer</h3>
          <p className="max-w-140 text-body-13 text-text-secondary">
            The analyzer is what reads your source and builds the graph. It ships as source and is
            compiled once, on your machine, with your own toolchain — DevKit can run that build for
            you.
          </p>
        </div>
        {statuses.map((status) => (
          <ToolchainCard key={status.language} status={status} onBuilt={onBuilt} />
        ))}
      </div>
    </div>
  );
}

function ToolchainCard({
  status,
  onBuilt,
}: {
  status: CodeGraphToolchainStatus;
  onBuilt: () => void;
}) {
  const [building, setBuilding] = useState(false);
  const [lines, setLines] = useState<string[]>([]);
  const [error, setError] = useState("");
  const logRef = useRef<HTMLPreElement>(null);

  // Subscribe for the card's whole life rather than only while building: the
  // first lines arrive before the promise has even been awaited, and a
  // subscription set up inside the click handler races them.
  useEffect(
    () =>
      onCodeGraphAnalyzerBuildOutput((language, line) => {
        if (language !== status.language) return;
        // Bounded: a Maven build on a cold cache writes thousands of lines and
        // only the tail is ever read. The head is in the terminal for anyone
        // who wants it.
        setLines((prev) => [...prev, line].slice(-400));
      }),
    [status.language],
  );

  // Follow the tail, which is the only part of a build log anyone watches.
  useEffect(() => {
    const log = logRef.current;
    if (log) log.scrollTop = log.scrollHeight;
  }, [lines]);

  async function build() {
    setBuilding(true);
    setError("");
    setLines([]);
    try {
      await buildCodeGraphAnalyzer(status.language);
      onBuilt();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBuilding(false);
    }
  }

  return (
    <div className="flex w-full flex-col gap-4 rounded-lg border border-border-subtle bg-surface-base p-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex flex-col gap-1">
          <span className="text-body-13-medium text-text-primary">{status.label} analyzer</span>
          <span className="text-code-11 text-text-tertiary">{status.artifact}</span>
        </div>
        <Button
          size="sm"
          disabled={building || !status.buildable}
          onClick={() => void build()}
          title={
            status.buildable
              ? status.buildCommand
              : "Install what is missing below, then reload this panel."
          }
        >
          {building ? <Loader2 className="animate-spin" /> : <Hammer />}
          {building ? "Building…" : "Build analyzer"}
        </Button>
      </div>

      <ul className="flex flex-col gap-2">
        {status.requirements.map((requirement) => (
          <RequirementRow key={requirement.program} requirement={requirement} />
        ))}
      </ul>

      {!status.buildable && (
        <p className="text-body-12 text-text-tertiary">
          Prefer your own terminal? <code className="text-code-11">{status.buildCommand}</code> in{" "}
          <code className="text-code-11">analyzer/</code> does the same thing.
        </p>
      )}

      {error && (
        <p className="text-body-12 text-status-danger">
          {error} The build output below says what went wrong.
        </p>
      )}

      {lines.length > 0 && (
        <pre
          ref={logRef}
          className="max-h-64 overflow-auto rounded-md border border-border-subtle bg-bg-canvas p-3 text-code-11 text-text-secondary"
        >
          {lines.join("\n")}
        </pre>
      )}
    </div>
  );
}

function RequirementRow({ requirement }: { requirement: CodeGraphRequirement }) {
  const version = requirement.found ? ` ${requirement.found}` : "";
  return (
    <li className="flex items-start gap-2.5">
      {requirement.met ? (
        <Check className="mt-0.5 size-4 shrink-0 text-status-success" />
      ) : (
        <TriangleAlert className="mt-0.5 size-4 shrink-0 text-status-warning" />
      )}
      <div className="flex min-w-0 flex-col gap-0.5">
        <span
          className={cn(
            "text-body-13",
            requirement.met ? "text-text-secondary" : "text-text-primary",
          )}
        >
          {requirement.label}
          {version}
          {requirement.minVersion && !requirement.met && ` — needs ${requirement.minVersion}+`}
        </span>
        {/* Which one, and where it came from. On a machine with several JDKs
            that is the next thing the user wants to know, and when the answer
            is "the one inside your IDE" it is also the reassurance that
            nothing needs installing. */}
        {requirement.met && requirement.path && (
          <span className="truncate text-code-11 text-text-tertiary">
            {requirement.source}: {requirement.path}
          </span>
        )}
        {!requirement.met && (
          <span className="text-body-12 text-text-tertiary">
            {requirement.problem ? `${requirement.problem}. ` : ""}
            {requirement.installHint}
          </span>
        )}
      </div>
    </li>
  );
}
