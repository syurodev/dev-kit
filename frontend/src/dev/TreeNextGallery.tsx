import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  Database,
  FileText,
  Folder,
  FolderOpen,
  KeyRound,
  Layers,
  Lock,
  MoreHorizontal,
  Plus,
  Table2,
  Type,
} from "lucide-react";
import { TreeView, type TreeNodeData } from "@/components/tree-view";
import { useTheme } from "@/theme/ThemeProvider";
import { FAMILIES, resolvedPalettes } from "@/theme/families";
import { applyThemeFamily } from "@/theme/applyTheme";
import type { ThemeFamilyPreference } from "@/bridge/preferences";

/**
 * PROTOTYPE — the shared tree, against a Database rail and a Notes-shaped one,
 * at the rail's 280 and its 240 floor, beside today's TreeView. Interactive:
 * rows open, select, show their hover actions and pin while scrolling.
 */

const GLYPH = "size-3.5 shrink-0 text-text-tertiary";

function RowActions() {
  const action = "flex size-5 items-center justify-center rounded-sm text-text-tertiary hover:bg-state-soft hover:text-text-primary";
  return (
    <>
      <button type="button" aria-label="New" className={action} onClick={(e) => e.stopPropagation()}>
        <Plus className="size-3" />
      </button>
      <button type="button" aria-label="More" className={action} onClick={(e) => e.stopPropagation()}>
        <MoreHorizontal className="size-3" />
      </button>
    </>
  );
}

function useTreeState(initialOpen: string[], initialSelected: string) {
  const [open, setOpen] = useState(() => new Set(initialOpen));
  const [selected, setSelected] = useState(initialSelected);
  return (id: string, extra: Partial<TreeNodeData> = {}): Pick<TreeNodeData, "id" | "expanded" | "onToggle" | "selected" | "onSelect"> & Partial<TreeNodeData> => ({
    id,
    expanded: open.has(id),
    onToggle: () =>
      setOpen((current) => {
        const next = new Set(current);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return next;
      }),
    selected: selected === id,
    onSelect: () => setSelected(id),
    ...extra,
  });
}

const LONG_TABLES = [
  "backup_his_medical_records_2023_archive",
  "crm_campaign_branch_maps",
  "billing_invoice_details",
  "ba_his_employee_map",
];

function useDatabaseNodes(): TreeNodeData[] {
  const n = useTreeState(
    ["prod", "orders", "orders.public", "orders.public.customers", "orders.public.orders_archive"],
    "orders.public.customers",
  );
  const folderIcon = (id: string, isOpen: boolean) =>
    isOpen ? <FolderOpen key={id} className={GLYPH} /> : <Folder key={id} className={GLYPH} />;

  const columns: TreeNodeData[] = [
    { id: "id", label: "id", icon: <KeyRound className={GLYPH} />, meta: "bigint" },
    { id: "email", label: "email", icon: <Type className={GLYPH} />, meta: "varchar(255)" },
    { id: "full_name", label: "full_name", icon: <Type className={GLYPH} />, meta: "text" },
    { id: "created_at", label: "created_at", icon: <Type className={GLYPH} />, meta: "timestamptz" },
  ];
  const tables: TreeNodeData[] = [
    ...LONG_TABLES,
    ...Array.from({ length: 56 }, (_, i) => `table_${String(i).padStart(2, "0")}`),
  ].map((name) => ({
    ...n(`orders.public.${name}`),
    label: name,
    title: `public.${name}`,
    icon: <Table2 className={GLYPH} />,
    expandable: true,
  }));
  tables.splice(1, 0, {
    ...n("orders.public.customers"),
    label: "customers",
    icon: <Table2 className={GLYPH} />,
    expandable: true,
    children: columns,
    actions: <RowActions />,
  });
  tables.splice(2, 0, {
    ...n("orders.public.orders_archive"),
    label: "orders_archive",
    icon: <Table2 className={GLYPH} />,
    expandable: true,
    loading: true,
  });

  const schema = (conn: string, name: string, children: TreeNodeData[], count: number): TreeNodeData => ({
    ...n(`${conn}.${name}`),
    label: name,
    icon: <Layers className={GLYPH} />,
    expandable: true,
    sticky: 24,
    count,
    children,
    actions: <RowActions />,
  });

  const orders = n("orders");
  return [
    {
      ...n("prod"),
      label: "Production",
      icon: folderIcon("prod", n("prod").expanded ?? false),
      expandable: true,
      count: 2,
      children: [
        {
          ...orders,
          label: "orders-db",
          icon: <Database className={GLYPH} />,
          expandable: true,
          sticky: 0,
          count: 2,
          actions: <RowActions />,
          children: [
            schema("orders", "public", tables, tables.length),
            schema("orders", "billing", [], 18),
          ],
        },
        {
          ...n("analytics"),
          label: "analytics-ro",
          icon: <Database className={GLYPH} />,
          meta: <Lock className="size-3" />,
          expandable: true,
          count: 4,
        },
      ],
    },
    {
      ...n("dev"),
      label: "Development",
      icon: folderIcon("dev", n("dev").expanded ?? false),
      expandable: true,
      count: 1,
      children: [{ ...n("local"), label: "local-mysql", icon: <Database className={GLYPH} />, expandable: true }],
    },
  ];
}

