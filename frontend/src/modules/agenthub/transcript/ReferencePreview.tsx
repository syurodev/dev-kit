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
import { HIGHLIGHT_LIMIT } from "@/components/code/highlight";
import { languageForPath } from "@/components/code/languages";
import { dispatchIntent, type Placement } from "@/workbench/intents";
import type { FileRef } from "./fileRef";
import { fileRefQuery, useFileRefResolution, type RefContext, type RefResolution, type RefTarget } from "./resolveFileRef";
import { openRefTarget } from "./openReference";
import { RefPickerList } from "./RefPicker";
import { copiedPath, fileMenuItems, urlMenuItems, type FileMenuId, type MenuItem, type UrlMenuId } from "./referenceMenu";
import { useTranscriptActions } from "./transcriptActions";

/** Lines shown either side of the target line. */
const CONTEXT_LINES = 4;
/** Rows in a preview's body, in every state: the slice's full height. */
const PREVIEW_ROWS = CONTEXT_LINES * 2 + 1;
/** Past the highlight limit, the text around the target that is coloured: enough that a comment or string opened above is still read as one. */
const HIGHLIGHT_WINDOW = 200;

const LINE_BREAK = /\r?\n/;

/** A file's lines; a trailing newline is not a line. */
function fileLines(content: string): string[] {
  const all = content.split(LINE_BREAK);
  if (all.length > 1 && all[all.length - 1] === "") all.pop();
  return all;
}

/**
 * The lines a preview shows: four either side of `line`, clamped to the file,
 * or the first nine when there is no line.
 */
export function previewSlice(content: string, line: number | undefined): { startLine: number; lines: string[] } {
  const all = fileLines(content);
  if (line === undefined) return { startLine: 1, lines: all.slice(0, PREVIEW_ROWS) };
  const at = Math.min(Math.max(line, 1), all.length);
  const start = Math.max(1, at - CONTEXT_LINES);
  const end = Math.min(all.length, at + CONTEXT_LINES);
  return { startLine: start, lines: all.slice(start - 1, end) };
}

/**
 * What a preview highlights and which lines of it it shows. A file the
 * highlighter takes is highlighted whole, so a comment or string opened above
 * the slice still colours it; a larger one is highlighted in a window of two
 * hundred lines either side of the target. Lines are 1-based and inclusive;
 * `target` is the line raised, clamped to the file.
 */
export type PreviewWindow = { code: string; codeStart: number; startLine: number; endLine: number; target?: number; total: number };

export function previewWindow(content: string, line: number | undefined): PreviewWindow {
  const all = fileLines(content);
  const total = all.length;
  const target = line === undefined ? undefined : Math.min(Math.max(line, 1), total);
  const startLine = target === undefined ? 1 : Math.max(1, target - CONTEXT_LINES);
  const endLine = target === undefined ? Math.min(total, PREVIEW_ROWS) : Math.min(total, target + CONTEXT_LINES);
  if (content.length <= HIGHLIGHT_LIMIT) return { code: all.join("\n"), codeStart: 1, startLine, endLine, target, total };
  const around = target ?? 1;
  const codeStart = Math.max(1, around - HIGHLIGHT_WINDOW);
  const codeEnd = Math.min(total, around + HIGHLIGHT_WINDOW);
  return { code: all.slice(codeStart - 1, codeEnd).join("\n"), codeStart, startLine, endLine, target, total };
}

