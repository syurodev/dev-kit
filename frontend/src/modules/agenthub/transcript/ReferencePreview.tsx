import * as React from "react";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { Columns2, Copy, ExternalLink, FileText, FolderTree, Link2 } from "lucide-react";
import { toast } from "sonner";

import { cn } from "@/lib/utils";
import { queryClient } from "@/lib/query";
import { errorMessage } from "@/bridge/errors";
import { copyText } from "@/bridge/clipboard";
import { openExternal } from "@/bridge/browser";
import { readFolderFile, type FileRead } from "@/bridge/workspaceCode";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { useHighlight, type Highlight } from "@/components/code/CodeBlock";
import { languageForPath } from "@/components/code/languages";
import type { Placement } from "@/workbench/intents";
import type { FileRef } from "./fileRef";
import { fileRefQuery, useFileRefResolution, type RefContext, type RefResolution, type RefTarget } from "./resolveFileRef";
import { openRefTarget } from "./openReference";
import { RefPickerList } from "./RefPicker";
import { copiedPath, fileMenuItems, urlMenuItems, type FileMenuId, type MenuItem, type UrlMenuId } from "./referenceMenu";
import { useTranscriptActions } from "./transcriptActions";

/** Lines shown either side of the target line. */
const CONTEXT_LINES = 4;

/**
 * The lines a preview shows: four either side of `line`, clamped to the file,
 * or the first nine when there is no line. A trailing newline is not a line.
 */
export function previewSlice(content: string, line: number | undefined): { startLine: number; lines: string[] } {
  const all = content.split("\n");
  if (all.length > 1 && all[all.length - 1] === "") all.pop();
  if (line === undefined) return { startLine: 1, lines: all.slice(0, CONTEXT_LINES * 2 + 1) };
  const at = Math.min(Math.max(line, 1), all.length);
  const start = Math.max(1, at - CONTEXT_LINES);
  const end = Math.min(all.length, at + CONTEXT_LINES);
  return { startLine: start, lines: all.slice(start - 1, end) };
}

/** The code split into lines, each with its colours; a syntax run that spans lines is cut at each newline. */
export function highlightedLines(code: string, highlight: Pick<Highlight, "code" | "segments"> | null): React.ReactNode[] {
  if (!highlight || highlight.code !== code) return code.split("\n");
  const lines: React.ReactNode[][] = [[]];
  let key = 0;
  for (const segment of highlight.segments) {
    segment.text.split("\n").forEach((part, index) => {
      if (index > 0) lines.push([]);
      if (part === "") return;
      lines[lines.length - 1].push(
        segment.className ? (
          <span key={key++} className={segment.className}>
            {part}
          </span>
        ) : (
          part
        ),
      );
    });
  }
  return lines.map((nodes) => (nodes.length === 1 && typeof nodes[0] === "string" ? nodes[0] : nodes.length === 0 ? "" : nodes));
}

type Controls = {
  /** Closes the hover card, before an action moves the person elsewhere. */
  close: () => void;
  /** The reference in the prose, for a picker to anchor to. */
  anchor: () => HTMLElement | null;
};

/**
 * A reference in prose with a hover preview and a context menu, the
 * ModuleSwitcher composition: the hover card's trigger is the context menu's
 * trigger, and the card gives way when the menu opens. The preview is only
 * mounted while the card is open, so nothing is read before someone hovers.
 */
export function ReferencePreview({
  preview,
  menu,
  children,
}: {
  preview: (controls: Controls) => React.ReactNode;
  menu: ((controls: Controls) => React.ReactNode) | null;
  children: React.ReactElement;
}) {
  const [open, setOpen] = React.useState(false);
  const triggerRef = React.useRef<HTMLElement | null>(null);
  const controls: Controls = { close: () => setOpen(false), anchor: () => triggerRef.current };
  return (
    <HoverCard open={open} onOpenChange={setOpen} openDelay={400} closeDelay={120}>
      <ContextMenu onOpenChange={(menuOpen) => menuOpen && setOpen(false)}>
        <HoverCardTrigger
          asChild
          ref={(node: HTMLElement | null) => {
            triggerRef.current = node;
          }}
        >
          {/* select-text: the reference is part of a sentence someone may select. */}
          <ContextMenuTrigger asChild className="select-text">
            {children}
          </ContextMenuTrigger>
        </HoverCardTrigger>
        {menu ? menu(controls) : null}
      </ContextMenu>
      <HoverCardContent align="start" className="w-96 p-0">
        {preview(controls)}
      </HoverCardContent>
    </HoverCard>
  );
}

