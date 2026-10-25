### PDF documents (`internal/pdfdoc`, `internal/agenthub/documents*.go`, `internal/mcp/tools_pdf.go`)
A PDF attached to a prompt is kept as a **document of the session** and read by the agent through
`pdf.*` MCP tools; the prompt carries only what it is and how to read it. Why not native: ACP's `resource`
can carry a `blob`, but claude-agent-acp 0.78.0 ignores blob resources (`promptToClaude`: "Ignore blob
resources (unsupported)") and Codex has no PDF input at all, so a PDF sent natively would vanish without an
error. Re-check when bumping either runtime.

- **Engine** (`internal/pdfdoc`, a leaf): PDFium compiled to WebAssembly under wazero via
  `klippa-app/go-pdfium` — pure Go, no cgo, nothing to install, one binary on every platform (+13 MB on the
  server-mode binary). The module gets **no file system and no network** (go-pdfium's default mounts `/`;
  `FSConfig` is overridden) and receives the document as bytes. First start compiles the module (~3.5 s),
  cached in `<data>/pdf-engine` so later launches take ~90 ms. Concurrency 2; analysis and renders are cached
  (LRU). Limits, each reported and never applied by quietly doing less: 100 MiB / 3000 pages per file, 20
  documents / 500 MiB per session, 25 pages per text read, 4 images (≤ 6 MiB total) per view, dpi 50–300,
  longest edge ≤ 2400 px, ~48 KiB of text per result (pages that do not fit are named), search covers every
  page within a time budget and returns `resumeFromPage` if it ran out.
- **Page chrome**: a web page printed to PDF carries the site around it. A line that repeats across pages
  in the same place **and** sits in the top/bottom 9 % band or in a column beside the body is chrome
  (`chrome.go`); a table header that repeats mid-page is not. Chrome is left out of `readText`/`search` by
  default, counted and sampled in the result, listed in `pdf.info`, and `includeChrome:true` keeps it. It is a
  heuristic and says so.
- **Scans**: no OCR (a tesseract/wasm engine plus language data was judged not worth the package weight; a
  model reads the page image better). `info` reports `textLayer` full/partial/none and the pages without one;
  `readText` says "No text layer" for them; `search` lists `pagesWithoutText`; `viewPages` shows them.
- **Store**: `agent_documents` (migration 40) + `<data>/agent-documents/<session>/<docN>.pdf` (0600, atomic
  write). Handle `docN` is per session and never reused; same bytes twice in a session = one document.
  `Library.Open` re-hashes the stored copy. Rows go in `DeleteSessions`' transaction, files in
  `Service.DeleteSession`, and `Sweep` at start-up removes folders of gone sessions and files with no row. The
  directory is in `dataDirSecrets`: an agent cannot reach it by path, only through the tools. Plain files in
  the data directory, like the transcript: outside the vault and sync.
- **Import** happens in `Service.Invoke` before the turn is recorded (`importDocuments`): an unreadable,
  password-protected or over-limit PDF refuses the prompt with its name and reason. An agent caller's path is
  held to its project (`resolveAttachmentPath`, symlinks resolved). The workspace context block gains a
  "PDF documents attached to this session" section, so a resumed or compacted conversation is told again.
- **Tools** (`pdf.list|info|search|readText|viewPages`, read-only, registered only when a store is wired).
  The caller's identity *is* its session (the per-session socket): an agent reads only its own documents and
  naming another session gets the same "No such document" as a missing one; a terminal caller must pass
  `sessionId`. They go through the gateway as `builtin.agenthub agenthub:read session:<id>`; audit metadata
  carries a handle, never a file name or query. They are **pre-approved** (`MCPEndpoint.PreApprovedTools` →
  Claude `allowedTools`, Codex MCP elicitation accepted): measured, Claude denies every call otherwise.
  Every result that quotes a document says it is data, not instructions.
- **Images reach the model differently per vendor — measured with the real runtimes, not read off the code**:
  Claude Code passes an MCP `image` block through untouched (byte-identical PNG), **but gives the model
  `structuredContent` instead of `content` when a result has both**, so `readText` returns `content` only.
  Codex 0.147 runs MCP tools from JavaScript inside its `exec` tool (`await tools.mcp__devkit__pdf_viewPages(…)`)
  and an image reaches the model **only if the script calls `image(result.content[i])`**; `codexNaming`
  (`devkit_prompt.go`) and the PDF guide say so, and the e2e test has a control showing an unforwarded image
  never arrives. Page pictures are dropped from the *stored* transcript row (`transcript_slim.go`; the live
  event keeps them for Claude; DevKit's Codex translator already reduces them to a byte count).
- **Verification**: `go test ./internal/pdfdoc ./internal/mcp ./internal/agenthub` (fixtures made by
  reportlab/pypdf in `internal/pdfdoc/testdata`: web-print with sidebar/header/footer and a table over pages
  2–4, scan, scan+text, AES-encrypted, damaged). `cmd/pdfe2e` runs the **real** DevKit Service, MCP socket and
  proxy, bubblewrap sandbox, claude-agent-acp + Claude Code and `codex app-server`, with only the model's HTTP
  API mocked so every request the runtime sends can be read back. Skipped unless
  `DEVKIT_PDF_E2E_CLAUDE=<dir with npm install @agentclientprotocol/claude-agent-acp@0.78.0>` and
  `DEVKIT_PDF_E2E_CODEX=<dir with npm install @openai/codex@0.147.0>` are set;
  `DEVKIT_PDF_E2E_SAMPLE=<a long real PDF>` adds the real-document tests.
  `go test ./cmd/pdfe2e/ -v -timeout 20m`.

