import * as React from "react"
import { Dialog as DialogPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { XIcon } from "lucide-react"

function Dialog({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Root>) {
  return <DialogPrimitive.Root data-slot="dialog" {...props} />
}

function DialogTrigger({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Trigger>) {
  return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />
}

function DialogPortal({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Portal>) {
  return <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />
}

function DialogClose({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Close>) {
  return <DialogPrimitive.Close data-slot="dialog-close" {...props} />
}

function DialogOverlay({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      data-slot="dialog-overlay"
      className={cn(
        "fixed inset-0 isolate z-50 bg-black/10 supports-backdrop-filter:backdrop-blur-xs duration-(--motion-base) data-[state=closed]:duration-(--motion-fast) data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=closed]:animate-out data-[state=closed]:fade-out-0",
        className
      )}
      {...props}
    />
  )
}

/**
 * Layout the body takes from the content box.
 *
 * Callers style DialogContent with one className that mixes the box
 * (`sm:max-w-2xl`, a background) with how its children are laid out
 * (`gap-0 p-0 flex flex-col`). The body sits between the box and the
 * children, so it inherits the second half and the children are placed
 * exactly as they were. Padding stays on the box.
 */
const INHERIT_LAYOUT: React.CSSProperties = {
  display: "inherit",
  flexDirection: "inherit",
  flexWrap: "inherit",
  gap: "inherit",
  alignItems: "inherit",
  alignContent: "inherit",
  justifyItems: "inherit",
  justifyContent: "inherit",
  gridTemplateColumns: "inherit",
}

/**
 * The dialog follows its content's height instead of jumping to it.
 *
 * The body is `h-fit shrink-0`, so its height is what the content needs and
 * never what the animating box is passing through. When it changes, the box
 * animates from the height it was showing to the height it now has.
 *
 * WAAPI on the element rather than React state: nothing re-renders, the box's
 * own height stays `auto` once the animation ends, and a caller's `max-h`
 * still clamps both ends. A ref callback rather than an effect, because Radix
 * mounts the content only when the dialog opens.
 */
function followContentHeight(body: HTMLDivElement | null) {
  const box = body?.parentElement
  if (!body || !box || typeof ResizeObserver === "undefined") return

  const overflow = box.style.overflow
  let settled = box.offsetHeight
  let running: Animation | null = null

  const observer = new ResizeObserver(() => {
    // Mid-animation the box is where the animation has it; start from there.
    const from = running ? parseFloat(getComputedStyle(box).height) : settled
    running?.cancel()
    running = null
    const to = box.offsetHeight
    settled = to

    if (Math.abs(from - to) < 1 || matchMedia("(prefers-reduced-motion: reduce)").matches) {
      box.style.overflow = overflow
      return
    }

    const tokens = getComputedStyle(document.documentElement)
    box.style.overflow = "hidden"
    const animation = box.animate([{ height: `${from}px` }, { height: `${to}px` }], {
      duration: parseFloat(tokens.getPropertyValue("--motion-base")) || 180,
      easing: tokens.getPropertyValue("--ease-out-strong").trim() || "ease-out",
    })
    running = animation
    animation.onfinish = () => {
      if (running !== animation) return
      running = null
      box.style.overflow = overflow
    }
  })
  observer.observe(body)

  return () => {
    observer.disconnect()
    running?.cancel()
  }
}

function DialogContent({
  className,
  children,
  showCloseButton = true,
  portal = true,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & {
  showCloseButton?: boolean
  portal?: boolean
}) {
  const content = (
    <>
      <DialogOverlay />
      <DialogPrimitive.Content
        data-slot="dialog-content"
        className={cn(
          "fixed top-1/2 left-1/2 z-50 grid w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 gap-4 rounded-xl bg-popover p-4 text-body-13 text-popover-foreground ring-1 ring-foreground/10 outline-none sm:max-w-sm duration-(--motion-base) data-[state=closed]:duration-(--motion-fast) data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
          className
        )}
        {...props}
      >
        <div
          ref={followContentHeight}
          data-slot="dialog-body"
          className="h-fit w-full min-w-0 shrink-0"
          style={INHERIT_LAYOUT}
        >
          {children}
        </div>
        {showCloseButton && (
          <DialogPrimitive.Close data-slot="dialog-close" asChild>
            <Button
              variant="ghost"
              className="absolute top-2 right-2"
              size="icon-sm"
            >
              <XIcon />
              <span className="sr-only">Close</span>
            </Button>
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </>
  )

  if (!portal) return content
  return <DialogPortal>{content}</DialogPortal>
}

function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-header"
      className={cn("flex flex-col gap-2", className)}
      {...props}
    />
  )
}

function DialogFooter({
  className,
  showCloseButton = false,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  showCloseButton?: boolean
}) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn(
        "-mx-4 -mb-4 flex flex-col-reverse gap-2 rounded-b-xl border-t bg-muted/50 p-4 sm:flex-row sm:justify-end",
        className
      )}
      {...props}
    >
      {children}
      {showCloseButton && (
        <DialogPrimitive.Close asChild>
          <Button variant="outline">Close</Button>
        </DialogPrimitive.Close>
      )}
    </div>
  )
}

function DialogTitle({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn(
        "text-title-16",
        className
      )}
      {...props}
    />
  )
}

function DialogDescription({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      data-slot="dialog-description"
      className={cn(
        "text-body-13 text-muted-foreground *:[a]:underline *:[a]:underline-offset-3 *:[a]:hover:text-foreground",
        className
      )}
      {...props}
    />
  )
}

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
}
