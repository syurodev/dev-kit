# Repair dialog lows: R1a-2, R1a-3, R1a-4

Branch `fix/repair-dialog-lows` (base `6ba80806`). Three low findings from
[the R1a review](../../reviews/2026-10-28-r1a-fc-o2-dirswap-review.md). No new error code, no
`AppService` signature change (bindings untouched), no change to what a repair removes or to the
confirmation. Harness activation stays inert.

## R1a-2: the 64-entry bound was unreachable and the refusal said "split it"

Cause. `MaxStaleEntries` (64) bounds the count, but the review the person confirms must also fit
`maxSummaryBytes` (8 KiB). With long names (stored names are shown to 128 characters) 48 entries
already overflow. `PrepareRepair` then returned `ErrTooLargeToShow` ("Split it into smaller changes"),
which cannot be done for a repair, while `DiagnoseStale` still said "repairable" and the window offered
"Review repair".

Choice. Keep the review exact and the bound a pure upper bound; make the diagnosis tell the truth.

- `DiagnoseStale` builds the same review `PrepareRepair` would build and sets the existing `truncated`
  flag when it would not fit (as it already did for more than 64 entries). No new field, so no model or
  binding change; the window already withholds the offer for `truncated`.
- `PrepareRepair` (workspace) and `PrepareSessionRepair` (the same defect in the session repair) map
  `ErrTooLargeToShow` to the existing `ErrRepairTooLarge`, whose sentence now says what is true: the
  settings are too many or too long to show in full, a repair cannot be split, restore the missing
  module or build instead. Same published code (`harness.notAvailable`).
- The panel says this in plain words above the list (not after 64 entries), hides the sentence that
  promised a repair, and keeps "Review repair" disabled.

Why not a compact summary form or a lower bound. A compact form would change what the person is shown
for the same approval and needs its own review of "exact". Lowering `MaxStaleEntries` to a value that is
always reachable (far fewer than 48 at 128-character names) would refuse repairs of ordinary drift that fits. The
bound that is reachable depends on name lengths, so the diagnosis computes it instead of guessing.
Nothing is truncated or abbreviated: a removal the person was not shown is never confirmed, and nothing
is written when the review does not fit.

Tests. `internal/harnessconfig/repair_test.go`: `TestARepairTooLongToShowInFullIsNotOfferedAndSaysWhy`
(64 entries of 128 characters: diagnosis truncated, prepare refused with the repair's sentence, no
"Split", profile unchanged), `TestTheDiagnosisOffersARepairExactlyWhenItCanBePrepared` (counts 1..64 x
name lengths 8..128: offered if and only if prepared, and a prepared review lists every entry).
`internal/app/harness_stale_session_repair_test.go`:
`TestASessionRepairTooLongToShowInFullIsRefusedWithoutAdviceToSplitIt`. Frontend: `HarnessDialogs.test.tsx`
"a repair too long to show in full is explained and not offered".

## R1a-3: Close on the Result limits tab abandoned a waiting apply

Cause. Only the dialog's `onOpenChange` consulted `busy`; the limits tab's Close called `onClose`
directly, and the limits tab's own controller was not consulted at all.

Choice. One rule, `closeBlocked({ editor, repair, limits })` in `workspaceHarnessModel.ts`, used by the
corner button and Escape (`onOpenChange`), the Tool groups Close, and the Result limits Close
(`LimitsTabClose`). `WorkspaceBudgetsPanel` reports `onPendingChange` (prepare or apply, not a read), so
a limits apply that waits for confirmation also blocks every way out. Cancel in the repair view already
used the repair's own `busy`.

Tests. `workspaceHarnessModel.test.ts` (each writer and phase blocks; idle does not),
`HarnessDialogs.test.tsx` (`LimitsTabClose` is disabled when blocked). End to end in a real browser, see
below.

## R1a-4 (F-D): the action row hung 16 px below the card

Cause. `components/ui/dialog.tsx` gives the body `max-height: inherit` and `shrink-0`; the card has
`max-h-[85vh]` and `p-4`, so a body that reaches the limit overflows the card's padding by its top padding.

Choice. The dialog card adds `[&>[data-slot=dialog-body]]:shrink` (Tailwind class, no inline style; the
same arbitrary variant `CellInspector` already uses, so the stylesheet gains no rule). The body then shrinks
into the card's content box and the scroll region absorbs the difference. The shared component is not
touched, so other dialogs are unchanged. Not applied to other dialogs that have the same hang; that is a
separate, shared change.

Test. `HarnessDialogs.test.tsx` checks the class on the card; the geometry is measured in a browser below.

## Browser check (headless Chrome 156 via `--dump-dom`, not the native window)

Method as in the R1a review's "Layout check": scratch Vite config and a stubbed `AppService` serving the
real `WorkspaceHarnessDialog` (scratch files removed, not committed), `getBoundingClientRect`,
`elementFromPoint`. The stub is not the real bindings.

| Measurement | Before (`6ba80806`) | After |
| --- | --- | --- |
| actions bottom minus card bottom, 64 entries, 1080 x 700 | +16 | -16 (inside the padding) |
| the same at 644 px and 424 px viewport heights, 1 and 64 entries, long names | +16 (pre-existing, per the review) | -16 |
| limits-tab Close while a repair apply waits: `disabled`, close calls after a click | false, 0 -> 1 | true, 0 -> 0 |
| 64 entries, `trunc=1` diagnosis: "Review repair" | n/a | disabled, explanation shown above the list |
| last list item reachable by scrolling, buttons on screen | yes | yes (unchanged) |

Not changed: at windows far below the app's minimum (700 x 257 and 320 x 304 CSS px) the scroll region
collapses to zero height and the row is 4 px low, as the review noted for those sizes.

## Not verified

The native Wails window, OS focus, and the real bindings (AP3). The browser run used a stub. The session
repair's too-long case is tested through the composed app but its Revoke dialog is not rendered in a
browser.