export type FilePreviewState =
  | { kind: "loading"; target?: RefTarget }
  | { kind: "text"; target: RefTarget; content: string }
  | { kind: "unreadable"; target: RefTarget; reason: "binary" | "too_large" | "not_regular" }
  | { kind: "missing" }
  | { kind: "ambiguous"; targets: RefTarget[] }
  | { kind: "unsupported" }
  | { kind: "error"; message: string };

const UNREADABLE: Record<"binary" | "too_large" | "not_regular", string> = {
  binary: "Binary file",
  too_large: "Too large to preview",
  not_regular: "Not a regular file",
};

function splitRel(rel: string): { name: string; dir: string } {
  const cut = rel.lastIndexOf("/");
  return cut < 0 ? { name: rel, dir: "" } : { name: rel.slice(cut + 1), dir: rel.slice(0, cut + 1) };
}

/** What the read error says, in the preview's words: the bridge reports both limits under one code. */
function readFailure(target: RefTarget, error: unknown): FilePreviewState {
  const message = errorMessage(error);
  if (/not text/i.test(message)) return { kind: "unreadable", target, reason: "binary" };
  if (/too large/i.test(message)) return { kind: "unreadable", target, reason: "too_large" };
  return { kind: "error", message };
}

/** The preview's state from the resolution and the read. */
export function filePreviewState(
  ctx: RefContext | null,
  resolution: Pick<UseQueryResult<RefResolution>, "data" | "error">,
  read: Pick<UseQueryResult<FileRead>, "data" | "error">,
): FilePreviewState {
  if (!ctx) return { kind: "unsupported" };
  if (resolution.error) return { kind: "error", message: errorMessage(resolution.error) };
  const data = resolution.data;
  if (!data) return { kind: "loading" };
  if (data.kind !== "found") return data;
  const target = data.target;
  if (target.unreadable) return { kind: "unreadable", target, reason: target.unreadable };
  if (read.error) return readFailure(target, read.error);
  if (read.data) return { kind: "text", target, content: read.data.content };
  return { kind: "loading", target };
}

/** The path as a header: folder dim and cut from the left, file name bold, the folder's name on the right. */
function PreviewHeader({ path, line, folderName }: { path: string; line?: number; folderName?: string }) {
  const { name, dir } = splitRel(path);
  return (
    <div className="flex min-w-0 items-baseline gap-2 border-b border-border-subtle p-2.5">
      <span className="flex min-w-0 flex-1 items-baseline text-code-12" title={path}>
        {dir ? (
          // rtl puts the ellipsis on the left, where the least telling part of a path is.
          <span dir="rtl" className="min-w-0 truncate text-left text-text-tertiary">
            <bdi dir="ltr">{dir}</bdi>
          </span>
        ) : null}
        <span className="shrink-0 font-medium text-text-primary">{name}</span>
        {line !== undefined ? <span className="shrink-0 text-text-tertiary">:{line}</span> : null}
      </span>
      {folderName ? <span className="shrink-0 text-caption-11 text-text-tertiary">{folderName}</span> : null}
    </div>
  );
}

function PreviewLines({ target, content }: { target: RefTarget; content: string }) {
  const slice = previewSlice(content, target.line);
  const code = slice.lines.join("\n");
  const highlight = useHighlight(code, languageForPath(target.rel) ?? "text");
  const lines = highlightedLines(code, highlight);
  const numbers = lines.map((_, index) => slice.startLine + index);
  // Two columns rather than a table: every cell is one line high (h-lh, so an
  // empty line keeps its height), and the target row is raised in both.
  return (
    <div className="flex overflow-hidden py-2.5 text-code-12">
      <div aria-hidden="true" className="flex shrink-0 flex-col text-right text-text-tertiary tabular-nums select-none">
        {numbers.map((number) => (
          <span key={number} className={cn("h-lh pr-3 pl-2.5", number === target.line && "bg-state-soft")}>
            {number}
          </span>
        ))}
      </div>
      <div className="flex min-w-0 flex-1 flex-col text-text-primary">
        {lines.map((node, index) => {
          const isTarget = numbers[index] === target.line;
          return (
            <span
              key={numbers[index]}
              data-testid="ref-preview-line"
              data-target={isTarget ? "true" : undefined}
              className={cn("h-lh overflow-hidden pr-2.5 text-ellipsis whitespace-pre", isTarget && "bg-state-soft")}
            >
              {node}
            </span>
          );
        })}
      </div>
    </div>
  );
}

