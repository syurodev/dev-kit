### Prompt attachments (`internal/agenthub/attachment*.go`)
DevKit reads an attached file itself and sends its contents (limit 8 MiB). Text, Markdown and HTML go
as they are; images as image blocks; **`.docx` and `.xlsx` have their text extracted**
(`attachment_docs.go`, `archive/zip` + `encoding/xml`) and ride as a `text/plain` resource, capped at 1 MiB
of text with the cut said aloud. Old binary `.doc`/`.xls` are refused by name with "save it as
.docx/.xlsx". **PDFs are not read into the prompt at all** (next section). Plain text only for Office files: no
layout, images, comments, formulas (the cached result is read) or headers/footnotes; a spreadsheet date is
its serial number. A malformed file panicking a reader is a refused attachment, not a crash.

