import { useEffect, useState } from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { SWITCHING_TRUTH, TERMS_WARNING } from "./accountPolicy";

/** The words of the terms warning and the box that accepts it. */
export function AccountRiskBody({
  checked,
  onCheckedChange,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex flex-col gap-3" data-testid="account-risk-body">
      <p className="text-body-13 text-text-secondary">{SWITCHING_TRUTH}</p>
      <p className="text-body-13 text-text-secondary">{TERMS_WARNING}</p>
      <Checkbox
        label="I understand, and I will check the terms of every account I add."
        checked={checked}
        onCheckedChange={(value) => onCheckedChange(value === true)}
      />
    </div>
  );
}

/**
 * Shown the first time switching is turned on, and never again once it was
 * accepted. Turning it on is the explicit action the backend wants before it
 * stamps the time; Cancel leaves the switch where it was.
 */
export function AccountRiskDialog({
  open,
  vendorName,
  onCancel,
  onConfirm,
  portal = true,
}: {
  open: boolean;
  vendorName: string;
  onCancel: () => void;
  onConfirm: () => void;
  /** False draws the panel in place, for a test that has no document to portal into. */
  portal?: boolean;
}) {
  const [checked, setChecked] = useState(false);
  useEffect(() => {
    if (!open) setChecked(false);
  }, [open]);
  return (
    <AlertDialog open={open} onOpenChange={(next) => !next && onCancel()}>
      <AlertDialogContent portal={portal}>
        <AlertDialogHeader>
          <AlertDialogTitle>{`Turn on account switching for ${vendorName}?`}</AlertDialogTitle>
          <AlertDialogDescription>
            {`DevKit can move a ${vendorName} session to another of your accounts when one reaches its limit.`}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AccountRiskBody checked={checked} onCheckedChange={setChecked} />
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction disabled={!checked} onClick={onConfirm}>
            Turn on
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
