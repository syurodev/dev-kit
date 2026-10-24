# Conversation-centred shell

Task: C1 / OPT-484f67. Written 2026-10-25. Status: **concrete design for review; shell code is not implemented here**.

The person chose the direction on 2026-10-24: conversation is DevKit's home surface and modules open beside it as inspectors. This document specifies that direction and proposes defaults for the remaining navigation choices. It does not reopen the decision to keep Agent Hub as one peer module.

## Existing foundations

- `frontend/src/workbench/Workbench.tsx`, `stores/workbenchTabs.ts`: document groups, split, focus, tab persistence.
- `frontend/src/workbench/intents.ts`: `EntityRef`, navigation intents with `replace|beside`, and attach intents.
- `frontend/src/modules/agenthub/documents.ts`: stable session tab identity and shared helpers.
- C2 split: reuse the session hooks/controller/surface; do not mount two controllers for one visible conversation or copy lifecycle state into a new home store.
- [Module-shell integration](../../design/module-shell-integration.md): shell owns rail and document strip. This remains the integration contract.

## Product layout

Wide windows show a primary conversation group and optional workbench groups beside it. The conversation uses the existing session surface, composer, permissions, draft and transcript. Inspectors show real owning-module documents, not screenshots of a second app. Notes, files, tasks, SQL and group Team/Timeline can be opened beside the conversation through the same entity-open path.

The shell owns group creation, resizing and focus. There is one shared tab strip per workbench group and one shared sidebar column. No module renders an additional fixed-width aside or maintains a private list of open documents.

Proposed defaults requiring review:

| Situation | Proposed behavior |
| --- | --- |
| Fresh window with no restored session | Show an empty conversation draft scoped to the selected workspace, with recent sessions/work groups available. Do not spawn a vendor or send a prompt. |
| Restore existing window | Restore document groups and the last focused document by stable IDs. Restore drafts according to their existing privacy/lifecycle rules; never auto-send. |
| Open resource from conversation | Default to an inspector group beside it; focus the selected resource without closing the conversation. The user can choose Open in current group. |
| Open another session | Use the session document helper and existing tab identity. Never reuse an inspector's entity ID as a session ID. |
| Narrow window | Keep the same workbench documents; show the focused group with a visible route back to the conversation and other groups through shell navigation. Do not create a second responsive tab store. |
| Close the last inspector | Conversation stays. Closing a conversation tab does not cancel its process or pause a managed work group. |
| Module switcher | Remains a way to open/browse module-owned content and settings; navigation does not silently move a document between groups. |

“Beside” is the preferred placement, not an instruction to steal focus when a background task finishes. Notifications offer an Open action; they never navigate automatically.

## Entity contract

Extend the existing compile-time module registrations with a small owner contract for each supported entity kind. Prefer composing current intents and palette search before adding a second registry. A proposed capability record says whether the owner can resolve/open/attach/search that kind. Components, loaders and routes remain locally registered code; backend text cannot name a component to import.

`EntityRef` stays the shared identity: owning module/kind/id plus workspace/folder/project qualifiers where needed. Labels are display hints, not identity. A resolved entity has a current label, availability and a stable document target; opening calls the owning module's `documents.ts` helper. The host rechecks permissions when an action reads/writes data. A navigation intent never grants resource access.

| Operation | Contract |
| --- | --- |
| Resolve | Return current identity/label/availability, or unavailable with a readable reason. Do not guess another document. |
| Open | Use the owning helper with explicit placement. Reopening focuses an existing matching document. |
| Attach | Put references in the existing composer draft; no automatic resource-body dump or prompt send. Resolve scope when the agent later reads. |
| Search | Reuse bounded, owner-scoped palette/search results; search of one module does not enumerate unrelated workspaces. |

Transcript references, tool results, inspector links and palette results must use these same operations. An unavailable handler hides/disables the action with a reason; no dead buttons. Sensitive bodies and credentials are never persisted in params or navigation preferences.

## Inspector behavior

Inspectors are real documents mounted in workbench groups. Their selection is derived from the focused group's active document, not a module-global selected ID. Draft/resource lifetimes stay in their owning service/store; hiding or unmounting a surface does not stop a query, SSH session or agent accidentally.

Permission/question UI uses the existing interaction owner and `useModuleVisibility`: inline in the visible conversation, shell prompt elsewhere, one answerable request per request ID. An inspector cannot clone an Allow button or approve on behalf of the conversation. Changing focus does not create another waiter.

Group Team/Timeline is one entity keyed by `groupId`. Its two modes are internal shadcn Tabs in one document. The group read model and details are independent of their mount position, so the temporary `agenthub.workgroups` document proposed in the team design can later open beside the conversation without rewriting orchestration or selection.

## Migration and compatibility

1. Complete/reuse C2 seams without changing conversation behavior.
2. Add shared entity resolution/open adapters and tests; retain existing intent names and helpers as compatibility entry points.
3. Introduce the conversation-home shell behind a local rollout switch, preserving current workbench IDs and recently-closed semantics.
4. Convert module launchers to open owner documents in inspector placement; one module per independently reviewable change.
5. Remove only superseded navigation adapters after restore/migration tests prove old tab state still opens correctly. No forced close of running resources.

C0's Ubuntu/WebKitGTK baseline informs performance validation. The new home is not evidence of better rendering performance by itself.

## Acceptance criteria for C3–C6 implementation

- Fresh startup/restore does not start or send work without an explicit action.
- Session opened from rail, notification, palette or inspector resolves to one stable tab and one session controller.
- Split groups, sidebar highlight, pin/close/reopen and title changes follow the existing shell contract.
- An entity's open and attach paths agree on identity/scope, including a file with the same name in two workspace folders.
- Permission prompts remain correctly scoped during tab/module/window changes; hidden documents do not open their own dialogs.
- Closing a deleted entity uses `closeTab(id, { remember: false })`; missing restore IDs show unavailable, not another entity.
- Wide/narrow, light/dark, keyboard navigation and reduced motion are covered in gallery and regression tests.
- UI profiling includes the native Ubuntu target; browser/gallery evidence is labeled as such.

No C3–C6 code or global navigation change is included in this design deliverable. The proposed fresh-window/narrow-window defaults above and rollout switch should be accepted before implementation.
