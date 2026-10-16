import { useState } from "react";
import { FileIcon } from "@/components/file-icon/FileIcon";
import { Button } from "@/components/ui/button";
import { AgentFileChip } from "@/modules/agenthub/transcript/AgentFileChip";
import { AgentProse } from "@/modules/agenthub/transcript/AgentMessageRich";
import { TranscriptActionsProvider } from "@/modules/agenthub/transcript/transcriptActions";

const FILES = ["Main.java", "application.yaml", "App.tsx", "main.go", "package.json", "README.md", "Dockerfile", "docker-compose.yaml", ".gitignore", "config.toml", "schema.sql", "report.pdf", "photo.png", "archive.zip", "unknown.custom"];

/** Offline icons, narrow chips and real prose references on both Mono grounds. */
export function FileIconsSection() {
  const [selected, setSelected] = useState("No file opened");
  const setMode = (mode: "light" | "dark") => {
    document.documentElement.classList.toggle("dark", mode === "dark");
    document.documentElement.dataset.theme = mode;
  };
  return (
    <section id="file-icons" className="flex flex-col gap-4">
      <h2 className="text-title-16 text-text-primary">Material file icons</h2>
      <div className="flex gap-2">
        <Button variant="outline" size="xs" onClick={() => setMode("dark")}>Preview dark icons</Button>
        <Button variant="outline" size="xs" onClick={() => setMode("light")}>Preview light icons</Button>
      </div>
      <div className="grid gap-6 rounded-control bg-surface-base p-4 lg:grid-cols-2">
        <div className="grid grid-cols-2 gap-1">
          {FILES.map((file) => <div key={file} className="flex min-w-0 items-center gap-2 py-1 text-code-12 text-text-primary"><FileIcon path={file} /><span className="truncate">{file}</span></div>)}
        </div>
        <div className="flex min-w-0 flex-col gap-4">
          <h3 className="text-body-13-medium text-text-primary">Agent Hub · buttons and badges</h3>
          <div className="flex flex-wrap gap-2">
            <AgentFileChip path="Main.java" line={42} onClick={() => setSelected("Main.java:42")} />
            <AgentFileChip path="application.yaml" />
            <AgentFileChip path="report.pdf" kind="document" detail="6 pages" />
          </div>
          <div className="w-64 max-w-full space-y-2" data-testid="file-icons-narrow">
            <AgentFileChip path="src/main/java/com/example/billing/VeryLongFileName.java" line={128} onClick={() => setSelected("Long file:128")} />
            <TranscriptActionsProvider value={{ onOpenFile: (path, line) => setSelected(`${path}:${line ?? 1}`) }}>
              <AgentProse text={'Sửa `src/Main.java:42` và [cấu hình](application.yaml). Đường dẫn dài: `src/main/java/com/example/billing/VeryLongFileName.java:128`.'} />
            </TranscriptActionsProvider>
          </div>
          <p role="status" className="text-body-13 text-text-secondary">{selected}</p>
        </div>
      </div>
    </section>
  );
}
