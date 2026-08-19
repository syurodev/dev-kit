import * as React from "react"
import { Copy, Check, Download, Eye, EyeOff } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "./button"
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
          "flex flex-col gap-2 rounded-[14px] border p-4.5 bg-surface-base transition-colors w-full max-w-[640px]",
          isCopied ? "border-status-success" : "border-border",
          className
        )}
        {...props}
      >
        {/* Header line */}
        <div className="flex items-center justify-between">
          <span
            className={cn(
              "text-[11px] font-medium tracking-[0.55px] uppercase transition-colors",
              isCopied ? "text-status-success" : "text-status-recovery"
            )}
          >
            {isCopied ? "RECOVERY KEY · COPIED" : "RECOVERY KEY · SHOWN ONCE"}
          </span>

          <button
            type="button"
            onClick={() => setIsRevealed(!isRevealed)}
            className="text-text-tertiary hover:text-text-primary text-[11px] font-medium flex items-center gap-1 transition-colors"
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
        <div className="flex items-center justify-between gap-3 py-1">
          <p className="font-mono text-[15px] font-medium text-text-primary tracking-wide select-all">
            {isRevealed ? recoveryKey : maskedKey}
          </p>

          <div className="flex items-center gap-2 shrink-0">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="secondary"
                  size="icon"
                  onClick={handleCopy}
                  aria-label="Copy recovery key"
                >
                  {isCopied ? (
                    <Check className="size-4 text-status-success" strokeWidth={2} />
                  ) : (
                    <Copy className="size-4 text-text-primary" strokeWidth={1.5} />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {isCopied ? "Copied to clipboard" : "Copy recovery key"}
              </TooltipContent>
            </Tooltip>

            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="secondary"
                  size="icon"
                  onClick={handleDownload}
                  aria-label="Download recovery key"
                >
                  <Download className="size-4 text-text-primary" strokeWidth={1.5} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Download recovery key file</TooltipContent>
            </Tooltip>
          </div>
        </div>

        {/* Consequence footer note */}
        <p
          className={cn(
            "text-[11px] font-normal leading-normal transition-colors",
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
