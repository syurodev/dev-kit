import { useState, type ReactNode } from "react";
import { Link2, TriangleAlert, X } from "lucide-react";
import { FileIcon } from "@/components/file-icon/FileIcon";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { formatBytes } from "@/bridge/agentRuntime";
import type { AgentAttachmentInfo } from "@/bridge/agenthub";
import { cn } from "@/lib/utils";
import { useComposerImagesStore } from "@/stores/composerImages";
import { decodeEntityRefUri } from "@/workbench/entityRef";
import type { DraftChip } from "../../composerDraft";
import { splitName } from "./chipName";
import { useAttachmentInfo } from "./useAttachmentInfo";

/** What DevKit does with a file of each kind, in the card. */
const KIND_NOTE: Record<string, string> = {
  text: "Text — sent with the prompt.",
  image: "Image — sent as a picture.",
  pdf: "PDF — kept as a document of this session; the agent reads it with DevKit's PDF tools.",
  word: "Word — its text is extracted and sent.",
  excel: "Excel — its text is extracted and sent.",
};

/**
 * A file, an image or a reference attached to a prompt, drawn the same way
 * wherever it stands in the draft. One family: 24px, an icon, the name. A long
 * name keeps its end and its extension (chipName.ts); the folder is not in the
 * chip, it is in the card that opens when the pointer rests on it.
 *
 * A file DevKit cannot send shows a warning where its icon was, and the reason
 * in the card. The chip is not a gate: sending still goes through and the
 * turn's own refusal still has the last word.
 */
export function AttachmentChip({
  chip,
  onRemove,
  className,
}: {
  chip: DraftChip;
  /** Shown as a × on hover and focus; the field's own Backspace works too. */
  onRemove?: () => void;
  className?: string;
}) {
  // The card asks again when it opens, so size and problems are not older than the hover.
  const [opened, setOpened] = useState(0);
  const info = useAttachmentInfo(chip.kind === "file" ? chip.key : null, opened);
  const image = useComposerImagesStore((state) => (chip.kind === "image" ? state.byId[chip.key] : undefined));
  const problem = chip.kind === "file" && info?.problem ? info.problem : "";
  const { head, tail } = splitName(chip.label);

  const icon =
    problem ? (
      <TriangleAlert aria-hidden="true" className="size-3 shrink-0 text-status-danger" />
    ) : chip.kind === "file" ? (
      <FileIcon path={chip.key} className="size-3" />
    ) : chip.kind === "image" ? (
      image ? (
        <img src={image.previewUrl} alt="" className="size-4 shrink-0 rounded-xs object-cover" />
      ) : (
        <FileIcon path={chip.label || "image.png"} className="size-3" />
      )
    ) : (
      <Link2 aria-hidden="true" className="size-3 shrink-0 text-text-tertiary" />
    );

  return (
    <HoverCard openDelay={400} closeDelay={120} onOpenChange={(open) => open && setOpened((n) => n + 1)}>
      <HoverCardTrigger asChild>
        <span
          data-testid="attachment-chip"
          data-kind={chip.kind}
          data-problem={problem ? "" : undefined}
          className={cn(
            "group/chip inline-flex h-6 max-w-full min-w-0 select-none items-center gap-1.5 rounded-md bg-(--state-soft) px-2 align-middle text-body-13 text-text-secondary",
            problem && "text-status-danger",
            className,
          )}
        >
          {icon}
          {/* One run of text, so the cut and its tail read as one name, not two items. */}
          <span className="flex min-w-0">
            <span className="min-w-0 truncate">{head}</span>
            {tail ? <span className="shrink-0">{tail}</span> : null}
          </span>
          {onRemove ? (
            <button
              type="button"
              aria-label={`Remove ${chip.label}`}
              // The field keeps its caret: a click on × must not take the focus.
              onMouseDown={(event) => event.preventDefault()}
              onClick={onRemove}
              className="-mr-1 grid size-4 shrink-0 place-items-center rounded-xs text-text-tertiary opacity-0 outline-none hover:text-text-primary focus-visible:opacity-100 focus-visible:ring-1 focus-visible:ring-ring group-hover/chip:opacity-100"
            >
              <X aria-hidden="true" className="size-3" />
            </button>
          ) : null}
        </span>
      </HoverCardTrigger>
      <HoverCardContent align="start" side="top" className="w-80" data-testid="attachment-card">
        <AttachmentCard chip={chip} info={info} image={image?.previewUrl} imageSize={image?.size} problem={problem} />
      </HoverCardContent>
    </HoverCard>
  );
}

function AttachmentCard({
  chip,
  info,
  image,
  imageSize,
  problem,
}: {
  chip: DraftChip;
  info?: AgentAttachmentInfo;
  image?: string;
  imageSize?: number;
  problem: string;
}) {
  const ref = chip.kind === "ref" ? decodeEntityRefUri(chip.key) : null;
  const facts: ReactNode[] = [];
  if (info?.size) facts.push(formatBytes(info.size));
  if (chip.kind === "image" && imageSize !== undefined) facts.push(formatBytes(imageSize));
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      {image ? <img src={image} alt={chip.label} className="max-h-48 w-full rounded-sm bg-surface-base object-contain" /> : null}
      <p className="text-body-13-medium break-all text-text-primary">{chip.label}</p>
      {chip.kind === "file" ? (
        <p className="text-code-12 break-all text-text-tertiary select-text">{chip.key}</p>
      ) : null}
      {ref ? (
        <p className="text-code-12 break-all text-text-tertiary select-text">
          {ref.module}/{ref.kind} · {ref.id}
        </p>
      ) : null}
      {facts.length > 0 ? <p className="text-caption-12 text-text-secondary">{facts.join(" · ")}</p> : null}
      {problem ? (
        <p role="alert" className="text-caption-12 text-status-danger">
          {problem}
        </p>
      ) : info && KIND_NOTE[info.kind] ? (
        <p className="text-caption-12 text-text-secondary">{KIND_NOTE[info.kind]}</p>
      ) : null}
    </div>
  );
}
