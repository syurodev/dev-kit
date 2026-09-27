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
        "flex w-full items-center gap-3 rounded-control border px-4 py-3 transition-colors backdrop-blur-md shadow-glass-edge",
        isSecure && "h-18 min-h-18 border-status-success bg-status-success/12",
        isIrreversible && "h-21 min-h-21 border-glass-border bg-glass-panel",
        className
      )}
      {...props}
    >
      {/* Consequence accent bar for irreversible notice: h 40, w3, rounded-2xs, --status-recovery */}
      {isIrreversible && (
        <div
          data-slot="security-notice-accent-bar"
          className="h-10 w-0.75 shrink-0 rounded-2xs bg-status-recovery"
        />
      )}

      {/* Icon: 18px */}
      <div className="shrink-0 flex items-center justify-center">
        {isSecure ? (
          <Shield className="size-4.5 text-status-success" strokeWidth={1.5} />
        ) : (
          <TriangleAlert className="size-4.5 text-status-recovery" strokeWidth={1.5} />
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
