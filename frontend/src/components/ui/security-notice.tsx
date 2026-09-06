import * as React from "react"
import { Shield, TriangleAlert } from "lucide-react"

import { cn } from "@/lib/utils"

export type SecurityNoticeType = "secure" | "irreversible"

export interface SecurityNoticeProps extends React.ComponentProps<"div"> {
  type?: SecurityNoticeType
  title?: string
  description?: string
}

function SecurityNotice({
  type = "secure",
  title,
  description,
  className,
  ...props
}: SecurityNoticeProps) {
  const isSecure = type === "secure"
  const isIrreversible = type === "irreversible"

  const defaultTitle = isSecure
    ? "Encrypted locally"
    : "Irrecoverable if both secrets are lost"

  const defaultDescription = isSecure
    ? "The vault key is held in memory only while unlocked. Locking zeroizes it."
    : "DevKit cannot recover your data without the master password or recovery key."

  return (
    <div
      data-slot="security-notice"
      data-type={type}
      className={cn(
        "flex w-full items-center gap-3 rounded-control border px-4 py-3 transition-colors backdrop-blur-[12px] shadow-[inset_0px_1px_0px_0px_rgba(255,255,255,0.06)]",
        isSecure && "h-[72px] min-h-[72px] border-status-success bg-[rgba(153,225,217,0.12)]",
        isIrreversible && "h-[84px] min-h-[84px] border-[rgba(255,255,255,0.1)] bg-[rgba(29,29,29,0.55)]",
        className
      )}
      {...props}
    >
      {/* Consequence accent bar for irreversible notice: h 40, w3, rounded-[2px], --status-recovery */}
      {isIrreversible && (
        <div
          data-slot="security-notice-accent-bar"
          className="h-10 w-[3px] shrink-0 rounded-[2px] bg-status-recovery"
        />
      )}

      {/* Icon: 18px */}
      <div className="shrink-0 flex items-center justify-center">
        {isSecure ? (
          <Shield className="size-[18px] text-status-success" strokeWidth={1.5} />
        ) : (
          <TriangleAlert className="size-[18px] text-status-recovery" strokeWidth={1.5} />
        )}
      </div>

      {/* Text content: gap-1 = 4px */}
      <div className="flex flex-1 flex-col gap-1 min-w-0">
        <p
          data-slot="security-notice-title"
          className="text-title-14 text-text-primary"
        >
          {title || defaultTitle}
        </p>
        <p
          data-slot="security-notice-description"
          className="text-body-12 text-text-secondary"
        >
          {description || defaultDescription}
        </p>
      </div>
    </div>
  )
}

export { SecurityNotice }