function useNotesNodes(): TreeNodeData[] {
  const n = useTreeState(["updates", "docs3", "sales", "untitled", "support"], "peter");
  const folder = (id: string, label: string, count: number | undefined, children: TreeNodeData[], extra: Partial<TreeNodeData> = {}) => {
    const base = n(id);
    return {
      ...base,
      label,
      icon: base.expanded ? <FolderOpen className={GLYPH} /> : <Folder className={GLYPH} />,
      expandable: true,
      count,
      children,
      actions: <RowActions />,
      ...extra,
    };
  };
  const doc = (id: string, label: string, count?: number): TreeNodeData => ({
    ...n(id),
    label,
    icon: <FileText className={GLYPH} />,
    count,
    actions: <RowActions />,
  });
  return [
    folder("updates", "Product Updates", 18, [
      folder("docs3", "Docs 3.0", undefined, [doc("strategy", "Product Strategy")], { meta: <Lock className="size-3" /> }),
      folder("sales", "Sales Pipeline", 16, [
        folder("kickoff", "Week Kickoff", 11, [doc("k1", "Monday")], { meta: <Lock className="size-3" /> }),
        folder("untitled", "Untitled Folder", 2, [doc("demo", "Product Demo"), doc("training", "Acme AI Sales Training", 1)]),
        doc("feedback", "Customer Feedback"),
      ]),
    ]),
    folder("support", "Customer Support Docs", 9, [
      folder("critical", "Critical Issues", 2, [doc("c1", "Login loop")]),
      doc("peter", "peter x louise"),
      doc("onboarding", "Onboarding Issues", 2),
      doc("resources", "Product Resources", 5),
    ]),
  ];
}

/** The narrowest room a label gets at each level, measured off the rendered rows. */
function LabelRoom({ target, names }: { target: React.RefObject<HTMLDivElement | null>; names: string[] }) {
  const [room, setRoom] = useState<string>("");
  useEffect(() => {
    const measure = () => {
      const el = target.current;
      if (!el) return;
      const byLevel = new Map<number, number>();
      for (const row of el.querySelectorAll<HTMLElement>("[role=treeitem][data-level]")) {
        const button = row.querySelector("button");
        const label = row.querySelector<HTMLElement>("[data-slot=tree-label]");
        if (!button || !label) continue;
        const width = Math.round(button.getBoundingClientRect().right - label.getBoundingClientRect().left);
        const level = Number(row.dataset.level);
        byLevel.set(level, Math.min(byLevel.get(level) ?? Infinity, width));
      }
      setRoom(
        [...byLevel]
          .sort(([a], [b]) => a - b)
          .map(([level, width]) => `${names[level] ?? `level ${level}`} ${width}px`)
          .join(" · "),
      );
    };
    const t = setTimeout(measure, 50);
    return () => clearTimeout(t);
  });
  return <p className="text-code-11 text-text-tertiary">Label room — {room || "…"}</p>;
}

function Rail({ width, height = 440, children }: { width: number; height?: number; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-code-11 text-text-tertiary">{width}px</span>
      {/* The rail's own ground, which is what the pinned rows paint. */}
      <div className="flex flex-col rounded-md bg-(--background-app) p-2" style={{ width, height }}>
        {children}
      </div>
    </div>
  );
}

function Sample({ title, nodes, levels }: { title: string; nodes: TreeNodeData[]; levels: string[] }) {
  const wide = useRef<HTMLDivElement>(null);
  const narrow = useRef<HTMLDivElement>(null);
  return (
    <div className="flex flex-col gap-2">
      <span className="text-body-12 text-text-secondary">{title}</span>
      <div className="flex flex-wrap items-start gap-6">
        <div ref={wide} className="flex flex-col gap-1">
          <Rail width={280}>
            <TreeView nodes={nodes} className="flex-1" />
          </Rail>
          <LabelRoom target={wide} names={levels} />
        </div>
        <div ref={narrow} className="flex flex-col gap-1">
          <Rail width={240}>
            <TreeView nodes={nodes} className="flex-1" />
          </Rail>
          <LabelRoom target={narrow} names={levels} />
        </div>
      </div>
    </div>
  );
}

/** Canvas-normalised sRGBA, so color-mix(), oklab() and color(srgb …) all read the same. */
function rgba(css: string): [number, number, number, number] {
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const ctx = canvas.getContext("2d")!;
  ctx.clearRect(0, 0, 1, 1);
  ctx.fillStyle = css;
  ctx.fillRect(0, 0, 1, 1);
  const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
  return [r, g, b, a / 255];
}

