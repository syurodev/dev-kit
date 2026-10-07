import { describe, expect, it } from "bun:test"
import { renderToString } from "react-dom/server"
import { Alert, AlertTitle, AlertDescription, AlertContent } from "./alert"
import { StepProgress } from "./step-progress"
import { LoadingStatus } from "./loading-status"
import { RecoveryKeyField } from "./recovery-key-field"
import { SecurityNotice } from "./security-notice"

describe("Step 3 Feedback & Security Reconciliation", () => {
  describe("Alert (Figma 29:39)", () => {
    it("renders Info variant with 72px height, status-info border, and title-13", () => {
      const html = renderToString(
        <Alert variant="info">
          <AlertContent>
            <AlertTitle>System Notice</AlertTitle>
            <AlertDescription>All vaults operating normally.</AlertDescription>
          </AlertContent>
        </Alert>,
      )

      expect(html).toContain('data-slot="alert"')
      expect(html).toContain("h-18")
      expect(html).toContain("border-status-info")
      expect(html).toContain("bg-status-info/8")
      expect(html).not.toContain("backdrop-blur")
      expect(html).toContain("text-title-13")
      expect(html).toContain("text-body-12")
      expect(html).toContain("gap-1")
    })

    it("renders Success variant with status-success border and feedback-success-soft bg", () => {
      const html = renderToString(
        <Alert variant="success">
          <AlertContent>
            <AlertTitle>Backup Created</AlertTitle>
            <AlertDescription>Snapshot saved to local disk.</AlertDescription>
          </AlertContent>
        </Alert>,
      )

      expect(html).toContain("border-status-success")
      expect(html).toContain("bg-status-success/12")
    })

    it("renders Warning variant with status-warning border and raw warning bg", () => {
      const html = renderToString(
        <Alert variant="warning">
          <AlertContent>
            <AlertTitle>Emergency Access</AlertTitle>
            <AlertDescription>Accessing recovery mode.</AlertDescription>
          </AlertContent>
        </Alert>,
      )

      expect(html).toContain("border-status-warning")
      expect(html).toContain("bg-status-warning/10")
    })

    it("renders Error variant with status-danger border and raw error bg", () => {
      const html = renderToString(
        <Alert variant="error">
          <AlertContent>
            <AlertTitle>Validation Error</AlertTitle>
            <AlertDescription>Invalid password format.</AlertDescription>
          </AlertContent>
        </Alert>,
      )

      expect(html).toContain("border-status-danger")
      expect(html).toContain("bg-status-danger/12")
    })
  })

  describe("Step Progress (Figma 29:76)", () => {
    it("renders 4-column layout on 600px frame without glow shadow on active milestone", () => {
      const html = renderToString(<StepProgress currentStep={2} />)

      expect(html).toContain('data-slot="step-progress"')
      expect(html).toContain("grid-cols-4")
      expect(html).toContain("max-w-150")
      // Figma 29:40 has no fill, no blur and no inset highlight. This is the
      // one component in 05/06 outside the shared glass recipe.
      expect(html).not.toContain("backdrop-blur")
      expect(html).not.toContain("inset_0px_1px")
      // Active milestone is 56px (w-14)
      expect(html).toContain("w-14")
      // Completed milestone is 32px (w-8)
      expect(html).toContain("w-8")
      // Milestone to label gap is 15px
      expect(html).toContain("gap-(--space-step-label)")
      // Glowing shadow must NOT exist
      expect(html).not.toContain("shadow-[0_0_8px")
    })
  })

  describe("Loading / Vault status (Figma 29:77)", () => {
    it("renders 320x48 frame with radius 12, grey spinner, and raised surface", () => {
      const html = renderToString(
        <LoadingStatus text="Checking secure storage status…" />,
      )

      expect(html).toContain('data-slot="loading-status"')
      expect(html).toContain("w-80")
      expect(html).toContain("h-12")
      expect(html).toContain("rounded-lg")
      expect(html).toContain("bg-surface-raised")
      expect(html).toContain("border-border-subtle")
      expect(html).not.toContain("backdrop-blur")
      // Spinner is grey --icon-secondary #c6c8c2
      expect(html).toContain("text-icon-secondary")
      expect(html).toContain("text-body-13-medium")
    })
  })

  describe("Recovery Key Field (Figma 30:51)", () => {
    it("renders 132px height, radius 14, 36px action buttons, and preserved header text", () => {
      const html = renderToString(
        <RecoveryKeyField recoveryKey="TEST-KEY-1234-5678-ABCD" defaultRevealed />,
      )

      expect(html).toContain('data-slot="recovery-key-field"')
      expect(html).toContain("h-33")
      expect(html).toContain("rounded-float")
      expect(html).toContain("bg-surface-well")
      expect(html).not.toContain("backdrop-blur")
      expect(html).toContain("border-border-subtle")
      expect(html).toContain("RECOVERY KEY · SHOWN ONCE")
      expect(html).toContain("text-code-12-medium")
      expect(html).toContain("size-9")
    })
  })

  describe("Security Notice (Figma 30:68)", () => {
    it("renders Secure type with 72px height, status-success border, and title-14", () => {
      const html = renderToString(<SecurityNotice type="secure" />)

      expect(html).toContain('data-slot="security-notice"')
      expect(html).toContain("h-18")
      expect(html).toContain("border-status-success")
      expect(html).toContain("bg-status-success/12")
      expect(html).toContain("text-title-14")
      expect(html).toContain("items-center")
      expect(html).not.toContain("backdrop-blur")
    })

    it("renders Irreversible type with 84px height, accent bar, and recessed surface", () => {
      const html = renderToString(<SecurityNotice type="irreversible" />)

      expect(html).toContain("h-21")
      expect(html).toContain("border-border-subtle")
      expect(html).toContain("bg-surface-well")
      expect(html).toContain('data-slot="security-notice-accent-bar"')
      expect(html).toContain("h-10")
      expect(html).toContain("w-0.75")
      expect(html).toContain("rounded-2xs")
      expect(html).toContain("bg-status-recovery")
    })
  })

  // Vault Shell (Figma 30:69) was removed on 2026-09-06. Its 30px titlebar was
  // one of five title bars in the tree; the app now has exactly one, at 40px,
  // because macOS pins the traffic lights to the window and a bar that changes
  // height between the gate and the workbench cannot keep them aligned in both.
  // Figma 30:69 still says "Title bar remains 30px" and needs updating.

  describe("VaultGate Integration", () => {
    // Figma `Vault Content / Checking` (751:1435) draws a spinner beside the
    // status line and a mono footnote under it — not the LoadingStatus card.
    it("renders the checking step inside the Vault Stage", () => {
      const { VaultGate } = require("../../workbench/VaultGate")
      const html = renderToString(
        <VaultGate>
          <div>Workbench Main Area</div>
        </VaultGate>,
      )

      expect(html).toContain("Checking secure storage status…")
      expect(html).toContain("DevKit is verifying the local vault state")
      expect(html).toContain("No workspace data is exposed during this check.")
      // The gate is the whole screen, so the stage is opaque and the titlebar
      // carries no workbench actions (748:1391).
      expect(html).toContain("bg-surface-base")
      expect(html).not.toContain("Workbench Main Area")
    })
  })
})
