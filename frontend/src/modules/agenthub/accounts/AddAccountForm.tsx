import { useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ACCOUNT_LABEL_MAX } from "./accountModel";

/**
 * "Add account": a name, then the account exists with nothing signed in, and its
 * row offers the sign-in. Closed until asked for, like the model profiles form.
 */
export function AddAccountForm({
  vendorName,
  busy,
  onAdd,
  initiallyOpen = false,
  initialLabel = "",
}: {
  vendorName: string;
  busy: boolean;
  /** Resolves true when the account was added, so the form can close. */
  onAdd: (label: string) => Promise<boolean>;
  /** Opens with the form already showing (the gallery draws it so). */
  initiallyOpen?: boolean;
  initialLabel?: string;
}) {
  const [open, setOpen] = useState(initiallyOpen);
  const [label, setLabel] = useState(initialLabel);
  const [problem, setProblem] = useState("");

  async function submit() {
    const name = label.trim();
    if (!name) {
      setProblem("Give the account a name.");
      return;
    }
    setProblem("");
    if (await onAdd(name)) {
      setLabel("");
      setOpen(false);
    }
  }

  if (!open) {
    return (
      <div className="flex items-center gap-3">
        <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => setOpen(true)}>
          <Plus />
          Add account
        </Button>
      </div>
    );
  }

  return (
    <form
      aria-label={`Add a ${vendorName} account`}
      className="flex flex-col gap-3 rounded-control border border-border-subtle bg-surface-raised p-3.5"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <div className="w-72">
        <Input
          label="Account name"
          value={label}
          maxLength={ACCOUNT_LABEL_MAX}
          placeholder="Work"
          error={problem || undefined}
          helperText={`Only for you. You sign in to the ${vendorName} account on the next step.`}
          onChange={(event) => {
            setLabel(event.target.value);
            setProblem("");
          }}
        />
      </div>
      <div className="flex items-center gap-2">
        <Button type="submit" size="lg" disabled={busy}>
          {busy ? "Adding…" : "Add account"}
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="lg"
          disabled={busy}
          onClick={() => {
            setOpen(false);
            setLabel("");
            setProblem("");
          }}
        >
          Cancel
        </Button>
      </div>
    </form>
  );
}
