import { FileIcon } from "@/components/file-icon/FileIcon";
import { memo, useMemo, useState } from "react";
import { ChevronRight, Copy, Ellipsis, FolderSearch, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { errorMessage } from "@/bridge/errors";
import { copyText } from "@/bridge/clipboard";
import { revealFolderFile } from "@/bridge/workspaceCode";
import { Button } from "@/components/ui/button";
import { detectPlatform } from "@/lib/platform";
import { fullPath, revealLabel } from "./fileMenu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { shortLabel } from "../graph/labels";
import { useCodeGraphStore } from "../codegraphStore";
import { openFile } from "../fileActions";
import { NavButtons } from "./NavButtons";
import { useCursor } from "../cursor";
import { symbolPathAt, outlineRows } from "../workspace/outline";
import { baseName, dirName } from "../workspace/identity";
import type { FileBuffer } from "../workspace/bufferModel";
import { docKey } from "../workspace/identity";

/** At most this many entries in a sibling or symbol menu before it asks for narrowing. */
const MENU_LIMIT = 80;
/** More path segments than this collapse their middle into "…". */
const MAX_SEGMENTS = 4;

type Segment = { label: string; dir: string; kind: "folder" | "dir" | "file" };

/** Folder → directories → file, with a long middle folded away. */
export function pathSegments(folderLabel: string, path: string): Segment[] {
  const parts = path.split("/").filter(Boolean);
  const all: Segment[] = [{ label: folderLabel, dir: "", kind: "folder" }];
  parts.forEach((part, i) => {
    all.push({ label: part, dir: parts.slice(0, i).join("/"), kind: i === parts.length - 1 ? "file" : "dir" });
  });
  if (all.length <= MAX_SEGMENTS) return all;
  // Keep the folder and the last two; the middle is "…" (titled with what it hides).
  const hidden = all.slice(1, all.length - 2);
  return [all[0], { label: "…", dir: hidden.map((s) => s.label).join("/"), kind: "dir" }, ...all.slice(-2)];
}

/**
 * Back / Forward and the path of the file in front: folder → directories → file
 * → the symbol the caret is in.
 *
 * Nothing in it names the primary folder; the folder is the document's own. Each
 * directory and the file offers the files nearby, and the symbol offers the
 * file's outline — so moving around a folder or a file does not need the
 * sidebar.
 */
export const FileToolbar = memo(function FileToolbar({
  workspaceId,
  folderId,
  folderLabel,
  path,
  buffer,
  right,
}: {
  workspaceId: string;
  folderId: string;
  folderLabel: string;
  path: string;
  /** Absent for a document with no text buffer (a PDF). */
  buffer?: FileBuffer;
  right?: React.ReactNode;
}) {
  const key = docKey(workspaceId, folderId, path);
  const segments = useMemo(() => pathSegments(folderLabel, path), [folderLabel, path]);
  const hidden = segments.some((s) => s.label === "…");

  return (
    <div className="flex h-8 shrink-0 items-center justify-between gap-3 border-b border-border-subtle px-2">
      <div className="flex min-w-0 flex-1 items-center gap-0.5">
        <NavButtons />
        <nav aria-label="File path" className="flex min-w-0 flex-1 items-center overflow-hidden pl-1 text-code-11 text-text-secondary" title={`${folderLabel} › ${path}`}>
          {segments.map((seg, i) => (
            <span key={`${i}:${seg.label}`} className={cn("flex items-center", seg.kind === "file" ? "min-w-0 shrink" : "min-w-0 shrink-[2]")}>
              {i > 0 ? <ChevronRight className="mx-0.5 size-3 shrink-0 text-text-tertiary" aria-hidden /> : null}
              {seg.kind === "file" ? <FileIcon path={path} className="mr-1 size-3.5" /> : null}
              {seg.label === "…" ? (
                <span title={hidden ? seg.dir : undefined} className="px-0.5 text-text-tertiary">…</span>
              ) : (
                <SiblingMenu
                  workspaceId={workspaceId}
                  folderId={folderId}
                  label={seg.label}
                  dir={seg.kind === "file" ? dirName(path) : seg.dir ? `${seg.dir}/${seg.label}` : ""}
                  current={path}
                  strong={seg.kind === "file"}
                />
              )}
            </span>
          ))}
          <SymbolCrumb workspaceId={workspaceId} folderId={folderId} path={path} bufferKey={key} dirty={buffer?.dirty ?? false} />
        </nav>
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        {right}
        <FileActionsMenu workspaceId={workspaceId} folderId={folderId} path={path} />
      </div>
    </div>
  );
});

/**
 * What anyone wants to do with a file they are looking at besides reading it: take its
 * path somewhere, or find it on disk. Reveal is named for the desktop's own file manager
 * and left out where there is none (a browser). The full path needs the folder to be one
 * of the open workspace's; the relative path never does.
 */
function FileActionsMenu({ workspaceId, folderId, path }: { workspaceId: string; folderId: string; path: string }) {
  const folderPath = useCodeGraphStore((s) => s.folders.find((f) => f.id === folderId)?.path);
  const reveal = revealLabel(detectPlatform());
  const copy = (text: string, what: string) =>
    copyText(text).then(
      () => toast(`${what} copied`),
      (e: unknown) => toast.error(errorMessage(e)),
    );
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="File actions" title="File actions">
          <Ellipsis />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        {folderPath ? (
          <DropdownMenuItem icon={<Copy />} onSelect={() => void copy(fullPath(folderPath, path), "Path")}>
            Copy path
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuItem icon={<Copy />} onSelect={() => void copy(path, "Relative path")}>
          Copy relative path
        </DropdownMenuItem>
        {reveal ? (
          <DropdownMenuItem
            icon={<FolderSearch />}
            onSelect={() => void revealFolderFile(workspaceId, folderId, path).catch((e: unknown) => toast.error(errorMessage(e)))}
          >
            {reveal}
          </DropdownMenuItem>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** A path segment that opens the files beside it. */
function SiblingMenu({
  workspaceId,
  folderId,
  label,
  dir,
  current,
  strong,
}: {
  workspaceId: string;
  folderId: string;
  label: string;
  /** The directory whose files the menu lists. */
  dir: string;
  current: string;
  strong: boolean;
}) {
  const [open, setOpen] = useState(false);
  const files = useCodeGraphStore((s) => (open ? s.data[folderId]?.files : undefined));
  const entries = useMemo(() => {
    if (!files) return [];
    const prefix = dir ? `${dir}/` : "";
    const out: string[] = [];
    for (const e of files) {
      if (e.isDir || !e.path.startsWith(prefix)) continue;
      if (e.path.indexOf("/", prefix.length) !== -1) continue;
      out.push(e.path);
    }
    return out;
  }, [files, dir]);
  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className={cn(
            "max-w-48 min-w-0 truncate rounded-sm px-1 py-0.5 hover:bg-state-soft hover:text-text-primary focus-visible:ring-1 focus-visible:ring-status-info focus-visible:outline-none",
            strong && "text-text-primary",
          )}
          aria-label={`Files in ${dir || label}`}
        >
          {label}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-80 w-64 overflow-y-auto">
        <DropdownMenuLabel className="truncate">{dir || label}</DropdownMenuLabel>
        {entries.length === 0 ? <DropdownMenuItem disabled>No files here</DropdownMenuItem> : null}
        {entries.slice(0, MENU_LIMIT).map((p) => (
          <DropdownMenuItem icon={<FileIcon path={p} className="size-3.5" />} key={p} disabled={p === current} onSelect={() => void openFile({ workspaceId, folderId, path: p }, null, { preview: false })}>
            <span className="truncate">{baseName(p)}</span>
          </DropdownMenuItem>
        ))}
        {entries.length > MENU_LIMIT ? <DropdownMenuItem disabled>{entries.length - MENU_LIMIT} more — use the Files tab</DropdownMenuItem> : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * The innermost symbol the caret is in, from the index — so for an unsaved
 * buffer it is "as of the last save" and says so on hover. Reads the outline
 * and the caret's saved-file line; does not rescan anything on a keystroke.
 */
function SymbolCrumb({
  workspaceId,
  folderId,
  path,
  bufferKey,
  dirty,
}: {
  workspaceId: string;
  folderId: string;
  path: string;
  bufferKey: string;
  dirty: boolean;
}) {
  const outline = useCodeGraphStore((s) => s.outlines[bufferKey]);
  const line = useCursor(bufferKey);
  const chain = useMemo(() => (outline && line > 0 ? symbolPathAt(outline.symbols, line) : []), [outline, line]);
  const last = chain[chain.length - 1];
  const rows = useMemo(() => (outline ? outlineRows(outline.symbols, "", (s) => shortLabel(s.label, s.kind)) : []), [outline]);
  if (!outline || outline.symbols.length === 0) return null;
  return (
    <span className="flex min-w-0 shrink items-center">
      <ChevronRight className="mx-0.5 size-3 shrink-0 text-text-tertiary" aria-hidden />
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className="min-w-0 max-w-56 truncate rounded-sm px-1 py-0.5 text-text-primary hover:bg-state-soft focus-visible:ring-1 focus-visible:ring-status-info focus-visible:outline-none"
            aria-label="Symbols in this file"
            title={dirty ? "From the last indexed version; unsaved edits are not reflected." : undefined}
          >
            {last ? shortLabel(last.label, last.kind) : "Symbols"}
            {dirty && last ? <span aria-hidden className="text-text-tertiary"> ~</span> : null}
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="max-h-80 w-72 overflow-y-auto">
          <DropdownMenuLabel>Symbols{dirty ? " · last indexed version" : ""}</DropdownMenuLabel>
          {rows.slice(0, MENU_LIMIT).map(({ symbol, depth }) => (
            <DropdownMenuItem
              key={symbol.id}
              onSelect={() => void openFile({ workspaceId, folderId, path }, symbol.line, { preview: false })}
              style={{ paddingLeft: 8 + depth * 12 }}
              className={cn(chain.includes(symbol) && "bg-state-soft")}
            >
              <span className="truncate">{shortLabel(symbol.label, symbol.kind)}</span>
              <span className="ml-auto pl-2 text-code-11 text-text-tertiary">{symbol.line}</span>
            </DropdownMenuItem>
          ))}
          {rows.length > MENU_LIMIT ? <DropdownMenuItem disabled>{rows.length - MENU_LIMIT} more symbols</DropdownMenuItem> : null}
        </DropdownMenuContent>
      </DropdownMenu>
    </span>
  );
}

/** A spinner for a save in flight. */
export function SavingMark() {
  return <Loader2 className="size-3.5 animate-spin text-text-tertiary motion-reduce:animate-none" aria-label="Saving" />;
}
