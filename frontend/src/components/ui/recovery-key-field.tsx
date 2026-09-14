import * as React from "react"
import { Copy, Check, Download, Eye, EyeOff } from "lucide-react"

import { cn } from "@/lib/utils"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./tooltip"

export interface RecoveryKeyFieldProps extends React.ComponentProps<"div"> {
  recoveryKey: string
  defaultRevealed?: boolean
  onCopy?: () => void
  onDownload?: () => void
}

function RecoveryKeyField({
  recoveryKey,
  defaultRevealed = false,
  onCopy,
  onDownload,
  className,
  ...props
}: RecoveryKeyFieldProps) {
  const [isRevealed, setIsRevealed] = React.useState(defaultRevealed)
  const [isCopied, setIsCopied] = React.useState(false)

  const maskedKey = "•••• •••• •••• •••• •••• ••••"

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(recoveryKey)
      setIsCopied(true)
      onCopy?.()
      setTimeout(() => setIsCopied(false), 3000)
    } catch {
      // Fallback
    }
  }

  const handleDownload = () => {
    if (onDownload) {
      onDownload()
      return
    }
    const blob = new Blob([`DevKit Vault Recovery Key:\n${recoveryKey}\n\nKeep this key safe and offline.`], {
      type: "text/plain;charset=utf-8",
    })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = "devkit-recovery-key.txt"
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <TooltipProvider>
      <div
        data-slot="recovery-key-field"
        className={cn(
          "flex flex-col justify-between w-full max-w-[640px] h-[132px] min-h-[132px] rounded-float border border-glass-border bg-glass-panel px-(--space-recovery-x) py-(--space-step-label) backdrop-blur-[20px] shadow-[inset_0px_1px_0px_0px_rgba(255,255,255,0.06)] transition-colors",
          className
        )}
        {...props}
      >
        {/* Header line */}
        <div className="flex items-center justify-between">
          <span
            data-slot="recovery-key-label"
            className={cn(
              "text-label-11-caps transition-colors",
              isCopied ? "text-status-success" : "text-status-recovery"
            )}
          >
            RECOVERY KEY · SHOWN ONCE
          </span>

          <button
            type="button"
            data-slot="recovery-key-reveal-toggle"
            onClick={() => setIsRevealed(!isRevealed)}
            className="text-text-tertiary hover:text-text-primary text-caption-11-medium flex items-center gap-1 transition-colors cursor-pointer"
          >
            {isRevealed ? (
              <>
                <EyeOff className="size-3.5" strokeWidth={1.5} />
                <span>Hide</span>
              </>
            ) : (
              <>
                <Eye className="size-3.5" strokeWidth={1.5} />
                <span>Reveal</span>
              </>
            )}
          </button>
        </div>

        {/* Key Display & Actions */}
        <div className="flex items-center justify-between gap-3">
          <p
            data-slot="recovery-key-value"
            className="text-code-12-medium text-text-primary select-all font-mono"
          >
            {isRevealed ? recoveryKey : maskedKey}
          </p>

          <div className="flex items-center gap-2 shrink-0">
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  data-slot="recovery-key-copy-button"
                  onClick={handleCopy}
                  aria-label="Copy recovery key"
                  className="flex size-9 items-center justify-center rounded-control border border-glass-border bg-glass-panel backdrop-blur-[12px] text-text-primary hover:bg-white/10 transition-colors cursor-pointer"
                >
                  {isCopied ? (
                    <Check className="size-4 text-status-success" strokeWidth={2} />
                  ) : (
                    <Copy className="size-4 text-text-primary" strokeWidth={1.5} />
                  )}
                </button>
              </TooltipTrigger>
              <TooltipContent>
                {isCopied ? "Copied to clipboard" : "Copy recovery key"}
              </TooltipContent>
            </Tooltip>

            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  data-slot="recovery-key-download-button"
                  onClick={handleDownload}
                  aria-label="Download recovery key"
                  className="flex size-9 items-center justify-center rounded-control border border-glass-border bg-glass-panel backdrop-blur-[12px] text-text-primary hover:bg-white/10 transition-colors cursor-pointer"
                >
                  <Download className="size-4 text-text-primary" strokeWidth={1.5} />
                </button>
              </TooltipTrigger>
              <TooltipContent>Download recovery key file</TooltipContent>
            </Tooltip>
          </div>
        </div>

        {/* Consequence footer note */}
        <p
          data-slot="recovery-key-footer"
          className={cn(
            "text-caption-11 transition-colors",
            isCopied ? "text-status-success" : "text-text-tertiary"
          )}
        >
          {isCopied
            ? "Copied to clipboard. Clear clipboard after storing safely."
            : "Copy or download, then store offline. This value cannot be shown again."}
        </p>
      </div>
    </TooltipProvider>
  )
}

export { RecoveryKeyField }
