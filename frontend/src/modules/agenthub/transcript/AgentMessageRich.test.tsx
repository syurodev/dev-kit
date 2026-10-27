import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentProse } from "./AgentMessageRich";
import { TranscriptActionsProvider } from "./transcriptActions";

const render = (text: string) =>
  renderToString(
    <TooltipProvider>
      <AgentProse text={text} settled />
    </TooltipProvider>,
  );

const TABLE = [
  "| Folder | Files | Note |",
  "| :--- | ---: | :---: |",
  "| root (`src/`) | 12 | **main** entry |",
  "| gateway/ | 3 | ok |",
].join("\n");

describe("markdown tables in prose", () => {
  test("draw as the shared resizable table", () => {
    const html = render(TABLE);
    expect(html).toContain('data-testid="agent-table"');
    expect(html).toContain("table-fixed");
    expect(html).toContain("<colgroup>");
    expect(html.match(/role="separator"/g)?.length).toBe(3);
  });

  test("keep their alignment while allowing every column to wrap", () => {
    const html = render(TABLE);
    const firstRow = html.split("<tbody")[1].split("</tr>")[0];
    const cells = firstRow.split("<td").slice(1);
    expect(cells[0]).not.toContain("whitespace-nowrap");
    expect(cells[1]).toContain("text-right");
    expect(cells[1]).toContain("tabular-nums");
    expect(cells[2]).toContain("text-center");
    expect(cells[1]).not.toContain("whitespace-nowrap");
  });

  test("keep inline markdown in their cells", () => {
    const html = render(TABLE);
    expect(html).toContain("<strong");
    expect(html).toContain("src/");
  });

  test("retain long first-column prose without clipping it to one line", () => {
    const text = "Partition chứa event của các domain khác. Hiện tại ảnh hưởng chỉ nằm trong một domain nhưng mô tả cần được đọc đầy đủ.";
    const html = render(`| Phát hiện | Mức | Giảm nhẹ |\n| --- | --- | --- |\n| ${text} | Trung bình | Tách topic theo domain |`);
    const firstCell = html.split("<tbody")[1].split("<td")[1].split("</td>")[0];
    expect(firstCell).toContain(text);
    expect(firstCell).toContain("wrap-anywhere");
    expect(firstCell).not.toContain("truncate");
    expect(firstCell).not.toContain("whitespace-nowrap");
  });

  test("file badges in table cells preserve long labels and line suffixes", () => {
    const path = "services/gateway/internal/handlers/VeryLongEventDispatchFunction.java";
    const text = `| Phát hiện |\n| --- |\n| Trước \`${path}:128\` sau badge và [tên file dài cần đọc đầy đủ](${path}). |`;
    const html = renderToString(
      <TooltipProvider>
        <TranscriptActionsProvider value={{ onOpenFile: () => {} }}>
          <AgentProse text={text} settled />
        </TranscriptActionsProvider>
      </TooltipProvider>,
    );
    expect(html).toContain('data-testid="agent-file-ref"');
    expect(html).toContain(`aria-label="File ${path}:128, opens in editor"`);
    expect(html).toContain(path);
    expect(html).toContain(":128");
    expect(html).toContain("sau badge");
    const labels = [...html.matchAll(/data-file-label[^>]*>/g)].map((match) => match[0]);
    expect(labels).toHaveLength(2);
    for (const label of labels) expect(label).toContain("wrap-anywhere");
    expect(html.split("<tbody")[1]).not.toContain("truncate");
  });

  test("windowed table file badges keep the fixed row height", () => {
    const rows = Array.from({ length: 101 }, () => "| `src/VeryLongEventDispatchFunction.java` |");
    const html = renderToString(
      <TooltipProvider>
        <TranscriptActionsProvider value={{ onOpenFile: () => {} }}>
          <AgentProse text={["| File |", "| --- |", ...rows].join("\n")} />
        </TranscriptActionsProvider>
      </TooltipProvider>,
    );
    expect(html).toContain('data-testid="agent-file-ref"');
    expect(html).not.toContain("h-auto min-h-6");
    expect(html).toContain("h-6 whitespace-nowrap");
  });
});