function PreviewNote({ children }: { children: React.ReactNode }) {
  return <p className="p-2.5 text-body-13 text-text-secondary">{children}</p>;
}

function PreviewFooter({ children }: { children: React.ReactNode }) {
  return <div className="flex justify-end gap-1.5 border-t border-border-subtle p-2.5">{children}</div>;
}

/** The file preview for a given state; the hooks live in FileRefPreview. */
export function FilePreviewView({
  fileRef,
  state,
  onOpen,
  onPick,
}: {
  fileRef: FileRef;
  state: FilePreviewState;
  onOpen: (placement: Placement) => void;
  onPick: (target: RefTarget) => void;
}) {
  const target = "target" in state ? state.target : undefined;
  const header = target ? (
    <PreviewHeader path={target.rel} line={fileRef.line} folderName={target.folderName} />
  ) : (
    <PreviewHeader path={fileRef.path} line={fileRef.line} />
  );
  const openButtons = (
    <PreviewFooter>
      <Button size="xs" variant="ghost" onClick={() => onOpen("replace")}>
        Open here
      </Button>
      <Button size="xs" variant="secondary" onClick={() => onOpen("beside")}>
        Open
      </Button>
    </PreviewFooter>
  );

  let body: React.ReactNode;
  let footer: React.ReactNode = null;
  switch (state.kind) {
    case "loading":
      body = (
        <div className="flex flex-col gap-1.5 p-2.5 text-code-12">
          <Skeleton className="h-lh w-3/4" />
          <Skeleton className="h-lh w-1/2" />
          <Skeleton className="h-lh w-2/3" />
        </div>
      );
      footer = openButtons;
      break;
    case "text":
      body = <PreviewLines target={state.target} content={state.content} />;
      footer = openButtons;
      break;
    case "unreadable":
      body = <PreviewNote>{UNREADABLE[state.reason]}</PreviewNote>;
      footer = openButtons;
      break;
    case "missing":
      body = <PreviewNote>Not found in this workspace</PreviewNote>;
      break;
    case "ambiguous":
      body = (
        <div className="flex flex-col gap-1 p-1">
          <p className="px-2 pt-1 text-caption-11 text-text-tertiary">Which file?</p>
          <RefPickerList targets={state.targets} onPick={onPick} />
        </div>
      );
      break;
    case "unsupported":
      footer = (
        <PreviewFooter>
          <Button size="xs" variant="secondary" onClick={() => onOpen("beside")}>
            Open in Code Graph
          </Button>
        </PreviewFooter>
      );
      break;
    case "error":
      body = <PreviewNote>{state.message}</PreviewNote>;
      break;
  }
  return (
    <div data-testid="ref-preview-file" className="flex flex-col">
      {header}
      {body}
      {footer}
    </div>
  );
}

/** Resolves and reads the reference — mounted only while the card is open. */
function FileRefPreview({
  fileRef,
  refContext,
  onOpen,
  onPick,
}: {
  fileRef: FileRef;
  refContext: RefContext | null;
  onOpen: (placement: Placement) => void;
  onPick: (target: RefTarget) => void;
}) {
  const resolution = useFileRefResolution(fileRef, refContext, true);
  const found = resolution.data?.kind === "found" ? resolution.data.target : null;
  const read = useQuery({
    queryKey: ["agenthub", "fileRefRead", found?.workspaceId ?? "", found?.folderId ?? "", found?.rel ?? ""] as const,
    queryFn: () => readFolderFile(found!.workspaceId, found!.folderId, found!.rel),
    enabled: found !== null && !found.unreadable,
    staleTime: 30_000,
  });
  return <FilePreviewView fileRef={fileRef} state={filePreviewState(refContext, resolution, read)} onOpen={onOpen} onPick={onPick} />;
}

function hostOf(url: string): string {
  try {
    const parsed = new URL(url);
    return parsed.host || parsed.protocol.replace(/:$/, "");
  } catch {
    return "";
  }
}

async function copyWithNotice(text: string, notice: string) {
  try {
    await copyText(text);
    toast.success(notice);
  } catch (error) {
    toast.error(`Couldn't copy: ${errorMessage(error)}`);
  }
}

