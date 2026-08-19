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
        "flex w-full items-start gap-3 rounded-[10px] border px-4 py-3 transition-colors",
        isSecure && "border-status-success/80 bg-status-success/10",
        isIrreversible && "border-border bg-surface-raised",
        className
      )}
      {...props}
    >
      {/* Consequence accent bar for irreversible notice */}
      {isIrreversible && (
        <div className="h-9 w-[3px] shrink-0 rounded-full bg-status-recovery mt-0.5" />
      )}

      {/* Icon */}
      <div className="shrink-0 mt-0.5">
        {isSecure ? (
          <Shield className="size-[18px] text-status-success" strokeWidth={1.5} />
        ) : (
          <TriangleAlert className="size-[18px] text-status-recovery" strokeWidth={1.5} />
        )}
      </div>

      {/* Text content */}
      <div className="flex flex-1 flex-col gap-1 min-w-0">
        <p className="text-[14px] font-semibold text-text-primary leading-tight">
          {title || defaultTitle}
        </p>
        <p className="text-[12px] font-normal text-text-secondary leading-relaxed">
          {description || defaultDescription}
        </p>
      </div>
    </div>
  )
}

export { SecurityNotice }
