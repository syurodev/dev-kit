import { memo, useEffect, useState } from "react";
import { toAppError, isCancelError } from "@/bridge/errors";
import { pdfPage, type PdfPageImage } from "@/bridge/workspaceCode";
import type { PageBox } from "./pdfLayout";
import { cachedPage, pageKey, rememberPage } from "./pdfPageCache";

/**
 * One page of the column: the picture Go drew, in the box the layout reserved
 * for it. It asks only while mounted — the document unmounts pages that scroll
 * far away — and gives up a request it no longer needs.
 */
export const PdfPageView = memo(function PdfPageView({
  workspaceId,
  folderId,
  path,
  revision,
  box,
  dpi,
  onChanged,
}: {
  workspaceId: string;
  folderId: string;
  path: string;
  revision: string;
  box: PageBox;
  dpi: number;
  /** The file was replaced on disk after this document was opened. */
  onChanged: () => void;
}) {
  const key = pageKey(folderId, path, revision, box.page, dpi);
  const [image, setImage] = useState<PdfPageImage | undefined>(() => cachedPage(key));
  const [error, setError] = useState("");

  useEffect(() => {
    setError("");
    const hit = cachedPage(key);
    if (hit) {
      setImage(hit);
      return;
    }
    const call = pdfPage(workspaceId, folderId, path, revision, box.page, dpi);
    let live = true;
    call.then(
      (drawn) => {
        if (!live) return;
        rememberPage(key, drawn);
        setImage(drawn);
      },
      (e: unknown) => {
        if (!live || isCancelError(e)) return;
        const app = toAppError(e);
        if (app.code === "pdf_changed") onChanged();
        else setError(app.message);
      },
    );
    return () => {
      live = false;
      call.cancel();
    };
    // `key` carries workspace-independent identity; the ids it omits cannot change under one mounted view.
  }, [key, workspaceId, folderId, path, revision, box.page, dpi, onChanged]);

  return (
    <div
      className="absolute overflow-hidden border border-border-subtle bg-surface-raised"
      style={{ top: box.top, left: box.left, width: box.width, height: box.height }}
      data-page={box.page}
    >
      {image ? (
        // The picture is already on white, whatever the theme.
        <img src={image.src} alt={`Page ${box.page}`} width={box.width} height={box.height} draggable={false} className="block size-full select-none" />
      ) : error ? (
        <p role="alert" className="p-4 text-body-12 text-status-danger">
          Page {box.page} could not be drawn. {error}
        </p>
      ) : (
        <div className="size-full animate-pulse bg-state-soft motion-reduce:animate-none" aria-busy="true" aria-label={`Loading page ${box.page}`} />
      )}
    </div>
  );
});
