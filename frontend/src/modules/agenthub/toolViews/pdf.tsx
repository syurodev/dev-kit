import { useId, useState } from "react";
import { ChevronRight, FileSearch, FileText, Files, Images, Info } from "lucide-react";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import {
  FramedCard,
  FramedCardHeader,
  FramedCardMore,
  FramedCardPreview,
  FramedCardRow,
} from "@/components/ui/framed-card";
import { cn } from "@/lib/utils";
import {
  formatBytes,
  markMatches,
  type PdfDocument,
  type PdfInfo,
  type PdfPage,
  type PdfPages,
  type PdfSearch,
  type PdfText,
} from "./pdfResult";

/**
 * The `pdf.*` tools' result cards (docs/dev-kit/tool-result-view.mdx). PDF
 * documents belong to the Agent Hub session that attached them, so Agent Hub
 * registers these (../manifest.ts). They draw only what the call carries: a
 * page picture is the result's own image block, never fetched again.
 */

const MAX_ROWS = 10;

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

function Notes({ notes }: { notes: readonly string[] }) {
  if (notes.length === 0) return null;
  return (
    <div className="flex flex-col gap-(--space-micro) px-(--space-inline) pt-(--space-micro)">
      {notes.map((note, i) => (
        <p key={i} className="text-caption-11 text-text-tertiary">
          {note}
        </p>
      ))}
    </div>
  );
}

// --- pdf.viewPages ----------------------------------------------------------------

/** One page at the size it was drawn, in a dialog. */
export function PdfPageZoom({
  page,
  open,
  onOpenChange,
  portal,
}: {
  page: PdfPage;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  portal?: boolean;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* As wide as the window allows, not a form's width: the page keeps
          its own pixels and scrolls inside when it is larger still. The
          `!` is for DialogContent's own sm:max-w-sm: a variant is another
          conflict group to cn, so it would stay and win from sm up. */}
      <DialogContent
        portal={portal}
        aria-describedby={undefined}
        className="max-h-screen-85 w-auto max-w-screen-gutter! overflow-auto"
      >
        <DialogTitle className="text-label-12 text-text-secondary">
          {`Page ${page.page}${page.region ? " · region" : ""}`}
        </DialogTitle>
        {page.src ? (
          <img
            src={page.src}
            alt={`Page ${page.page}`}
            width={page.widthPx}
            height={page.heightPx}
            className="h-auto max-w-none rounded-control"
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function PageThumb({ page }: { page: PdfPage }) {
  const [open, setOpen] = useState(false);
  const label = `Page ${page.page}${page.region ? " · region" : ""}`;
  // The page's own proportions, so the placeholder of a reloaded row takes
  // the room its picture took while the turn was live.
  const ratio = { aspectRatio: `${page.widthPx} / ${page.heightPx}` };
  if (!page.src) {
    return (
      <div
        data-testid="pdf-page-placeholder"
        className="flex w-32 items-center justify-center rounded-control bg-(--state-soft) text-caption-11 text-text-tertiary"
        style={ratio}
      >
        {label}
      </div>
    );
  }
  return (
    <>
      <button
        type="button"
        aria-label={`Open page ${page.page} at full size`}
        onClick={() => setOpen(true)}
        className="flex w-32 cursor-zoom-in flex-col gap-(--space-micro) rounded-control text-left focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none"
      >
        <img
          src={page.src}
          alt={`Page ${page.page}`}
          width={page.widthPx}
          height={page.heightPx}
          className="h-auto w-full rounded-control bg-(--state-soft)"
          style={ratio}
        />
        <span className="text-caption-11 text-text-tertiary">{label}</span>
      </button>
      <PdfPageZoom page={page} open={open} onOpenChange={setOpen} />
    </>
  );
}

/** `pdf.viewPages` — the pages the agent looked at, as it saw them while the turn was live. */
export function PdfPagesCard({ pages, heading }: { pages: PdfPages; heading?: string }) {
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Images}
        title={heading ?? "PDF pages"}
        meta={pages.pages.map((page) => `p.${page.page}`).join(", ")}
      />
      <FramedCardPreview>
        <p className="truncate text-caption-11 text-text-tertiary" title={pages.caption}>
          {pages.caption}
        </p>
        <div className="flex flex-wrap items-start gap-(--space-inline)">
          {pages.pages.map((page, i) => (
            <PageThumb key={`${page.page}-${i}`} page={page} />
          ))}
        </div>
        {pages.imagesDropped ? (
          <p className="text-caption-11 text-text-tertiary">Page images aren't kept after the session reloads</p>
        ) : null}
      </FramedCardPreview>
    </FramedCard>
  );
}

// --- pdf.search -------------------------------------------------------------------

function Snippet({ text, query, match }: { text: string; query: string; match: string }) {
  return (
    <>
      {markMatches(text, query, match).map((span, i) =>
        span.mark ? (
          <mark key={i} className="rounded-sm bg-(--feedback-selected) text-text-primary">
            {span.text}
          </mark>
        ) : (
          <span key={i}>{span.text}</span>
        ),
      )}
    </>
  );
}

/** `pdf.search` — where the words are, page by page. */
export function PdfSearchCard({ search, heading }: { search: PdfSearch; heading?: string }) {
  const shown = search.hits.slice(0, MAX_ROWS);
  const stopped = search.complete
    ? ""
    : search.resumeFromPage
      ? `Stopped at page ${search.resumeFromPage}`
      : "Stopped before the last page";
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={FileSearch}
        title={heading ?? `PDF search · “${search.query}”`}
        meta={`${plural(search.hits.length, "hit")} · ${plural(search.pagesSearched, "page")} searched`}
      />
      <FramedCardPreview flush>
        {shown.length === 0 ? (
          <p className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
            {search.complete ? "No matches" : "No matches so far"}
          </p>
        ) : null}
        {shown.map((hit, i) => (
          <FramedCardRow
            key={`${hit.page}-${i}`}
            label={
              <span title={hit.snippet}>
                <span className="text-code-12-medium text-text-secondary">{`p.${hit.page}`}</span>
                {" · "}
                <Snippet text={hit.snippet} query={search.query} match={hit.match} />
              </span>
            }
          />
        ))}
        <FramedCardMore count={search.hits.length - shown.length} />
        {stopped || search.truncated ? (
          <p className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
            {[stopped, search.truncated ? "More matches than were returned" : ""].filter(Boolean).join(" · ")}
          </p>
        ) : null}
        {search.pagesWithoutText.length > 0 ? (
          <p className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
            {`No text layer, not searched: p.${search.pagesWithoutText.join(", ")}`}
          </p>
        ) : null}
      </FramedCardPreview>
    </FramedCard>
  );
}

