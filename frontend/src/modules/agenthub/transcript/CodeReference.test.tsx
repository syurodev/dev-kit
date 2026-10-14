import { describe, expect, mock, test } from "bun:test";
import * as React from "react";
import { renderToString as renderRaw } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentProse } from "./AgentMessageRich";
import { TranscriptActionsProvider, type TranscriptActions } from "./transcriptActions";
import { CommitPreview, openCommitRef, type CommitOpenDeps } from "./CodeReference";
import type { SymbolHit } from "./codeRefs";
import type { RefContext } from "./resolveFileRef";

const renderToString = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);

const CTX: RefContext = { sessionId: "s1", workspaceId: "ws", projectPath: "/work/repo" };
const HIT: SymbolHit = {
  workspaceId: "ws",
  folderId: "api",
  folderName: "api",
  filePath: "internal/agenthub/service.go",
  line: 42,
  kind: "method",
  label: "Service.Invoke",
};
const lookupSymbol = (name: string) => (name === "Service.Invoke" ? HIT : undefined);

function prose(text: string, settled: boolean, actions: TranscriptActions = { refContext: CTX, lookupSymbol, openRef: () => {} }) {
  return renderToString(
    <TranscriptActionsProvider value={actions}>
      <AgentProse text={text} settled={settled} />
    </TranscriptActionsProvider>,
  );
}

describe("symbols in prose", () => {
  test("a settled message links a symbol that resolves, with a dotted underline", () => {
    const html = prose("Call `Service.Invoke` here.", true);
    expect(html).toContain('data-testid="agent-symbol-ref"');
    expect(html).toContain("decoration-dotted");
    expect(html).toContain('role="button"');
    expect(html).toContain(">Service.Invoke<");
  });

  test("a live message keeps the span plain", () => {
    const html = prose("Call `Service.Invoke` here.", false);
    expect(html).not.toContain("agent-symbol-ref");
    expect(html).not.toContain("decoration-dotted");
    expect(html).toContain(">Service.Invoke</code>");
  });

  test("a name that does not resolve stays plain", () => {
    expect(prose("Call `Other.Thing` here.", true)).not.toContain("agent-symbol-ref");
  });

  test("a session with no workspace looks nothing up", () => {
    const html = prose("Call `Service.Invoke` here.", true, { refContext: { ...CTX, workspaceId: "" }, lookupSymbol, openRef: () => {} });
    expect(html).not.toContain("agent-symbol-ref");
  });

  test("a file reference is still a file reference", () => {
    const html = prose("See `internal/agenthub/service.go`.", true);
    expect(html).toContain('data-testid="agent-file-ref"');
    expect(html).not.toContain("agent-symbol-ref");
  });
});

describe("code inside a link", () => {
  test("a commit hash linked to its page is the link only", () => {
    const html = prose("Fixed in [`abc1234`](https://github.com/o/r/commit/abc1234).", true);
    expect(html).toContain('data-testid="agent-link"');
    expect(html).not.toContain("agent-commit-ref");
    expect(html).not.toContain('role="button"');
  });

  test("a symbol or a path linked to a file is the link only", () => {
    const html = prose("See [`Service.Invoke`](src/service.go:12) and [`src/service.go`](src/service.go).", true);
    expect(html.match(/data-testid="agent-link"/g)?.length).toBe(2);
    expect(html).not.toContain("agent-symbol-ref");
    expect(html).not.toContain("agent-file-ref");
  });

  test("emphasis inside a link is still inside it", () => {
    expect(prose("[**`abc1234`**](https://x.dev/c/abc1234)", true)).not.toContain("agent-commit-ref");
  });
});

describe("accessible names", () => {
  test("each reference says what it is and where it opens", () => {
    const html = prose("`Service.Invoke`, `a1b2c3d4e5` and `auth/session.go:88`.", true);
    expect(html).toContain('aria-label="Symbol Service.Invoke, opens definition"');
    expect(html).toContain('aria-label="Commit a1b2c3d, opens in Git"');
    expect(html).toContain('aria-label="File auth/session.go:88, opens in editor"');
  });
});

describe("commits in prose", () => {
  test("a settled message links a commit hash, with a dotted underline", () => {
    const html = prose("Landed in `a1b2c3d`.", true);
    expect(html).toContain('data-testid="agent-commit-ref"');
    expect(html).toContain("decoration-dotted");
  });

  test("a live message keeps the hash plain", () => {
    expect(prose("Landed in `a1b2c3d`.", false)).not.toContain("agent-commit-ref");
  });

  test("with no session there is no repository to open it in", () => {
    expect(prose("Landed in `a1b2c3d`.", true, {})).not.toContain("agent-commit-ref");
  });
});

describe("CommitPreview", () => {
  test("the short sha, the folder and Open in Git, which says it moves Git", () => {
    const html = renderToString(<CommitPreview sha="a1b2c3d4e5f6a7b8" folder="repo" onOpen={() => {}} />);
    expect(html).toContain('data-testid="ref-preview-commit"');
    expect(html).toContain(">a1b2c3d<");
    expect(html).toContain("Commit in repo");
    expect(html).toContain("Open in Git (switches Git to repo)");
  });
});

describe("openCommitRef", () => {
  const deps = (over: Partial<CommitOpenDeps> = {}) => {
    const calls: string[] = [];
    const d: CommitOpenDeps = {
      folders: mock(async () => [{ id: "a", path: "/work" }, { id: "b", path: "/work/repo" }]),
      pin: mock(async (_m: "git", path: string) => {
        calls.push(`pin ${path}`);
      }),
      dispatch: mock(async (name: string, ref: { id: string; label: string }) => {
        calls.push(`${name} ${ref.id} ${ref.label}`);
        return "ok" as const;
      }) as unknown as CommitOpenDeps["dispatch"],
      toastError: mock(() => {}),
      ...over,
    };
    return { d, calls };
  };

  test("pins Git to the project's folder, then reveals the commit", async () => {
    const { d, calls } = deps();
    await openCommitRef("a1b2c3d4e5", CTX, "beside", d);
    expect(calls).toEqual(["pin /work/repo", "git.reveal a1b2c3d4e5 a1b2c3d"]);
    expect(d.toastError).not.toHaveBeenCalled();
  });

  test("a scratch session pins its project", async () => {
    const { d, calls } = deps();
    await openCommitRef("a1b2c3d", { ...CTX, workspaceId: "", projectPath: "/tmp/scratch" }, "beside", d);
    expect(d.folders).not.toHaveBeenCalled();
    expect(calls[0]).toBe("pin /tmp/scratch");
  });

  test("says so when Git is not available", async () => {
    const { d } = deps({ dispatch: mock(async () => "unavailable" as const) as unknown as CommitOpenDeps["dispatch"] });
    await openCommitRef("a1b2c3d", CTX, "beside", d);
    expect(d.toastError).toHaveBeenCalledTimes(1);
  });

  test("a failed pin opens nothing and says why", async () => {
    const { d, calls } = deps({
      pin: mock(async () => {
        throw new Error("boom");
      }),
    });
    await openCommitRef("a1b2c3d", CTX, "beside", d);
    expect(calls).toEqual([]);
    expect(d.toastError).toHaveBeenCalledTimes(1);
  });
});
