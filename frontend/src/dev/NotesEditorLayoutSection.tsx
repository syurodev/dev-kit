import { useEffect, useState } from "react";
import { Plate, usePlateEditor } from "platejs/react";
import { DataTable } from "@/components/ui/data-table/DataTable";
import { Editor, EditorContainer } from "@/components/ui/editor";
import { NoteOptionsMenu } from "@/modules/notes/NoteOptionsMenu";
import { NotesEditorKit } from "@/modules/notes/plate/plugins";
import {
  NOTE_CRUMB_CLASS,
  NoteTitleField,
  type NoteView,
  noteBodyClass,
  notePageClass,
  noteTitleClass,
} from "@/modules/notes/noteLayout";

const SAMPLE = `Nguồn: Confluence · Digital Connector Platform. Tổng hợp ngày 2026-10-19.

**Trạng thái:** kiến trúc đã chốt — ADR-0053 và ADR-0054 đã viết, chờ Hội đồng ký; Wave 1 đang chạy.

**Tầng 1 — stack riêng của từng integrator (~40 tổ chức)**

- \`galaxy-g-onboarding-service\` — BFF, giữ Conductor workflow + worker + bù trừ
- \`galaxy-g-authentication-service\` — IdP của **riêng** tổ chức đó
  - nested: không phải OP của Platform
1. numbered item one
2. numbered item two

## 1. Nguồn tham chiếu

| Nguồn | Trạng thái | Vai trò |
| --- | --- | --- |
| \`galaxy-g-governance\` | Đã ký | Xương sống governance: canonical model, EA-CNP-001 và quy tắc đặt tên cho mọi connector |
| ADR-0053 | Chờ ký | Hai làn theo FlowClass (MONEY / ≠ MONEY), mỗi làn một execution mode |
| ADR-0054 | Chờ ký | Onboard theo bậc sandbox → cell, kèm 5 amendment ★A1–A5 |

A short table spans the pane too:

| Wave | Ngày |
| --- | --- |
| 1 | 2026-08-17 |
| 2 | 2026-11-02 |

## 2. Bảng nhiều cột

| Connector | Owner | Lane | Mode | Registry | Runtime | SDK | Trạng thái | Ghi chú |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| SAP S/4 | Team A | MONEY | sync | v2 | jvm | 1.4 | live | Đang chạy production từ tháng 8 |
| Kafka bridge | Team B | ≠ MONEY | async | v2 | go | 0.9 | sandbox | Chờ amendment A3 |
`;

const FOLDERS = [
  { id: "arch", name: "Architecture", color: "blue" },
  { id: "research", name: "Research", color: "green" },
];

const TITLE = "00 · Digital Connector Platform — tổng quan kiến trúc (trang cha của toàn bộ bộ note)";

/**
 * The note editor's page as NoteEditor draws it (title, per-block column,
 * tables using the pane's width), without the bridge. `?gallery#notes-editor-layout`.
 */
export function NotesEditorLayoutSection() {
  const [view, setView] = useState<NoteView>({ fullWidth: false, smallText: false, font: "default", locked: false });
  const [folderId, setFolderId] = useState("arch");
  const flip = (key: "fullWidth" | "smallText" | "locked") => () => setView((v) => ({ ...v, [key]: !v[key] }));
  const [title, setTitle] = useState(TITLE);
  const editor = usePlateEditor({
    plugins: NotesEditorKit,
    value: [],
  });
  useEffect(() => {
    editor.tf.setValue(editor.api.markdown.deserialize(SAMPLE));
  }, [editor]);

  return (
    <section id="notes-editor-layout" className="flex flex-col gap-4 p-6">
      <h2 className="text-heading-24 text-text-primary">Notes editor layout</h2>
      <div
        data-testid="notes-editor-frame"
        className="notes-font-scope h-200 w-full resize-x overflow-hidden rounded-md border border-border-subtle"
      >
        <Plate editor={editor} readOnly={view.locked}>
          <EditorContainer className="h-full min-h-0 overflow-y-auto">
            <div className={notePageClass(view.font)}>
              <div className={NOTE_CRUMB_CLASS}>
                <span className="min-w-0 flex-1 truncate">Notes / Architecture / {title}</span>
                {view.locked && <span className="shrink-0">Locked</span>}
                <NoteOptionsMenu
                  view={view}
                  folders={FOLDERS}
                  folderId={folderId}
                  historyOpen={false}
                  onFontChange={(font) => setView((v) => ({ ...v, font }))}
                  onToggleSmallText={flip("smallText")}
                  onToggleFullWidth={flip("fullWidth")}
                  onToggleLocked={flip("locked")}
                  onCopyContents={() => {}}
                  onDuplicate={() => {}}
                  onMoveTo={setFolderId}
                  onToggleHistory={() => {}}
                  onDelete={() => {}}
                />
              </div>
              <NoteTitleField
                value={title}
                readOnly={view.locked}
                className={noteTitleClass(view.fullWidth)}
                onChange={setTitle}
                onEnter={() => editor.tf.focus({ edge: "start" })}
              />
              <Editor variant="none" className={noteBodyClass(view)} />
            </div>
          </EditorContainer>
        </Plate>
      </div>
      {/* The shared table a note's tables are drawn to match (tool results,
          markdown tables in the agent transcript). */}
      <h3 data-testid="notes-table-reference" className="text-title-16 text-text-primary">
        DataTable, for comparison
      </h3>
      <DataTable
        className="max-w-240 rounded-control border border-border-subtle"
        columns={[
          { key: "source", header: "Nguồn", nowrap: true },
          { key: "state", header: "Trạng thái" },
          { key: "role", header: "Vai trò" },
        ]}
        rows={[
          ["galaxy-g-governance", "Đã ký", "Xương sống governance: canonical model, EA-CNP-001 và quy tắc đặt tên cho mọi connector"],
          ["ADR-0053", "Chờ ký", "Hai làn theo FlowClass (MONEY / ≠ MONEY), mỗi làn một execution mode"],
          ["ADR-0054", "Chờ ký", "Onboard theo bậc sandbox → cell, kèm 5 amendment ★A1–A5"],
        ]}
      />
    </section>
  );
}