// --- pdf.readText -----------------------------------------------------------------

function PageSection({ page, text, defaultOpen }: { page: number; text: string; defaultOpen: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  const bodyId = useId();
  return (
    <div className="flex flex-col">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={bodyId}
        onClick={() => setOpen((value) => !value)}
        className="flex h-(--control-height-xs) w-full min-w-0 cursor-pointer items-center gap-(--space-micro) rounded-sm px-(--space-micro) text-left text-label-12 text-text-secondary transition-colors duration-(--motion-fast) hover:bg-(--state-soft) focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none"
      >
        <ChevronRight
          aria-hidden="true"
          className={cn(
            "size-(--icon-12) shrink-0 transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none",
            open && "rotate-90",
          )}
        />
        {`Page ${page}`}
      </button>
      <AnimatedCollapse open={open} className="pt-(--space-micro)">
        <div id={bodyId} className="max-h-64 overflow-auto px-(--space-micro) text-code-12 break-words whitespace-pre-wrap text-text-primary">
          {text}
        </div>
      </AnimatedCollapse>
    </div>
  );
}

/** `pdf.readText` — the text the agent read, one page per section. */
export function PdfTextCard({ text, heading }: { text: PdfText; heading?: string }) {
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader icon={FileText} title={heading ?? "PDF text"} meta={plural(text.pages.length, "page")} />
      <FramedCardPreview>
        <p className="truncate text-caption-11 text-text-tertiary" title={text.header}>
          {text.header}
        </p>
        {text.pages.map((page, i) => (
          <PageSection key={`${page.page}-${i}`} page={page.page} text={page.text} defaultOpen={i === 0} />
        ))}
        {text.footer.map((line, i) => (
          <p key={i} className="text-caption-11 text-text-tertiary">
            {line}
          </p>
        ))}
      </FramedCardPreview>
    </FramedCard>
  );
}

// --- pdf.info / pdf.list ----------------------------------------------------------

/** `pdf.info` — what the document is, as rows of label and value. */
export function PdfInfoCard({ info, heading }: { info: PdfInfo; heading?: string }) {
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader icon={Info} title={heading ?? "PDF document"} meta={plural(info.document.pages, "page")} />
      <FramedCardPreview>
        <dl className="flex flex-col gap-(--space-micro)">
          {info.rows.map(([label, value]) => (
            <div key={label} className="flex min-w-0 items-baseline gap-(--space-inline)">
              <dt className="w-20 shrink-0 text-caption-11 text-text-tertiary">{label}</dt>
              <dd className="min-w-0 truncate text-body-13 text-text-primary" title={value}>
                {value}
              </dd>
            </div>
          ))}
        </dl>
        <Notes notes={info.notes} />
      </FramedCardPreview>
    </FramedCard>
  );
}

/** `pdf.list` — the documents attached to this session. */
export function PdfListCard({ documents, heading }: { documents: PdfDocument[]; heading?: string }) {
  const shown = documents.slice(0, MAX_ROWS);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader icon={Files} title={heading ?? "PDF documents"} meta={plural(documents.length, "document")} />
      <FramedCardPreview flush>
        {shown.length === 0 ? (
          <p className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
            No documents attached
          </p>
        ) : null}
        {shown.map((doc) => (
          <FramedCardRow
            key={doc.id}
            label={doc.name}
            trailing={doc.id}
            detail={[
              plural(doc.pages, "page"),
              doc.sizeBytes > 0 ? formatBytes(doc.sizeBytes) : "",
              doc.textLayer ? `text layer ${doc.textLayer}` : "",
            ]
              .filter(Boolean)
              .join(" · ")}
          />
        ))}
        <FramedCardMore count={documents.length - shown.length} />
      </FramedCardPreview>
    </FramedCard>
  );
}
