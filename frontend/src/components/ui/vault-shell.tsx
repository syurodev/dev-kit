import * as React from "react"
import { Vault } from "lucide-react"

import { cn } from "@/lib/utils"

export interface VaultShellProps extends React.ComponentProps<"div"> {
  title?: string
  badgeText?: string
  children?: React.ReactNode
}

function VaultShell({
  title = "DevKit Vault",
  badgeText = "LOCAL · ENCRYPTED",
  children,
  className,
  ...props
}: VaultShellProps) {
  return (
    <div
      data-slot="vault-shell"
      className={cn(
        "relative flex flex-col w-full max-w-[960px] min-h-[540px] rounded-[18px] bg-bg-canvas border border-border overflow-hidden shadow-2xl transition-all",
        className
      )}
      {...props}
    >
      {/* 30px Title Bar / Header */}
      <div
        data-slot="vault-shell-header"
        className="flex h-[30px] w-full shrink-0 items-center justify-between border-b border-border-subtle bg-surface-base px-4 select-none"
      >
        <div className="flex items-center gap-2">
          <Vault className="size-3.5 text-text-secondary" strokeWidth={1.5} />
          <span className="text-[11px] font-semibold text-text-primary">
            {title}
          </span>
        </div>

        <span className="text-[9px] font-medium tracking-wider text-status-success uppercase">
          {badgeText}
        </span>
      </div>

      {/* Main Content Area */}
      <div
        data-slot="vault-shell-content"
        className="flex flex-1 items-center justify-center p-6 md:p-10"
      >
        <div className="flex flex-col items-center w-full max-w-[640px] min-h-[380px] rounded-[18px] bg-surface-base border border-border-subtle p-6 md:p-8 relative">
          {children}
        </div>
      </div>
    </div>
  )
}

export { VaultShell }
