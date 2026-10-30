# Architecture rules (enforced by `go test ./internal/arch/`)

The codebase is meant to grow a harness core without turning into one tangle. The rules below are Go tests, so
breaking one fails the build; none is advice. A failure names the file and the rule. To allow something new, edit
the table in the test **in the same change and say why**: widening a rule is a design decision, not a fix.

```text
React/Wails · native shell · CLI · MCP client      clients
                 ↓
    the adapter for each: root package (Wails shell), internal/bridge, internal/mcp
                 ↓
    the application API: internal/bridge methods over internal/app.Container
                 ↓
    harness · tools · policy · domain services     internal/*
                 ↓
    storage · OS · external APIs
```

Everything above the application API can be replaced (another window toolkit, no window, a CLI, an HTTP host)
without touching the layers below, as long as nothing below imports upward.

## What is enforced

| Rule | Test | File |
| --- | --- | --- |
| Package dependency direction, no import cycles | `TestLayering`, `TestNoImportCycles` | `arch_test.go` |
| Core packages (`harness`, `harnessconfig`, `agentwork`, `interaction`, `identity`, `permissions`, `audit`, `capabilities`, `usagemetrics`, `preferences`, `apperrors`) import only a closed list of in-module packages | `TestCorePackagesImportOnlyWhatTheyMay` | `core_layering_test.go` |
| Who may import `harness`, `harnessconfig`, `agentwork`, `bridge`, `mcp`, `telegramcontrol` | `TestCorePackagesAreImportedOnlyByWhoMay` | `core_layering_test.go` |
| `harness`, `harnessconfig`, `agentwork` use the standard library only | `TestHarnessCoreUsesOnlyTheStandardLibraryAndItself` | `core_layering_test.go` |
| **Only the desktop shell (root package, `build/ios`) imports Wails** - not `internal/`, not `cmd/`, not tests | `TestOnlyTheDesktopShellImportsTheWindowToolkit` | `adapters_test.go` |
| Policy, ledger, lock and managed-work tables are touched only by their owners | `TestProtectedTablesAreTouchedOnlyByTheirOwners` | `tables_test.go` |
| No `os.Exit`/`log.Fatal` outside `main`; `panic` only for constructor invariants; no `time.Sleep` in production code | `TestOnlyMainEndsTheProcess`, `TestPanicIsOnlyForConstructorInvariants`, `TestProductionCodeDoesNotSleep` | `source_rules_test.go` |
| A `context.WithoutCancel` carries a deadline in the same function | `TestDetachedContextsCarryADeadline` | `source_rules_test.go` |
| `AGENTS.md` ≤ 10 KiB, its links resolve, every reference guide is linked | `TestAgentsGuideStaysShortAndItsLinksResolve`, `TestEveryReferenceGuideIsLinkedFromAgents` | `repo_rules_test.go` |
| Every package has a `// Package x ...` comment | `TestEveryPackageHasADocComment` | `repo_rules_test.go` |
| A production file stays under 1200 lines | `TestProductionFilesDoNotGrowPastTheirCaps` | `repo_rules_test.go` |

## Ratchets

Some rules already had exceptions when they were written. Those are listed in the test (`unboundedDetached`,
`packagesWithoutDoc`, `fileLineCaps`, `guidesNotYetLinked`) and behave as ratchets: a new exception fails, and an
exception that no longer applies fails until it is removed, so the lists only shrink. Do not add to them to make a
change pass; fix the cause, or if the exception is real, say why next to the entry.

## Hosting the Go side without Wails

The window toolkit now lives in the root package only. The bridge reaches the native dialogs and the
"is this the server build" question through `bridge.NativeHost`, which the shell passes to
`bridge.NewAppServiceWithHost`; a nil field means "not available here". What remains for a host with no Wails at
all is a transport adapter for the application API (the methods of `bridge.AppService`), plus its own entry point:
the Wails server build already exposes them over loopback HTTP, and `cmd/devkit-mcp` is an MCP adapter over the same
container. Keep new application behaviour in `internal/app` and the domain packages, and new transport concerns in
an adapter; do not put either in the other.
