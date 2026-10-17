import type { ReactNode } from "react";

/**
 * The guide lines of a tree, and the geometry they hang off, shared by every
 * tree drawn as rows: the sidebar's TreeView and Code Graph's flow outline.
 * One drawing, so the two read as one component.
 */

/**
 * --control-height-sm, the row height of every tree in the app. Rows are
 * fixed-height, which is what lets the tree be windowed by arithmetic instead
 * of by measuring.
 *
 * One height, not one per caller: the database rail drew 24, the agent swarm
 * 32 and Code Graph its own 28 and 33, so the same tree looked like three
 * components from one module to the next. The tree owns its metrics — row
 * height, icon size, spacing, type — and a caller only says what is in a row.
 */
export const TREE_ROW_HEIGHT = 28;

/**
 * 16 a level. A guide needs room for its elbow between the parent's chevron
 * and the child's, and at 16 that elbow is 8px wide on a row with a chevron —
 * the full TREE_ELBOW_RADIUS, where 14 left it 6px and capped the corner there.
 * The guides carry depth, which is what keeps five levels readable in a rail.
 */
export const TREE_INDENT = 16;
/** From the tree's left edge to a top-level row's chevron. */
export const TREE_INSET = 4;
/** The chevron slot, kept on a leaf so names stay on one vertical line. */
export const TREE_CHEVRON = 12;

/**
 * --border-default measured 1.02–1.30 against the rail across the palettes, a
 * guide nobody sees. Tertiary text at 35% measured 1.55–1.93: a line in every
 * palette, well under the labels.
 *
 * Mixed into the rail's ground rather than into transparent: an elbow overlaps
 * the line it leaves, and two translucent borders on one pixel draw it twice
 * as dark. Opaque, the overlap does not show.
 */
export const TREE_GUIDE = "1px solid color-mix(in srgb, var(--text-tertiary) 35%, var(--background-app))";
/**
 * Elbow corner radius, well under the elbow's size so the corner has a straight
 * run on both sides of it. At 8 in an 8px-wide elbow the whole thing was one arc
 * and read as a diagonal slash rather than a rounded corner.
 */
export const TREE_ELBOW_RADIUS = 6;

/**
 * A selected row's highlight, from its chevron to the right edge. Tinted, and
 * the border stays: --status-info is the mark that clears 3:1 in every
 * palette (themeContrast.test.ts), the tint alone does not. Measured
 * 4.51–13.38 in the gallery. A row that is not selected keeps a transparent
 * border, so selecting it cannot move its label.
 */
export const TREE_ROW_SELECTED = "border-status-info bg-status-info/10 text-text-primary";

/** Where a row at `level` (0 for a top row) starts its chevron. */
export const treeContentX = (level: number) => TREE_INSET + level * TREE_INDENT;
/** The guide under a row at `depth`: the centre of its chevron. */
export const treeLineX = (depth: number) => treeContentX(depth) + TREE_CHEVRON / 2;

export type TreeGuidesProps = {
  /** 0 for a top row. */
  level: number;
  /** For each level of the row's path, from the root: whether that node is its parent's last child. */
  lasts: readonly boolean[];
  /** The row is open, so a guide runs down from its chevron. */
  open: boolean;
  /** No chevron of its own: the elbow runs through the empty slot to the label. */
  leaf: boolean;
  rowHeight: number;
};

/**
 * The guides a row draws: a full-height line for every ancestor whose branch
 * continues past it, an elbow from its parent's line into its own chevron, and
 * — when open — a stub down from that chevron to its first child.
 *
 * CSS borders on absolute spans, which snap to the pixel at any display scale.
 * Everything about the lines lives here, so drawing them another way (SVG, for
 * curves or a highlighted path) is a change to this function alone.
 */
export function TreeGuides({ level, lasts, open, leaf, rowHeight }: TreeGuidesProps) {
  const half = rowHeight / 2;
  // A row with a chevron takes the elbow to the chevron. A leaf has an empty
  // chevron slot, and stopping there left its elbow short of the icon —
  // shallow next to the rows above it. It runs through the slot instead.
  const reachesIcon = leaf;
  const lines: ReactNode[] = [];
  for (let depth = 0; depth < level; depth++) {
    const continues = !lasts[depth + 1];
    if (depth === level - 1) {
      // A continuing branch is one straight line the full height of the row,
      // with the elbow laid over it. As the elbow's own border to the middle
      // and a second line from there, the elbow's rounded corner spent its
      // last few px curving away and every row left that much of a gap.
      if (continues) {
        lines.push(
          <span key={`line-${depth}`} style={{ position: "absolute", left: treeLineX(depth), top: 0, bottom: 0, borderLeft: TREE_GUIDE }} />,
        );
      }
      lines.push(
        <span
          key={`elbow-${depth}`}
          style={{
            position: "absolute",
            left: treeLineX(depth),
            top: 0,
            // To the chevron slot's edge: the glyph sits inset in its slot, so
            // this still clears the arrow, and the extra 2px is the straight run.
            width: treeContentX(level) - treeLineX(depth) + (reachesIcon ? TREE_CHEVRON : 0),
            height: half,
            borderLeft: TREE_GUIDE,
            borderBottom: TREE_GUIDE,
            borderBottomLeftRadius: TREE_ELBOW_RADIUS,
          }}
        />,
      );
    } else if (continues) {
      lines.push(
        <span key={`line-${depth}`} style={{ position: "absolute", left: treeLineX(depth), top: 0, bottom: 0, borderLeft: TREE_GUIDE }} />,
      );
    }
  }
  if (open) {
    lines.push(
      <span key="stub" style={{ position: "absolute", left: treeLineX(level), top: half + TREE_CHEVRON / 2, bottom: 0, borderLeft: TREE_GUIDE }} />,
    );
  }
  return <span aria-hidden>{lines}</span>;
}
