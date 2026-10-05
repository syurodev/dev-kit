import { Button } from "@/components/ui/button";
import { FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { useDatabaseStore } from "../databaseStore";

/** Schema state belongs to the connection rail, even with no document open. */
export function SchemaFeedback() {
  const error = useDatabaseStore((s) => s.schemaError);
  const pending = useDatabaseStore((s) => s.indexingSchema);
  const profileId = useDatabaseStore((s) => s.selectedProfileId);
  const refresh = useDatabaseStore((s) => s.refreshSchema);
  if (!profileId) return null;
  if (pending) return <PendingStatus className="shrink-0 px-2 py-2">Indexing schema…</PendingStatus>;
  if (!error) return null;
  return (
    <FeedbackBanner
      tone="error"
      title="Could not index schema"
      className="mx-1 mb-2 w-auto shrink-0 items-start"
      action={
        <Button className="w-full basis-full" variant="outline" size="xs" onClick={() => void refresh(profileId)}>
          Retry schema indexing
        </Button>
      }
    >
      {error}
    </FeedbackBanner>
  );
}
