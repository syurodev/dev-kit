import { useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Check, Copy, Hammer, Loader2, TriangleAlert, Wrench } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { errorMessage } from "@/bridge/errors";
import { copyText } from "@/bridge/clipboard";
import {
  buildCodeGraphAnalyzer,
  onCodeGraphAnalyzerBuildOutput,
  type CodeGraphInstallCommand,
  type CodeGraphRequirement,
  type CodeGraphToolchainStatus,
} from "@/bridge/codegraph";
import { createProgressBuffer } from "@/lib/progressBuffer";

/**
 * What CodeGraph shows when its analyzer has not been built.
 *
 * Nothing in the module works without an analyzer, so the setup takes the whole
 * canvas rather than a red "connection_failed" line in a sidebar nobody is
 * looking at.
 *
 * Per-language by construction: a machine can be ready for one language and not
 * another, so each toolchain gets its own card, requirements and build.
 */
export function AnalyzerSetup({
  statuses,
  onBuilt,
}: {
  statuses: CodeGraphToolchainStatus[];
  onBuilt: () => void;
}) {
  // Two different problems wear the same panel, and telling a user to build
  // something that is already built is worse than saying nothing. An installed
  // DevKit ships the analyzer but no runtime to launch it, so "not built" and
  // "cannot run" are both real and they need different sentences.
  const nothingBuilt = statuses.some((s) => !s.hasArtifact);

  return (
    <div className="flex h-full flex-col items-center overflow-auto p-8">
      <div className="flex w-full max-w-180 flex-col items-center gap-6">
        <span className="flex size-16 items-center justify-center rounded-full border border-glass-border bg-state-soft">
          <Wrench size={26} className="text-text-secondary" />
        </span>
        <div className="flex flex-col items-center gap-2 text-center">
          <h3 className="text-title-16 text-text-primary">
            {nothingBuilt ? "CodeGraph needs its analyzer" : "CodeGraph cannot start its analyzer"}
          </h3>
          <p className="max-w-140 text-body-13 text-text-secondary">
            {nothingBuilt
              ? "The analyzer is what reads your source and builds the graph. It ships as source and is compiled once, on your machine, with your own toolchain — DevKit can run that build for you."
              : "The analyzer is here, but the runtime that launches it is not. Install what is listed below and reopen this view — nothing needs rebuilding."}
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
  const [lines, setLines] = useState<string[]>([]);
  const [error, setError] = useState("");
  const logRef = useRef<HTMLPreElement>(null);

  const buildMutation = useMutation({
    mutationFn: () => buildCodeGraphAnalyzer(status.language),
    onSuccess: () => {
      onBuilt();
    },
    onError: (e) => {
      setError(errorMessage(e));
    },
  });

  const building = buildMutation.isPending;

  // Subscribe for the card's whole life rather than only while building: the
  // first lines arrive before the promise has even been awaited, and a
  // subscription set up inside the click handler races them.
  useEffect(() => {
    // On a fixed beat, not per line: a Maven build on a cold cache writes
    // thousands of lines, and one setState each is thousands of renders of
    // text that scrolls straight past (smoothness 5, "progress has a beat").
    const buffer = createProgressBuffer<string>((batch) =>
      // Bounded: only the tail is ever read. The head is in the terminal for
      // anyone who wants it.
      setLines((prev) => [...prev, ...batch].slice(-400)),
    );
    const off = onCodeGraphAnalyzerBuildOutput((language, line) => {
      if (language !== status.language) return;
      buffer.push(line);
    });
    return () => {
      off();
      buffer.dispose();
    };
  }, [status.language]);

  // Follow the tail, which is the only part of a build log anyone watches.
  useEffect(() => {
    const log = logRef.current;
    if (log) log.scrollTop = log.scrollHeight;
  }, [lines]);

  async function build() {
    setError("");
    setLines([]);
    await buildMutation.mutateAsync();
  }

  return (
    <div className="flex w-full flex-col gap-4 rounded-lg border border-border-subtle bg-surface-base p-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex flex-col gap-1">
          <span className="text-body-13-medium text-text-primary">{status.label} analyzer</span>
          <span className="text-code-11 text-text-tertiary">{status.artifact}</span>
        </div>
        {/* No build button when the artifact is already there: the problem is
            the runtime, and building again would not touch it. */}
        {!status.hasArtifact && (
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
        )}
      </div>

      <ul className="flex flex-col gap-2">
        {status.requirements.map((requirement) => (
          <RequirementRow key={requirement.program} requirement={requirement} />
        ))}
      </ul>

      {!status.hasArtifact && !status.buildable && (
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

/**
 * One install command: which ecosystem it belongs to, the command itself, and a
 * click that copies it.
 *
 * The list is already narrowed to this platform by the backend, so what is on
 * screen is the handful of lines that could actually be typed here.
 */
function InstallCommandRow({ option }: { option: CodeGraphInstallCommand }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await copyText(option.command);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard is unavailable in some webviews; the command is selectable.
    }
  }

  return (
    <li className="flex items-center gap-2">
      <span className="w-32 shrink-0 text-caption-11 text-text-tertiary">{option.label}</span>
      <button
        type="button"
        onClick={() => void copy()}
        title="Copy"
        className="group/cmd flex min-w-0 flex-1 items-center gap-2 rounded-md border border-border-subtle bg-bg-canvas px-2.5 py-1 text-left transition-colors hover:border-status-info"
      >
        <code className="min-w-0 flex-1 truncate text-code-11 text-text-secondary">
          {option.command}
        </code>
        {copied ? (
          <Check className="size-3.5 shrink-0 text-status-success" />
        ) : (
          <Copy className="size-3.5 shrink-0 text-text-tertiary opacity-0 transition-opacity group-hover/cmd:opacity-100" />
        )}
      </button>
    </li>
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
          <>
            {requirement.problem && (
              <span className="text-body-12 text-text-tertiary">{requirement.problem}.</span>
            )}
            {/* Commands as commands, not as backticks inside a sentence. The
                panel renders text rather than markdown, so the old prose
                printed its own backticks — and a command buried in a
                paragraph is one nobody can copy. */}
            {requirement.install.length > 0 && (
              <ul className="mt-1.5 flex flex-col gap-1">
                {requirement.install.map((option) => (
                  <InstallCommandRow key={option.command} option={option} />
                ))}
              </ul>
            )}
            {requirement.installHint && (
              <span className="mt-1.5 text-body-12 text-text-tertiary">
                {requirement.installHint}
              </span>
            )}
          </>
        )}
      </div>
    </li>
  );
}