/** A web link's preview: the whole URL, its host, and nothing fetched. */
export function UrlPreview({ url, onDone }: { url: string; onDone?: () => void }) {
  const host = hostOf(url);
  return (
    <div data-testid="ref-preview-url" className="flex flex-col">
      <div className="flex flex-col gap-1 p-2.5">
        {host ? <span className="text-caption-11 text-text-tertiary">{host}</span> : null}
        <p className="text-code-12 break-all text-text-primary">{url}</p>
      </div>
      <PreviewFooter>
        <Button
          size="xs"
          variant="ghost"
          onClick={() => {
            onDone?.();
            void copyWithNotice(url, "Link copied");
          }}
        >
          Copy link
        </Button>
        <Button
          size="xs"
          variant="secondary"
          onClick={() => {
            onDone?.();
            void openExternal(url);
          }}
        >
          Open in browser
        </Button>
      </PreviewFooter>
    </div>
  );
}

const FILE_ICONS: Record<FileMenuId, React.ReactNode> = {
  "open-beside": <Columns2 />,
  "open-here": <FileText />,
  "copy-path": <Copy />,
  "copy-path-line": <Copy />,
  reveal: <FolderTree />,
};
const URL_ICONS: Record<UrlMenuId, React.ReactNode> = { open: <ExternalLink />, copy: <Link2 /> };

function ReferenceMenu<Id extends string>({
  items,
  icons,
  onSelect,
}: {
  items: MenuItem<Id>[];
  icons: Record<Id, React.ReactNode>;
  onSelect: (id: Id) => void;
}) {
  return (
    <ContextMenuContent>
      {items.map((item) => (
        <ContextMenuItem key={item.id} icon={icons[item.id]} onSelect={() => onSelect(item.id)}>
          {item.label}
        </ContextMenuItem>
      ))}
    </ContextMenuContent>
  );
}

/** The resolved folder-relative path, if the resolution is already cached; a right-click never starts a lookup. */
function cachedRel(fileRef: FileRef, ctx: RefContext | null): string | undefined {
  if (!ctx) return undefined;
  const resolution = queryClient.getQueryData<RefResolution>(fileRefQuery(fileRef, ctx).queryKey);
  return resolution?.kind === "found" ? resolution.target.rel : undefined;
}

/** Opens a candidate picked in the preview, and says so when it cannot. */
async function openPicked(target: RefTarget, line: number | undefined) {
  try {
    if ((await openRefTarget(target, line)) === "unavailable") toast.error(`Couldn't find ${target.rel} in this workspace`);
  } catch (error) {
    toast.error(`Couldn't open ${target.rel}: ${errorMessage(error)}`);
  }
}

/** A file reference in prose (inline code or a link), with its preview and menu. */
export function FileReference({ fileRef, children }: { fileRef: FileRef; children: React.ReactElement }) {
  const { refContext, openRef, onOpenFile } = useTranscriptActions();
  const ctx = refContext ?? null;
  const open = (anchor: HTMLElement | null, placement: Placement) => {
    if (openRef) openRef(fileRef, anchor, placement);
    else onOpenFile?.(fileRef.path, fileRef.line);
  };
  return (
    <ReferencePreview
      preview={({ close, anchor }) => (
        <FileRefPreview
          fileRef={fileRef}
          refContext={ctx}
          onOpen={(placement) => {
            close();
            open(anchor(), placement);
          }}
          onPick={(target) => {
            close();
            void openPicked(target, fileRef.line);
          }}
        />
      )}
      menu={({ anchor }) => (
        <ReferenceMenu
          items={fileMenuItems(fileRef)}
          icons={FILE_ICONS}
          onSelect={(id) => {
            switch (id) {
              case "open-beside":
                open(anchor(), "beside");
                return;
              // Opening here by ids also switches Code Graph's sidebar to Files.
              case "open-here":
              case "reveal":
                open(anchor(), "replace");
                return;
              case "copy-path":
              case "copy-path-line":
                void copyWithNotice(copiedPath(fileRef, cachedRel(fileRef, ctx), id === "copy-path-line"), "Path copied");
                return;
            }
          }}
        />
      )}
    >
      {children}
    </ReferencePreview>
  );
}

/** A web link in prose, with its preview and menu. */
export function UrlReference({ url, children }: { url: string; children: React.ReactElement }) {
  return (
    <ReferencePreview
      preview={({ close }) => <UrlPreview url={url} onDone={close} />}
      menu={() => (
        <ReferenceMenu
          items={urlMenuItems(url)}
          icons={URL_ICONS}
          onSelect={(id) => {
            if (id === "open") void openExternal(url);
            else void copyWithNotice(url, "Link copied");
          }}
        />
      )}
    >
      {children}
    </ReferencePreview>
  );
}
