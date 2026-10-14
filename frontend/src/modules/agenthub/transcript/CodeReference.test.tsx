import { describe, expect, mock, test } from "bun:test";
import * as React from "react";
import { renderToString as renderRaw } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentProse } from "./AgentMessageRich";
import { TranscriptActionsProvider, type TranscriptActions } from "./transcriptActions";
import { CommitPreview, EntityPreview, openCommitRef, openEntity, type CommitOpenDeps, type EntityOpenDeps } from "./CodeReference";
import type { SymbolHit } from "./codeRefs";
import type { RefContext } from "./resolveFileRef";
import { sessionEntities, type EntityHit } from "./entityRefs";
import { entityMenuItems } from "./referenceMenu";
import type { ToolCall } from "../types";

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

const devkitCall = (id: string, tool: string, output: unknown, rawInput?: unknown): ToolCall => ({
  id,
  name: "mcp__devkit__" + tool.split(".").join("_"),
  status: "completed",
  rawInput,
  content: [{ type: "content", content: { type: "text", text: JSON.stringify(output) } }],
});
const ENTITIES = sessionEntities(
  [
    devkitCall("s", "db.schema.get", { namespace: { public: { orders: ["id"] }, archive: { orders: ["id"], invoices: ["id"] } }, columns: {} }),
    devkitCall("t", "kafka.listTopics", { result: [{ name: "orders.created" }] }),
  ].map((toolCall) => ({ id: toolCall.id, kind: "tool_call", toolCall })),
);

describe("tables and topics in prose", () => {
  const withEntities: TranscriptActions = { refContext: CTX, lookupSymbol, openRef: () => {}, entities: ENTITIES };

  test("a settled message links a table the session has seen, with a dotted underline", () => {
    const html = prose("Rows land in `public.orders` and `invoices`.", true, withEntities);
    expect(html.match(/data-testid="agent-entity-ref"/g)?.length).toBe(2);
    expect(html).toContain('data-kind="table"');
    expect(html).toContain("decoration-dotted");
    expect(html).toContain('aria-label="Table public.orders, opens in Database"');
    expect(html).toContain('aria-label="Table archive.invoices, opens in Database"');
  });

  test("a topic the session has seen is a topic reference", () => {
    const html = prose("Published to `orders.created`.", true, withEntities);
    expect(html).toContain('data-testid="agent-entity-ref"');
    expect(html).toContain('data-kind="topic"');
    expect(html).toContain('aria-label="Topic orders.created, opens in Kafka"');
  });

  test("names the session has not seen, or that two schemas share, stay plain", () => {
    const html = prose("Not `public.users`, not `orders`, not `payments`.", true, withEntities);
    expect(html).not.toContain("agent-entity-ref");
  });

  test("a live message keeps them plain", () => {
    expect(prose("Rows land in `public.orders`.", false, withEntities)).not.toContain("agent-entity-ref");
  });

  test("inside a link they are the link only", () => {
    const html = prose("See [`public.orders`](https://example.com/orders).", true, withEntities);
    expect(html).toContain('data-testid="agent-link"');
    expect(html).not.toContain("agent-entity-ref");
  });

  test("a table is not also looked up as a symbol", () => {
    const looked: string[] = [];
    const lookup = (name: string) => {
      looked.push(name);
      return HIT;
    };
    const html = prose("`public.orders` and `Service.Invoke`.", true, { ...withEntities, lookupSymbol: lookup });
    expect(looked).toEqual(["Service.Invoke"]);
    expect(html).toContain('data-testid="agent-entity-ref"');
    expect(html.match(/agent-symbol-ref/g)?.length).toBe(1);
  });
});

const TABLE: EntityHit = { kind: "table", id: "public.orders", name: "orders", schema: "public", table: "orders" };
const TOPIC: EntityHit = { kind: "topic", id: "orders.created", name: "orders.created" };

describe("EntityPreview", () => {
  test("a table says its schema and opens in Database", () => {
    const html = renderToString(<EntityPreview hit={TABLE} onOpen={() => {}} />);
    expect(html).toContain('data-testid="ref-preview-entity"');
    expect(html).toContain(">orders<");
    expect(html).toContain("Table · public");
    expect(html).toContain("Open in Database");
  });

  test("a topic says so and opens in Kafka", () => {
    const html = renderToString(<EntityPreview hit={TOPIC} onOpen={() => {}} />);
    expect(html).toContain(">orders.created<");
    expect(html).toContain(">Topic<");
    expect(html).toContain("Open in Kafka");
  });
});

describe("entityMenuItems", () => {
  test("open where it lives, and copy its name", () => {
    expect(entityMenuItems(TABLE)).toEqual([
      { id: "open", label: "Open in Database" },
      { id: "copy", label: "Copy name" },
    ]);
    expect(entityMenuItems(TOPIC)[0]).toEqual({ id: "open", label: "Open in Kafka" });
  });
});

describe("openEntity", () => {
  const deps = (answer: "ok" | "unavailable" | Error) => {
    const calls: unknown[][] = [];
    const d: EntityOpenDeps = {
      dispatch: mock(async (...args: unknown[]) => {
        calls.push(args);
        if (answer instanceof Error) throw answer;
        return answer;
      }) as unknown as EntityOpenDeps["dispatch"],
      toastError: mock(() => {}),
    };
    return { d, calls };
  };

  test("a table opens through database.reveal with its schema.table id", async () => {
    const { d, calls } = deps("ok");
    await openEntity(TABLE, "beside", d);
    expect(calls).toEqual([["database.reveal", { module: "database", kind: "table", id: "public.orders", label: "orders" }, { placement: "beside" }]]);
    expect(d.toastError).not.toHaveBeenCalled();
  });

  test("a topic opens through kafka.reveal", async () => {
    const { d, calls } = deps("ok");
    await openEntity(TOPIC, "replace", d);
    expect(calls).toEqual([["kafka.reveal", { module: "kafka", kind: "topic", id: "orders.created", label: "orders.created" }, { placement: "replace" }]]);
  });

  test("says so when it could not be opened", async () => {
    const { d } = deps("unavailable");
    await openEntity(TOPIC, "beside", d);
    expect(d.toastError).toHaveBeenCalledTimes(1);
    expect((d.toastError as ReturnType<typeof mock>).mock.calls[0][0]).toStartWith("Couldn't open orders.created");
  });

  test("and when the reveal throws", async () => {
    const { d } = deps(new Error("boom"));
    await openEntity(TABLE, "beside", d);
    expect((d.toastError as ReturnType<typeof mock>).mock.calls[0][0]).toStartWith("Couldn't open public.orders");
  });
});
