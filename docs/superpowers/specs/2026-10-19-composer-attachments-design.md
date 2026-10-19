# Composer attachments: one chip per file, in the draft

Status: implemented on `composer-attachments-redesign`.

## What is wrong today

A file reaches the Agent Hub composer three ways, and each leaves something different behind:

| How it is added | Inline chip in the field | Chip in the strip above |
| --- | --- | --- |
| `@` list, `@` button | yes (`@name` goes into the text) | yes — absolute path, cut at the end |
| dragging files in from the OS | yes | yes |
| `+ → Attach files…` | no | yes |

So the same file is drawn twice, the strip chip shows the one part of a long path nobody needs (the folder) and cuts the part they do (the name), and images and references are a third and fourth kind of chip with their own remove buttons.

Underneath, the data is split in two: the draft *text* lives in `useAgentSessionsStore.promptBySession`, while the files, images and references live in three `useState` maps of `AgentHubView` — and a second, separate set in `AgentCompanion`. Two views of one session share its text and nothing else. A chip deleted in the field has to be told to the strip by hand (`mentionKeysRef`).

## Decision

**A file, an image and a reference are chips, and a chip lives inline in the draft. There is no strip.** The lists a turn carries (`attachments`, `images`, `refs`) are read off the draft when it is sent. The draft is the only source of truth, so deleting, cutting, pasting and copying a chip can never leave the list and the field disagreeing.

Chosen with the user over "cards in a strip, `@` is only text" (it would stop `@` attaching content, which changes what an agent receives) and over "a strip for some, inline for others" (two places for one thing).

## The draft keeps its type

`promptBySession` stays `Record<string, string>`. Everything that reads or writes it as text — the Companion, `agent.attach`, slash commands, `codegraphActions`, the failed-send restore — keeps working. A chip is written into the string as a token of private-use characters:

```
U+E000  kind ":" enc(payload) "|" enc(label)  U+E001        kind = file | image | ref
```

`payload` is the absolute path, the image id, or the `devkit://` ref URI; `enc` is `encodeURIComponent`, so `|`, `:` and the delimiters never occur inside. Private-use characters are not typed or pasted in practice, and the serializer replaces any that appear in text with U+FFFD, so a token can only come from a chip.

`modules/agenthub/composerDraft.ts` (pure) owns the format:

- `chipToken`, `parseDraft(draft) → { text, chips }` — `text` is what the agent reads: each token becomes `@label`, exactly what `@` put in the text before, so nothing the agent receives changes.
- `turnFromDraft(draft, images) → { text, attachments, images, refs }` — unique, in order. An image id with no registered image is dropped (it cannot be sent).
- `draftHasContent`, `appendChips` (for a caller that has no editor: `agent.attach` arriving while no composer is mounted).

## Editor

- A new Plate element `attachment` (void, inline; `kind`, `key` = payload, `value` = label) replaces the `mention` plugin. The `@` list still reads the text before the caret itself (no Plate combobox — see `mentionQuery.ts`).
- `serializeComposerValue` writes tokens, `deserializeComposerValue` is its exact inverse. `parseDraft` is the one place that turns tokens into the agent's text.
- **Copy and paste.** The clipboard's `text/plain` is the prompt as the agent reads it (`@name`), so pasting into any other app, or into a note, reads sensibly. The editor's own fragment (`application/x-slate-fragment`) still carries the chips, so copy-paste between composers — Hub, Companion, another session — brings the files along. An image chip pasted into another composer needs its bytes: images live in an in-memory registry (`stores/composerImages.ts`) keyed by id, not per session, so the id means the same everywhere.
- Every way of adding something inserts a chip at the caret (the end when the field has no caret): `@` list, the `@` button, `+ → Attach files…`, an OS drop, a pasted or dropped image, and — as `appendChips` on the string — a reference handed over by another module.

## Props

The composer loses `attachedFiles`, `onRemoveAttachment`, `imageAttachments`, `onRemoveImage`, `refs`, `onRemoveRef`, `onSelectContext`, `onPasteImage`, `onDropImage`; it makes images itself. `onBrowseContext` returns the paths the person picked instead of attaching them. `AgentHubView` and `AgentCompanion` lose their `attachments` / `images` / `refs` maps.

## Turn pipeline

Unchanged: `TurnInput`, `queuedPrompt`, `retryTurnFor`, `shouldSteer` still take arrays. They are filled from `turnFromDraft`.

- A turn that fails before it starts puts the **draft string** back into an empty field (it used to put back only the words), so the chips return with it.
- A queued prompt remembers the `draft` it was sent from, so "edit queued" restores the chips in place; its images go back into the registry.
- The new-session draft (`draft.attachments` / `draft.images`) goes the same way: its prompt string holds the chips.

## The chip

One component for the three kinds (`AttachmentChip`): 24px, an icon (file type, a 16px thumbnail, a link glyph), the name.

- **Long names keep their end.** The name is split into a head that truncates and a tail that never does (the last 8 characters of the stem and the extension): `SAP-Final Architecture — Galax…0054)-191026.pdf`. The folder is not in the chip.
- **Hover card** (rests, as `ReferencePreview` does): the full name, the full path, size, what DevKit will do with it ("PDF: kept as a document the agent reads with the pdf tools", "Word: its text is extracted", "image"), and why it cannot be sent when it cannot (over 8 MiB, an old `.doc`/`.xls`, not text, missing). An image shows its picture.
- A file that cannot be sent has a red icon and the reason in the card; sending still goes through, and Go's own refusal still tells the person (the chip is not a gate).
- Removing: Backspace/Delete like any atomic node, and a × on hover/focus with an accessible name.

## Go: describing a file

`AgentHubDescribeFiles(paths) → []AttachmentInfo{path, name, size, kind, problem}` reads only what `attachmentBlock` reads to decide: stat, extension, and the first bytes for "is it text". The rules are factored out of `attachmentBlock` into `describeAttachment`, so the card and the refusal cannot disagree. UI only: it stats whatever path the window names, like `AgentHubChooseFiles`, and is not an MCP tool.

## Not in this change

- Transcript rendering of what was sent (`AgentFileChip` in tool calls and messages).
- Blocking Send on a chip with a problem.
- Reordering chips by drag, and attaching folders.

## Found on the way

- **Pasting a prompt mangled it.** The composer kit carried `MarkdownPlugin`, whose paste reader treated a pasted prompt as a document: `# Title\n\n- a\n- b\n\nplain **bold** text` became `Title\nab\nplain bold text`, and a table became `ab12`. The kit has no heading, list or table plugins, so those nodes were dropped on the way out. The reader is gone; a paste goes in through `deserializeComposerValue`, so it comes back exactly as written (`attachments.test.ts`).
- Two composers showing one session (the Hub and the rail) used to share only its text; their files, images and references were separate state. They now share everything, because everything is in the draft.

## Known limits

- The image registry is in memory: a draft restored after a relaunch has its words and files, and an image chip whose bytes are gone is dropped when the turn is built. A draft abandoned with image chips in it leaves its entries until the window closes.
- Copy between composers carries chips through the editor's own fragment; text copied from a chip into a plain-text app reads `@name`, and pasting that back is text, not a chip.
