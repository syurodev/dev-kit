import {
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  ChevronsUpDown,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Pagination,
  PaginationContent,
  PaginationItem,
} from "@/components/ui/pagination";
import { BROWSE_PAGE_SIZE, BROWSE_PAGE_SIZES } from "../databaseStore";

/**
 * The pager, at the right of the table toolbar.
 *
 * In the toolbar rather than floating over the rows — the app already docks
 * something at the bottom of the pane, and two things anchored to the same
 * edge fight for it. The toolbar row had dead space on its right anyway.
 *
 * No card of its own: it sits in a row of ghost buttons, and a bordered pill
 * around half of them makes the toolbar read as two toolbars.
 *
 * Four arrows around a range, no page numbers: a row of chips is a second way
 * to say what "1401–1600 of 8,420" already says, and the only one of them
 * anybody aimed at was the last — which is what the last-page arrow is.
 *
 * Every control in the pill is one height (--control-height-sm). Mixed heights
 * inside a rounded-full pill leave the radius sitting unevenly around its
 * contents, which is what it looks like: a rounding bug.
 */
export function BrowsePager({
  pageIndex,
  pageSize,
  rowCount,
  totalRows,
  lastPage,
  hasNext,
  keyed,
  loading,
  onGoToPage,
  onPageSizeChange,
}: {
  pageIndex: number;
  pageSize: number;
  /** Rows on this page, which is what names the range. */
  rowCount: number;
  /** Rows matching the filter, or null while uncounted. */
  totalRows: number | null;
  /** The last page's index, or null while the count is unknown. */
  lastPage: number | null;
  hasNext: boolean;
  /** False when the table has no key, so pages are counted rather than walked. */
  keyed: boolean;
  loading: boolean;
  onGoToPage: (page: number) => void;
  onPageSizeChange: (size: number) => void;
}) {
  const firstRow = pageIndex * pageSize + 1;
  const lastRow = pageIndex * pageSize + rowCount;
  const atLast = lastPage === null ? !hasNext : pageIndex >= lastPage;

  return (
    <div className="flex items-center gap-1">
      <Pagination className="mx-0 w-auto">
        <PaginationContent>
          <PaginationItem>
            <Button
              variant="ghost"
              size="icon-sm"
              className="rounded-full"
              aria-label="First page"
              onClick={() => onGoToPage(0)}
              disabled={loading || pageIndex === 0}
            >
              <ChevronsLeft />
            </Button>
          </PaginationItem>
          <PaginationItem>
            <Button
              variant="ghost"
              size="icon-sm"
              className="rounded-full"
              aria-label="Previous page"
              onClick={() => onGoToPage(pageIndex - 1)}
              disabled={loading || pageIndex === 0}
            >
              <ChevronLeft />
            </Button>
          </PaginationItem>

          {/* Between the arrows, not beside them: the range is what the
                arrows move, and the thing being moved belongs between the
                controls that move it. Clicking it sets the page size, which is
                what decides how far one press of an arrow goes. */}
          <PaginationItem>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="xs"
                  className="gap-1 rounded-full px-2 text-code-11 tabular-nums"
                  aria-label="Rows per page"
                >
                  {rowCount === 0
                    ? "no rows"
                    : totalRows !== null
                      ? `${firstRow}–${lastRow} of ${totalRows.toLocaleString()}`
                      : `${firstRow}–${lastRow}${hasNext ? "+" : ""}`}
                  <ChevronsUpDown className="text-text-tertiary" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="center" side="top">
                <DropdownMenuLabel>Rows per page</DropdownMenuLabel>
                <DropdownMenuRadioGroup
                  value={String(pageSize)}
                  onValueChange={(value) => onPageSizeChange(Number(value))}
                >
                  {BROWSE_PAGE_SIZES.map((size) => (
                    <DropdownMenuRadioItem
                      key={size}
                      value={String(size)}
                      className="tabular-nums"
                    >
                      {size.toLocaleString()}
                      {size === BROWSE_PAGE_SIZE ? (
                        <span className="ml-1 text-text-tertiary">Default</span>
                      ) : null}
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </PaginationItem>

          <PaginationItem>
            <Button
              variant="ghost"
              size="icon-sm"
              className="rounded-full"
              aria-label="Next page"
              onClick={() => onGoToPage(pageIndex + 1)}
              disabled={loading || atLast}
            >
              <ChevronRight />
            </Button>
          </PaginationItem>
          <PaginationItem>
            <Button
              variant="ghost"
              size="icon-sm"
              className="rounded-full"
              aria-label="Last page"
              onClick={() => onGoToPage(lastPage ?? 0)}
              disabled={loading || lastPage === null || pageIndex === lastPage}
            >
              <ChevronsRight />
            </Button>
          </PaginationItem>
        </PaginationContent>
      </Pagination>

      {!keyed ? (
        // After the arrows, where DataGrip keeps its overflow: it is a fact
        // about how this pager moves, and the cost lands when you use it.
        <>
          <span className="mx-0.5 h-4 w-px bg-border-subtle" aria-hidden />
          <span
            className="flex h-(--control-height-sm) items-center rounded-full bg-state-soft px-2 text-code-11 text-text-tertiary"
            title="This table has no primary or unique key, so pages are counted rather than walked. Deep pages are slower, and rows inserted while you page can shift the window."
          >
            unkeyed
          </span>
        </>
      ) : null}
    </div>
  );
}
