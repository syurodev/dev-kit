import { useState } from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { ArrowRight, Bot, User } from "lucide-react";
import { splitComments, type Board, type Comment, type Transition } from "@/bridge/tasks";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { Tabs, TabsContent, TabsCount, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { comment } from "../taskActions";
import { relativeTime, statusLabel } from "../taskModel";

const LIST_MOTION = { duration: 180, easing: "cubic-bezier(0.23, 1, 0.32, 1)" };

/** "user" is the person at this machine; "agent:<session>" is an agent session. */
function authorName(author: string): { name: string; agent: boolean } {
  if (author.startsWith("agent:")) return { name: "Agent", agent: true };
  if (author === "user" || author === "") return { name: "You", agent: false };
  return { name: author, agent: false };
}

function Author({ author, at }: { author: string; at: string }) {
  const { name, agent } = authorName(author);
  const Icon = agent ? Bot : User;
  return (
    <div className="flex items-center gap-1.5 text-caption-11 text-text-tertiary">
      <Icon aria-hidden className={cn("size-(--icon-12)", agent && "text-status-success")} />
      <span className="text-caption-11-medium text-text-secondary" title={author}>
        {name}
      </span>
      <span className="tabular-nums" title={at}>
        {relativeTime(at)}
      </span>
    </div>
  );
}

function CommentList({ comments, empty }: { comments: readonly Comment[]; empty: string }) {
  const [listRef] = useAutoAnimate<HTMLUListElement>(LIST_MOTION);
  return (
    <ul ref={listRef} className="flex flex-col">
      {comments.length === 0 ? (
        <li className="py-3 text-body-12 text-text-tertiary">{empty}</li>
      ) : (
        comments.map((entry) => (
          <li
            key={entry.id}
            className={cn(
              "flex flex-col gap-1 border-t border-border-subtle py-2.5 first:border-t-0",
              // Drawn at once, confirmed when the bridge answers.
              entry.id.startsWith("pending:") && "opacity-60",
            )}
          >
            <Author author={entry.author} at={entry.createdAt} />
            <p className="text-body-13 whitespace-pre-wrap text-text-primary">{entry.body}</p>
          </li>
        ))
      )}
    </ul>
  );
}

/**
 * The composer. Its draft is its own state, so typing a comment renders this
 * box and not the thread above it (frontend-performance §1).
 */
function Composer({ taskId }: { taskId: string }) {
  const [draft, setDraft] = useState("");
  const send = () => {
    const body = draft.trim();
    if (!body) return;
    setDraft("");
    void comment(taskId, body).then((ok) => {
      // A refused comment comes back into the box rather than being lost.
      if (!ok) setDraft((current) => current || body);
    });
  };
  return (
    <div className="flex flex-col gap-2 pt-2">
      <Textarea
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            send();
          }
        }}
        placeholder="Leave a comment — an agent holding this task reads it"
        aria-label="Comment"
        className="min-h-16 text-body-13"
      />
      <div className="flex items-center gap-2">
        <span className="flex items-center gap-1 text-caption-11 text-text-tertiary">
          <Kbd>⌘↵</Kbd> to send
        </span>
        <Button size="sm" className="ml-auto" disabled={!draft.trim()} onClick={send}>
          Comment
        </Button>
      </div>
    </div>
  );
}

function HistoryList({ board, history }: { board: Board; history: readonly Transition[] }) {
  if (history.length === 0) {
    return <p className="py-3 text-body-12 text-text-tertiary">No moves yet.</p>;
  }
  // Newest first: the question this answers is "what happened last".
  const entries = [...history].reverse();
  return (
    <ul className="flex flex-col">
      {entries.map((entry, index) => (
        <li
          key={`${entry.at}-${index}`}
          className="flex h-8 items-center gap-2 border-t border-border-subtle first:border-t-0"
        >
          {/* A move from nothing is the task being made, and one that stays in
              its column is a session picking it up (internal/tasks: Create,
              Claim) — neither is a move. */}
          {entry.from && entry.from === entry.to ? (
            <span className="text-body-12 text-text-secondary">Picked up</span>
          ) : (
            <>
              <span className="text-body-12 text-text-secondary">
                {entry.from ? statusLabel(board, entry.from) : "Created"}
              </span>
              <ArrowRight aria-hidden className="size-(--icon-12) text-text-tertiary" />
              <span className="text-label-12 text-text-primary">{statusLabel(board, entry.to)}</span>
            </>
          )}
          <span className="ml-auto">
            <Author author={entry.by} at={entry.at} />
          </span>
        </li>
      ))}
    </ul>
  );
}

type Tab = "conversation" | "activity" | "history";

/**
 * What was said, what DevKit recorded, and where the task has been. System
 * comments stay out of the conversation — they are the service's own record
 * of what it ran, not something anybody said.
 */
export function TaskActivity({
  taskId,
  board,
  comments,
  history,
}: {
  taskId: string;
  board: Board;
  comments: readonly Comment[];
  history: readonly Transition[];
}) {
  const [tab, setTab] = useState<Tab>("conversation");
  const split = splitComments([...comments]);

  return (
    <Tabs value={tab} onValueChange={(value) => setTab(value as Tab)} className="flex flex-col gap-2">
      <TabsList variant="line" className="w-fit">
        <TabsTrigger value="conversation">
          Conversation <TabsCount>{split.conversation.length}</TabsCount>
        </TabsTrigger>
        <TabsTrigger value="activity">
          Activity <TabsCount>{split.activity.length}</TabsCount>
        </TabsTrigger>
        <TabsTrigger value="history">
          History <TabsCount>{history.length}</TabsCount>
        </TabsTrigger>
      </TabsList>
      <TabsContent value="conversation" className="flex flex-col">
        <CommentList comments={split.conversation} empty="Nothing said yet." />
        <Composer taskId={taskId} />
      </TabsContent>
      <TabsContent value="activity">
        <CommentList comments={split.activity} empty="DevKit has recorded nothing on this task yet." />
      </TabsContent>
      <TabsContent value="history">
        <HistoryList board={board} history={history} />
      </TabsContent>
    </Tabs>
  );
}
