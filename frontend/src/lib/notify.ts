import { toast } from "sonner";

/** Stable ids collapse repeated notifications for the same object/task. */
export function notify({ id, title, tone = "success", action }: {
  id: string; title: string; tone?: "success" | "error" | "info";
  action?: { label: string; onClick: () => void };
}) {
  return toast[tone](title, { id, action, duration: action || tone === "error" ? 6000 : 4000, closeButton: true });
}
