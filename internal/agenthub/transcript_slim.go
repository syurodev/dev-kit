package agenthub

import (
	"bytes"
	"encoding/json"
	"strings"
)

// A page image an agent asked for is a few hundred kilobytes, sat twice in the
// stored row (once as the result's image, once inside its raw output), and can
// be drawn again at any time from the document DevKit keeps. Kept in the
// transcript, thirty page views would be tens of megabytes of rows that exist
// only to be scrolled past — so the stored copy keeps the caption and drops the
// picture, and says it did. The live event, which carries the picture, is
// unaffected: the person watching the turn still sees what the agent saw.

// pageImageMarker is what the raw output becomes.
const pageImageMarker = "page images are not kept in the transcript: DevKit keeps the document and can draw the pages again"

// slimPageImages returns the update as it should be stored: without the page
// pictures of a tool call that fetched them. A call is one when its tool is
// pdf.viewPages (however the vendor spells it), or when the script that ran —
// Codex calls MCP tools from JavaScript in `exec` — names it. The argument is not
// changed; ok reports whether anything was removed.
func slimPageImages(u SessionUpdate) (stored SessionUpdate, ok bool) {
	if u.ToolCall == nil || !fetchesPageImages(u.ToolCall) {
		return u, false
	}
	call := *u.ToolCall
	call.Content = append([]ToolCallContent(nil), u.ToolCall.Content...)
	for i := range call.Content {
		if block := call.Content[i].Content; block != nil && block.Type == "image" && block.Data != "" {
			copied := *block
			copied.Data = ""
			call.Content[i].Content = &copied
			call.Content[i].Truncated = true
			ok = true
		}
	}
	if len(call.RawOutput) > 4096 && bytes.Contains(call.RawOutput, []byte("image")) {
		call.RawOutput, _ = json.Marshal(pageImageMarker)
		ok = true
	}
	u.ToolCall = &call
	return u, ok
}

func fetchesPageImages(call *ToolCall) bool {
	for _, name := range []string{call.Name, call.Title} {
		if strings.HasSuffix(name, "pdf_viewPages") || strings.HasSuffix(name, "pdf.viewPages") {
			return true
		}
	}
	return bytes.Contains(call.RawInput, []byte("pdf_viewPages"))
}
