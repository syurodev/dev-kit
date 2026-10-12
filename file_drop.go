package main

import (
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The composer is the only drop target (data-file-drop-target in the frontend);
// the webview already ignores a drop anywhere else. Which composer — the Agent
// Hub tab and the companion rail can both be mounted — travels in this
// attribute, so each can tell the drop is its own.
const composerDropAttr = "data-composer-id"

// filesDroppedEvent is the app event a drop onto a composer becomes.
const filesDroppedEvent = "agenthub:filesDropped"

// droppedFilesPayload turns a native drop into the event the composer reads.
//
// Only regular files are offered: a folder cannot be attached (DevKit reads an
// attachment's bytes), so it is counted in `skipped` and the composer says so
// rather than silently attaching nothing. The paths are the person's own drag,
// the same trust as the native picker's answer; what may actually be sent is
// still decided when the prompt is built (agenthub/attachment.go).
func droppedFilesPayload(files []string, target *application.DropTargetDetails) (map[string]any, bool) {
	if target == nil {
		return nil, false
	}
	composer := target.Attributes[composerDropAttr]
	if composer == "" {
		return nil, false
	}
	paths := make([]string, 0, len(files))
	skipped := 0
	for _, file := range files {
		if !filepath.IsAbs(file) {
			skipped++
			continue
		}
		info, err := os.Stat(file)
		if err != nil || !info.Mode().IsRegular() {
			skipped++
			continue
		}
		paths = append(paths, file)
	}
	if len(paths) == 0 && skipped == 0 {
		return nil, false
	}
	return map[string]any{"composer": composer, "paths": paths, "skipped": skipped}, true
}