/** The code split into lines, each with its colours; a syntax run that spans lines is cut at each newline. */
export function highlightedLines(code: string, highlight: Pick<Highlight, "code" | "segments"> | null): React.ReactNode[] {
  if (!highlight || highlight.code !== code) return code.split(LINE_BREAK);
  const lines: React.ReactNode[][] = [[]];
  let key = 0;
  for (const segment of highlight.segments) {
    segment.text.split(LINE_BREAK).forEach((part, index) => {
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

/** The rows a preview shows, cut from the highlighted window. */
export function previewRows(window: PreviewWindow, highlight: Pick<Highlight, "code" | "segments"> | null): React.ReactNode[] {
  return highlightedLines(window.code, highlight).slice(window.startLine - window.codeStart, window.endLine - window.codeStart + 1);
}

/** Whether a hover card may open now: a pointer resting on it, not just pressed, and no context menu over it. */
export function hoverMayOpen(state: { pointerInside: boolean; pressed: boolean; menuOpen: boolean }): boolean {
  return state.pointerInside && !state.pressed && !state.menuOpen;
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
 *
 * Only a pointer opens the card. Focus alone does not: a keyboard user opens
 * the reference with Enter (`onActivate`) and reaches the rest through the
 * context menu, which the trigger announces. A click closes the card, which
 * stays shut until the pointer leaves and comes back.
 */
export function ReferencePreview({
  preview,
  menu,
  onActivate,
  activateKeys,
  children,
}: {
  preview: (controls: Controls) => React.ReactNode;
  menu: ((controls: Controls) => React.ReactNode) | null;
  /** Opens the reference on click, and on `activateKeys`; attached here so the child needs no handler of its own. */
  onActivate?: (event: React.SyntheticEvent<HTMLElement>) => void;
  activateKeys?: readonly string[];
  children: React.ReactElement;
}) {
  const [open, setOpen] = React.useState(false);
  const triggerRef = React.useRef<HTMLElement | null>(null);
  const pointer = React.useRef({ pointerInside: false, pressed: false, menuOpen: false });
  const controls: Controls = { close: () => setOpen(false), anchor: () => triggerRef.current };
  return (
    <HoverCard
      open={open}
      onOpenChange={(next) => {
        if (next && !hoverMayOpen(pointer.current)) return;
        setOpen(next);
      }}
      openDelay={400}
      closeDelay={120}
    >
      <ContextMenu
        onOpenChange={(menuOpen) => {
          pointer.current.menuOpen = menuOpen;
          if (menuOpen) setOpen(false);
        }}
      >
        <HoverCardTrigger
          asChild
          ref={(node: HTMLElement | null) => {
            triggerRef.current = node;
          }}
          aria-haspopup="menu"
          onPointerEnter={() => {
            pointer.current.pointerInside = true;
          }}
          onPointerLeave={() => {
            pointer.current.pointerInside = false;
            pointer.current.pressed = false;
          }}
          onPointerDown={() => {
            pointer.current.pressed = true;
          }}
          onClick={(event: React.MouseEvent<HTMLElement>) => {
            setOpen(false);
            onActivate?.(event);
          }}
          onKeyDown={(event: React.KeyboardEvent<HTMLElement>) => {
            if (!onActivate || !activateKeys?.includes(event.key)) return;
            event.preventDefault();
            onActivate(event);
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
function PreviewHeader({
  path,
  line,
  total,
  folderName,
  kind,
}: {
  path: string;
  line?: number;
  /** The file's length, when known: a line past it is said so rather than shown as found. */
  total?: number;
  folderName?: string;
  kind?: string;
}) {
  const { name, dir } = splitRel(path);
  const pastEnd = line !== undefined && total !== undefined && line > total;
  return (
    <div className="flex min-w-0 items-baseline gap-2 border-b border-border-subtle p-2.5">
      <span className="flex min-w-0 flex-1 items-baseline text-code-12" title={path}>
        {dir ? (
          // rtl puts the ellipsis on the left, where the least telling part of a path is.
          <span dir="rtl" className="min-w-0 truncate text-left text-text-tertiary">
            <bdi dir="ltr">{dir}</bdi>
          </span>
        ) : null}
        <span className="min-w-0 truncate font-medium text-text-primary">{name}</span>
        {line !== undefined && !pastEnd ? <span className="shrink-0 text-text-tertiary">:{line}</span> : null}
      </span>
      {pastEnd ? <span className="shrink-0 text-caption-11 text-text-tertiary">{`line ${line} of ${total}`}</span> : null}
      {kind || folderName ? (
        <span className="shrink-0 text-caption-11 text-text-tertiary">{[kind, folderName].filter(Boolean).join(" · ")}</span>
      ) : null}
    </div>
  );
}

function PreviewLines({ window, language }: { window: PreviewWindow; language: string }) {
  const highlight = useHighlight(window.code, language);
  // Split once per highlight, not per render: the window can be a whole file.
  const rows = React.useMemo(() => previewRows(window, highlight), [window, highlight]);
  const numbers = rows.map((_, index) => window.startLine + index);
  // Two columns rather than a table: every cell is one line high (h-lh, so an
  // empty line keeps its height), and the target row is raised in both.
  return (
    <div className="flex py-2.5">
      <div aria-hidden="true" className="flex shrink-0 flex-col text-right text-text-tertiary tabular-nums select-none">
        {numbers.map((number) => (
          <span key={number} className={cn("h-lh pr-3 pl-2.5", number === window.target && "bg-state-soft")}>
            {number}
          </span>
        ))}
      </div>
      <div className="flex min-w-0 flex-1 flex-col text-text-primary tab-size-2">
        {rows.map((node, index) => {
          const isTarget = numbers[index] === window.target;
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

const SKELETON_WIDTHS = ["w-3/4", "w-1/2", "w-2/3", "w-5/6", "w-2/5", "w-3/5", "w-3/4", "w-1/2", "w-2/3"];

/**
 * The body's box: nine invisible rows give it the slice's height in every
 * state, so the card does not change size as a file loads, and whatever fills
 * it scrolls inside.
 */
function PreviewBody({ children }: { children: React.ReactNode }) {
  return (
    <div data-testid="ref-preview-body" className="relative text-code-12">
      <div data-testid="ref-preview-sizer" aria-hidden="true" className="invisible flex flex-col py-2.5">
        {SKELETON_WIDTHS.map((_, index) => (
          <span key={index} className="h-lh" />
        ))}
      </div>
      <div className="absolute inset-0 overflow-y-auto">{children}</div>
    </div>
  );
}

function PreviewNote({ children }: { children: React.ReactNode }) {
  return <p className="p-2.5 text-body-13 text-text-secondary">{children}</p>;
}

export function PreviewFooter({ children }: { children: React.ReactNode }) {
  return (
    <div data-testid="ref-preview-footer" className="flex justify-end gap-1.5 border-t border-border-subtle p-2.5">
      {children}
    </div>
  );
}

/** The file preview for a given state; the hooks live in FileRefPreview. */
export function FilePreviewView({
  fileRef,
  state,
  onOpen,
  onPick,
  kind,
}: {
  fileRef: FileRef;
  state: FilePreviewState;
  /** What the reference names, for a symbol: class, method. */
  kind?: string;
  /** No placement: the surface's own (beside in the Hub, in place in the rail). */
  onOpen: (placement?: Placement) => void;
  onPick: (target: RefTarget) => void;
}) {
  const content = state.kind === "text" ? state.content : null;
  const line = fileRef.line;
  const window = React.useMemo(() => (content === null ? null : previewWindow(content, line)), [content, line]);
  const target = "target" in state ? state.target : undefined;
  // The path as written until the resolved one arrives.
  const header = target ? (
    <PreviewHeader path={target.rel} line={line} total={window?.total} folderName={target.folderName} kind={kind} />
  ) : (
    <PreviewHeader path={fileRef.path} line={line} />
  );
  const canOpen = state.kind === "loading" || state.kind === "text" || state.kind === "unreadable";

  let body: React.ReactNode;
  switch (state.kind) {
    case "loading":
      body = (
        <div className="flex flex-col py-2.5 pr-2.5 pl-2.5">
          {SKELETON_WIDTHS.map((width, index) => (
            <span key={index} className="flex h-lh items-center">
              <Skeleton className={cn("h-3", width)} />
            </span>
          ))}
        </div>
      );
      break;
    case "text":
      body = window ? <PreviewLines window={window} language={languageForPath(state.target.rel) ?? "text"} /> : null;
      break;
    case "unreadable":
      body = <PreviewNote>{UNREADABLE[state.reason]}</PreviewNote>;
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
      body = <PreviewNote>No workspace to resolve this in.</PreviewNote>;
      break;
    case "error":
      body = <PreviewNote>{state.message}</PreviewNote>;
      break;
  }
  return (
    <div data-testid="ref-preview-file" className="flex flex-col">
      {header}
      <PreviewBody>{body}</PreviewBody>
      <PreviewFooter>
        {state.kind === "unsupported" ? (
          <Button size="xs" variant="secondary" onClick={() => onOpen()}>
            Open in Code Graph
          </Button>
        ) : (
          <>
            <Button size="xs" variant="ghost" disabled={!canOpen} onClick={() => onOpen("replace")}>
              Open here
            </Button>
            <Button size="xs" variant="secondary" disabled={!canOpen} onClick={() => onOpen()}>
              Open
            </Button>
          </>
        )}
      </PreviewFooter>
    </div>
  );
}

/** Reads a resolved file for its preview; nothing until there is a target. */
export function useRefTargetRead(found: RefTarget | null) {
  return useQuery({
    queryKey: ["agenthub", "fileRefRead", found?.workspaceId ?? "", found?.folderId ?? "", found?.rel ?? ""] as const,
    queryFn: () => readFolderFile(found!.workspaceId, found!.folderId, found!.rel),
    enabled: found !== null && !found.unreadable,
    // A file the agent is editing should not show old lines for long, nor stay in memory once the card is gone.
    staleTime: 5_000,
    gcTime: 30_000,
  });
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
  onOpen: (placement?: Placement) => void;
  onPick: (target: RefTarget) => void;
}) {
  const resolution = useFileRefResolution(fileRef, refContext, true);
  const found = resolution.data?.kind === "found" ? resolution.data.target : null;
  const read = useRefTargetRead(found);
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

export async function copyWithNotice(text: string, notice: string) {
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

type MenuProps<Id extends string> = {
  /** A function is called only when the menu opens: a streaming message never builds the menus of its references. */
  items: MenuItem<Id>[] | (() => MenuItem<Id>[]);
  icons: Record<Id, React.ReactNode>;
  onSelect: (id: Id) => void;
};

function MenuRows<Id extends string>({ items, icons, onSelect }: MenuProps<Id>) {
  const rows = typeof items === "function" ? items() : items;
  return rows.map((item) => (
    <ContextMenuItem key={item.id} icon={icons[item.id]} onSelect={() => onSelect(item.id)}>
      {item.label}
    </ContextMenuItem>
  ));
}

export function ReferenceMenu<Id extends string>(props: MenuProps<Id>) {
  // The rows are a component of their own, mounted by the content only while the menu is open.
  return (
    <ContextMenuContent>
      <MenuRows {...props} />
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
async function openPicked(target: RefTarget, line: number | undefined, placement: Placement, dispatch: typeof dispatchIntent) {
  try {
    if ((await openRefTarget(target, line, placement, dispatch)) === "unavailable") toast.error(`Couldn't find ${target.rel} in this workspace`);
  } catch (error) {
    toast.error(`Couldn't open ${target.rel}: ${errorMessage(error)}`);
  }
}

/**
 * How a file reference opens. With no placement it opens where the surface
 * opens things (beside in the Hub, in place in the rail); a candidate picked
 * in the card does the same.
 */
export function useFileRefOpener(fileRef: FileRef, dispatch: typeof dispatchIntent = dispatchIntent) {
  const { openRef, onOpenFile, refPlacement } = useTranscriptActions();
  return {
    open: (anchor: HTMLElement | null, placement?: Placement) => {
      if (openRef) openRef(fileRef, anchor, placement);
      else onOpenFile?.(fileRef.path, fileRef.line);
    },
    pick: (target: RefTarget) => openPicked(target, fileRef.line, refPlacement ?? "beside", dispatch),
  };
}

const CODE_KEYS = ["Enter", " "] as const;
const LINK_KEYS = ["Enter"] as const;

type FileReferenceProps = {
  path: string;
  line?: number;
  /** What the child shows, as a string. The memo compares it instead of the element, which is new on every render. */
  content: string;
  /** A link opens on Enter; inline code, like a button, on Enter and Space. */
  link?: boolean;
  children: React.ReactElement;
};

/**
 * A file reference in prose (inline code or a link), with its preview and
 * menu. It opens the file itself, so the child carries no handler that could
 * go stale while the memo keeps an earlier element.
 */
export const FileReference = React.memo(
  function FileReference({ path, line, link, children }: FileReferenceProps) {
    const fileRef = React.useMemo<FileRef>(() => (line === undefined ? { path } : { path, line }), [path, line]);
    const { refContext } = useTranscriptActions();
    const ctx = refContext ?? null;
    const opener = useFileRefOpener(fileRef);
    return (
      <ReferencePreview
        onActivate={(event) => opener.open(event.currentTarget)}
        activateKeys={link ? LINK_KEYS : CODE_KEYS}
        preview={({ close, anchor }) => (
          <FileRefPreview
            fileRef={fileRef}
            refContext={ctx}
            onOpen={(placement) => {
              close();
              opener.open(anchor(), placement);
            }}
            onPick={(target) => {
              close();
              void opener.pick(target);
            }}
          />
        )}
        menu={({ anchor }) => (
          <ReferenceMenu
            items={() => fileMenuItems(fileRef)}
            icons={FILE_ICONS}
            onSelect={(id) => {
              switch (id) {
                case "open-beside":
                  opener.open(anchor(), "beside");
                  return;
                // Opening here by ids also switches Code Graph's sidebar to Files.
                case "open-here":
                case "reveal":
                  opener.open(anchor(), "replace");
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
  },
  (a, b) => a.path === b.path && a.line === b.line && a.content === b.content && a.link === b.link,
);

type UrlReferenceProps = {
  url: string;
  /** What the child shows, as a string; compared by the memo instead of the element. */
  content: string;
  children: React.ReactElement;
};

/** A web link in prose, with its preview and menu. The child's own handler needs only the URL. */
export const UrlReference = React.memo(
  function UrlReference({ url, children }: UrlReferenceProps) {
    return (
      <ReferencePreview
        preview={({ close }) => <UrlPreview url={url} onDone={close} />}
        menu={() => (
          <ReferenceMenu
            items={() => urlMenuItems(url)}
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
  },
  (a, b) => a.url === b.url && a.content === b.content,
);
