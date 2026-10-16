import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Expand, RotateCcw, Search, TriangleAlert, X, ZoomIn, ZoomOut } from "lucide-react";
import { errorMessage } from "@/bridge/errors";
import { pdfInfo, statFolderFile, type PdfInfo } from "@/bridge/workspaceCode";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { tabKey, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { describeParams, FILE_VIEW, MODULE } from "../documents";
import { FileToolbar } from "../file/FileToolbar";
import { useDocumentOwnership } from "../useDocument";
import { PdfPageView } from "./PdfPageView";
import { PdfSearch } from "./PdfSearch";
import {
  currentPage,
  dpiFor,
  fitWidthZoom,
  formatBytes,
  layoutPages,
  MIN_ZOOM,
  PAGE_MARGIN,
  scrollAfterZoom,
  scrollTopOfPage,
  stepZoom,
  visibleRange,
} from "./pdfLayout";

type Zoom = "fit" | number;
/** Where each open PDF tab was, so switching tabs or reopening a split does not lose the place. */
const memory = new Map<string, { scrollTop: number; zoom: Zoom }>();

type Loaded = { status: "loading" } | { status: "error"; message: string; gone: boolean } | { status: "ready"; info: PdfInfo };

/**
 * A PDF of a folder as a tab: a column of pages drawn by Go (internal/pdfdoc),
 * scrolled, zoomed and searched here. Nothing of the file runs in the webview —
 * it only ever receives pictures — and only the pages near the viewport are
 * asked for. The file is `(workspace, folder, path)` like any Files document; it
 * is read-only and has no buffer.
 */
export function PdfDocument({ params }: { params?: Record<string, unknown> }) {
  const doc = describeParams(FILE_VIEW, params);
  const path = doc.path ?? "";
  const tabId = tabKey(MODULE, FILE_VIEW, params);
  const { owner, relation, label } = useDocumentOwnership(doc);
  const workspaceId = owner?.workspaceId ?? "";
  const folderId = owner?.folderId ?? "";

  const [loaded, setLoaded] = useState<Loaded>({ status: "loading" });
  const generation = useRef(0);
  const load = useCallback(() => {
    if (!workspaceId || !folderId || !path) return;
    const mine = ++generation.current;
    pdfInfo(workspaceId, folderId, path).then(
      (info) => mine === generation.current && setLoaded({ status: "ready", info }),
      (e: unknown) => mine === generation.current && setLoaded({ status: "error", message: errorMessage(e), gone: false }),
    );
  }, [workspaceId, folderId, path]);
  // Keyed by the ids, never by `owner`: that object is new on every render, and an effect that
  // depends on it would read the file again for each state it sets.
  const known = owner !== null && relation !== "removed";
  useEffect(() => {
    if (known) load();
    return () => void generation.current++;
  }, [known, load]);

  // The file followed to its new version when the window comes back to it.
  const info = loaded.status === "ready" ? loaded.info : null;
  useEffect(() => {
    if (!info) return;
    const check = () => {
      statFolderFile(workspaceId, folderId, path).then(
        (s) => {
          if (!s.exists) setLoaded({ status: "error", message: "This file is no longer on disk.", gone: true });
          else if (s.size !== info.size || s.modifiedAt !== info.modifiedAt) load();
        },
        () => undefined,
      );
    };
    window.addEventListener("focus", check);
    return () => window.removeEventListener("focus", check);
  }, [info, workspaceId, folderId, path, load]);

  const closeDeadTab = () => useWorkbenchTabsStore.getState().closeTab(tabId, { remember: false });

  if (!owner) {
    return (
      <EmptyState icon={TriangleAlert} title="Which folder is this file in?" body="This tab was saved before documents named their folder. Open the file again from Files.">
        <Button variant="outline" className="bg-surface-raised" onClick={closeDeadTab}>Close this tab</Button>
      </EmptyState>
    );
  }
  if (relation === "removed") {
    return (
      <EmptyState icon={TriangleAlert} title="This file's folder left the workspace" body={path}>
        <Button variant="outline" className="bg-surface-raised" onClick={closeDeadTab}>Close this tab</Button>
      </EmptyState>
    );
  }
  const folderLabel = label || (relation === "other-workspace" ? "another workspace" : "");
  const toolbar = (right?: React.ReactNode) => (
    <FileToolbar workspaceId={workspaceId} folderId={folderId} folderLabel={folderLabel} path={path} right={right} />
  );

  if (loaded.status === "loading") {
    return (
      <div className="flex h-full w-full flex-col overflow-hidden">
        {toolbar()}
        <EmptyState busy title={`Opening ${path.slice(path.lastIndexOf("/") + 1)}`} body={path} />
      </div>
    );
  }
  if (loaded.status === "error") {
    return (
      <div className="flex h-full w-full flex-col overflow-hidden">
        {toolbar()}
        <EmptyState icon={TriangleAlert} tone="danger" title={loaded.gone ? "This PDF is gone" : "Could not open this PDF"} body={loaded.message}>
          {loaded.gone ? (
            <Button variant="outline" className="bg-surface-raised" onClick={closeDeadTab}>Close this tab</Button>
          ) : (
            <Button variant="outline" className="bg-surface-raised" onClick={load}>
              Try again
              <RotateCcw className="size-4" />
            </Button>
          )}
        </EmptyState>
      </div>
    );
  }
  return (
    <PdfPages
      key={`${tabId}:${loaded.info.revision}`}
      tabId={tabId}
      workspaceId={workspaceId}
      folderId={folderId}
      path={path}
      info={loaded.info}
      toolbar={toolbar}
      onChanged={load}
    />
  );
}

function PdfPages({
  tabId,
  workspaceId,
  folderId,
  path,
  info,
  toolbar,
  onChanged,
}: {
  tabId: string;
  workspaceId: string;
  folderId: string;
  path: string;
  info: PdfInfo;
  toolbar: (right?: React.ReactNode) => React.ReactNode;
  onChanged: () => void;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const saved = memory.get(tabId);
  const [zoomSetting, setZoomSetting] = useState<Zoom>(saved?.zoom ?? "fit");
  const [viewport, setViewport] = useState({ width: 0, height: 0 });
  const [scrollTop, setScrollTop] = useState(saved?.scrollTop ?? 0);
  const [searching, setSearching] = useState(false);

  // The scroller's size, for fit-to-width and for which pages are near.
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const measure = () => setViewport({ width: el.clientWidth, height: el.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const zoom = zoomSetting === "fit" ? fitWidthZoom(info.pageSizes, Math.max(1, viewport.width - PAGE_MARGIN * 2)) : zoomSetting;
  const layout = useMemo(() => layoutPages(info.pageSizes, zoom), [info.pageSizes, zoom]);
  const dpi = dpiFor(zoom, window.devicePixelRatio);

  // Back where the tab was left, once the column has its height.
  const restored = useRef(false);
  useEffect(() => {
    const el = scroller.current;
    if (!el || restored.current || viewport.height === 0) return;
    restored.current = true;
    if (saved) el.scrollTop = saved.scrollTop;
  }, [viewport.height, saved]);

  const zoomRef = useRef<Zoom>(zoomSetting);
  zoomRef.current = zoomSetting;
  const frame = useRef(0);
  const onScroll = useCallback(() => {
    cancelAnimationFrame(frame.current);
    frame.current = requestAnimationFrame(() => {
      const top = scroller.current?.scrollTop ?? 0;
      setScrollTop(top);
      memory.set(tabId, { scrollTop: top, zoom: zoomRef.current });
    });
  }, [tabId]);
  useEffect(() => () => cancelAnimationFrame(frame.current), []);

  const range = visibleRange(layout.boxes, scrollTop, viewport.height);
  const page = currentPage(layout.boxes, scrollTop, viewport.height);
  const total = layout.boxes.length;

  const goTo = useCallback(
    (n: number) => {
      const el = scroller.current;
      if (el) el.scrollTop = scrollTopOfPage(layout.boxes, Math.min(Math.max(1, n), total));
    },
    [layout.boxes, total],
  );

  const setZoom = (next: Zoom) => {
    const el = scroller.current;
    const from = zoom;
    const to = next === "fit" ? fitWidthZoom(info.pageSizes, Math.max(1, viewport.width - PAGE_MARGIN * 2)) : next;
    setZoomSetting(next);
    memory.set(tabId, { scrollTop: el?.scrollTop ?? 0, zoom: next });
    if (el) requestAnimationFrame(() => (el.scrollTop = scrollAfterZoom(el.scrollTop, from, to)));
  };

  const right = (
    <>
      <PageField page={page} total={total} onGo={goTo} />
      <span className="mx-0.5 h-4 w-px bg-border-subtle" aria-hidden />
      <Button variant="ghost" size="icon-sm" aria-label="Zoom out" title="Zoom out" disabled={zoom <= MIN_ZOOM} onClick={() => setZoom(stepZoom(zoom, -1))}>
        <ZoomOut />
      </Button>
      <span className="min-w-10 text-center text-code-11 text-text-secondary tabular-nums" aria-live="polite">
        {Math.round(zoom * 100)}%
      </span>
      <Button variant="ghost" size="icon-sm" aria-label="Zoom in" title="Zoom in" onClick={() => setZoom(stepZoom(zoom, 1))}>
        <ZoomIn />
      </Button>
      <Button variant={zoomSetting === "fit" ? "secondary" : "ghost"} size="icon-sm" aria-label="Fit to width" aria-pressed={zoomSetting === "fit"} title="Fit to width" onClick={() => setZoom("fit")}>
        <Expand />
      </Button>
      <span className="mx-0.5 h-4 w-px bg-border-subtle" aria-hidden />
      <Button variant={searching ? "secondary" : "ghost"} size="icon-sm" aria-label="Search in this PDF" aria-pressed={searching} title="Search" onClick={() => setSearching((s) => !s)}>
        {searching ? <X /> : <Search />}
      </Button>
      <span className="ml-1 hidden text-code-11 text-text-tertiary @2xl:inline">{formatBytes(info.size)}</span>
    </>
  );

  return (
    <div
      className="@container flex h-full w-full flex-col overflow-hidden outline-none"
      tabIndex={-1}
      onKeyDown={(e) => {
        if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "f") {
          e.preventDefault();
          setSearching(true);
        }
      }}
    >
      {toolbar(right)}
      {searching ? (
        <PdfSearch workspaceId={workspaceId} folderId={folderId} path={path} revision={info.revision} onGo={goTo} onClose={() => setSearching(false)} onChanged={onChanged} />
      ) : null}
      <div ref={scroller} data-scroll-box onScroll={onScroll} className="min-h-0 flex-1 overflow-auto bg-surface-well" role="document" aria-label={`PDF, ${total} pages`}>
        <div className="relative mx-auto" style={{ width: layout.width + PAGE_MARGIN * 2, height: layout.height }}>
          <div className="absolute inset-y-0" style={{ left: PAGE_MARGIN, width: layout.width }}>
            {range
              ? layout.boxes.slice(range.first - 1, range.last).map((box) => (
                  <PdfPageView key={box.page} workspaceId={workspaceId} folderId={folderId} path={path} revision={info.revision} box={box} dpi={dpi} onChanged={onChanged} />
                ))
              : null}
          </div>
        </div>
      </div>
    </div>
  );
}

/** "Page [3] of 12": type a number and press Enter to go there. */
function PageField({ page, total, onGo }: { page: number; total: number; onGo: (n: number) => void }) {
  const [draft, setDraft] = useState<string | null>(null);
  const commit = () => {
    const n = Number.parseInt(draft ?? "", 10);
    if (Number.isFinite(n)) onGo(n);
    setDraft(null);
  };
  return (
    <label className="flex items-center gap-1.5 text-code-11 text-text-secondary">
      <span className="sr-only">Page</span>
      <input
        inputMode="numeric"
        value={draft ?? String(page)}
        aria-label="Page number"
        className="h-5 w-9 rounded-sm border border-border-subtle bg-transparent text-center text-code-11 text-text-primary tabular-nums outline-none focus-visible:ring-1 focus-visible:ring-status-info"
        onFocus={(e) => e.currentTarget.select()}
        onChange={(e) => setDraft(e.target.value.replace(/\D/g, ""))}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") e.currentTarget.blur();
          if (e.key === "Escape") setDraft(null);
        }}
      />
      <span aria-hidden>of {total}</span>
    </label>
  );
}
