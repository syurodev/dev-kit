import { TriangleAlert } from "lucide-react";
import { EngineMark } from "./EngineBadge";

/**
 * Where this console's statements go.
 *
 * A console is a file now, and a file that says "SELECT * FROM users" says
 * nothing about WHICH users. The header line in the file records it; this is
 * the same fact in the window, and it is in the toolbar beside Run because
 * that is the button it describes.
 *
 * What it shows is the LIVE target, never the recorded one. The two can
 * disagree — a console restored from disk was written against whatever was
 * selected last session — and of the two, only the live one answers the
 * question anybody is actually asking, which is where the next Run lands. The
 * recorded one is shown beside it when they differ, because a query written
 * for one database and about to be sent to another is worth a second's pause;
 * it is a note, not a warning to dismiss, and it goes away as soon as the
 * console is saved against the connection it is now pointed at.
 */
export function ConsoleBinding({
  connection,
  database,
  engine,
  writtenFor,
}: {
  /** The selected connection's name, or "" when none is selected. */
  connection: string;
  /** The schema or database the statements resolve against. */
  database: string;
  engine?: string;
  /** The connection the file was last written for, when that is not this one. */
  writtenFor?: string;
}) {
  return (
    <div className="flex min-w-0 items-center gap-1.5 pl-1 text-code-11 text-text-secondary">
      {connection ? (
        <>
          <EngineMark engine={engine} className="size-3.5" />
          <span className="truncate text-text-primary">{connection}</span>
          {database ? (
            <>
              <span className="text-text-tertiary">/</span>
              <span className="truncate">{database}</span>
            </>
          ) : null}
        </>
      ) : (
        // Not an error and not styled as one: no connection is the state the
        // app opens in. Run is disabled beside it, which is the other half of
        // the same sentence.
        <span className="truncate">No connection selected</span>
      )}
      {writtenFor ? (
        <span className="flex min-w-0 items-center gap-1">
          <TriangleAlert className="size-3 shrink-0 text-status-warning" />
          <span className="truncate">{`written for ${writtenFor}`}</span>
        </span>
      ) : null}
    </div>
  );
}