function over(fg: [number, number, number, number], bg: [number, number, number, number]): [number, number, number, number] {
  const a = fg[3];
  return [fg[0] * a + bg[0] * (1 - a), fg[1] * a + bg[1] * (1 - a), fg[2] * a + bg[2] * (1 - a), 1];
}

function contrast(a: [number, number, number, number], b: [number, number, number, number]): number {
  const lum = ([r, g, b]: number[]) => {
    const [R, G, B] = [r, g, b].map((v) => {
      const c = v / 255;
      return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * R + 0.7152 * G + 0.0722 * B;
  };
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

type ContrastRow = { palette: string; border: number; label: number; guide: number };

/**
 * Every palette in both modes: the selection border against the rail (a mark,
 * 3:1), the selected label against its tint (text, 4.5:1), and the guide
 * against the rail (decoration — reported, not required).
 */
function ContrastTable() {
  const { family } = useTheme();
  const probe = useRef<HTMLDivElement>(null);
  const [rows, setRows] = useState<ContrastRow[]>([]);

  function check() {
    const root = document.documentElement;
    const el = probe.current;
    if (!el) return;
    const savedTheme = root.dataset.theme;
    const savedDark = root.classList.contains("dark");
    const savedScheme = root.style.colorScheme;
    const out: ContrastRow[] = [];
    for (const id of Object.keys(FAMILIES)) {
      applyThemeFamily(id as ThemeFamilyPreference, resolvedPalettes(id as ThemeFamilyPreference));
      for (const mode of ["dark", "light"] as const) {
        // All three, as ThemeProvider sets them: tokens written with
        // light-dark() follow color-scheme, not the attribute or the class.
        root.dataset.theme = mode;
        root.classList.toggle("dark", mode === "dark");
        root.style.colorScheme = mode;
        const rail = rgba(getComputedStyle(el).backgroundColor);
        const selected = el.querySelector<HTMLElement>("[data-probe=selected]")!;
        const guide = el.querySelector<HTMLElement>("[data-probe=guide]")!;
        const tint = over(rgba(getComputedStyle(selected).backgroundColor), rail);
        out.push({
          palette: `${id} ${mode}`,
          border: contrast(over(rgba(getComputedStyle(selected).borderTopColor), rail), rail),
          label: contrast(over(rgba(getComputedStyle(selected).color), tint), tint),
          guide: contrast(over(rgba(getComputedStyle(guide).borderLeftColor), rail), rail),
        });
      }
    }
    const current = family ?? "devkit";
    applyThemeFamily(current, resolvedPalettes(current));
    if (savedTheme) root.dataset.theme = savedTheme;
    root.classList.toggle("dark", savedDark);
    root.style.colorScheme = savedScheme;
    setRows(out);
  }

  const cell = (value: number, floor?: number) => (
    <td className={floor !== undefined && value < floor ? "text-status-danger" : "text-text-primary"}>{value.toFixed(2)}</td>
  );

  return (
    <div className="flex flex-col gap-2">
      <div ref={probe} className="flex w-60 items-center gap-2 rounded-md bg-(--background-app) p-2">
        <div data-probe="selected" className="flex-1 rounded-md border border-status-info bg-status-info/10 px-2 text-body-13 text-text-primary">
          customers
        </div>
        <span data-probe="guide" className="h-6" style={{ borderLeft: "1px solid color-mix(in srgb, var(--text-tertiary) 35%, var(--background-app))" }} />
      </div>
      <button type="button" className="w-fit rounded-md border border-border-default px-2 py-1 text-body-12 hover:bg-state-soft" onClick={check}>
        Check every palette
      </button>
      {rows.length ? (
        <table className="w-fit text-code-11">
          <thead className="text-text-tertiary">
            <tr>
              <th className="pr-4 text-left font-normal">palette</th>
              <th className="pr-4 text-left font-normal">selection border ≥3</th>
              <th className="pr-4 text-left font-normal">selected label ≥4.5</th>
              <th className="text-left font-normal">guide</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.palette}>
                <td className="pr-4 text-text-secondary">{row.palette}</td>
                {cell(row.border, 3)}
                {cell(row.label, 4.5)}
                {cell(row.guide)}
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </div>
  );
}

export function TreeNextSection() {
  const database = useDatabaseNodes();
  const notes = useNotesNodes();
  return (
    <section className="border-b border-border-subtle px-6 py-5">
      <h2 className="text-label-11-caps text-text-tertiary">08 Tree — prototype</h2>
      <div className="mt-4 flex flex-col gap-8">
        <Sample
          title="Database rail — folder › connection › schema › table › column. Scroll to see the pins; hover a schema or table for its actions."
          nodes={database}
          levels={["folder", "connection", "schema", "table", "column"]}
        />
        <Sample title="Notes-shaped tree — the reference image's data." nodes={notes} levels={["level 0", "level 1", "level 2", "level 3"]} />
        <ContrastTable />
      </div>
    </section>
  );
}
