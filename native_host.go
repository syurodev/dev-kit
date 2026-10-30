package main

import (
	"errors"
	"strings"

	"devkit/internal/bridge"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// nativeHost is the bridge's window toolkit: the dialogs and the server-mode
// question, answered by Wails. Everything under internal/ is toolkit-free; this
// file and the other root-package shell files are where the toolkit lives.
func nativeHost() bridge.NativeHost {
	return bridge.NativeHost{
		SaveFile:      nativeSaveFileDialog,
		OpenFiles:     nativeOpenFilesDialog,
		OpenDirectory: nativeOpenDirectoryDialog,
		IsServer:      func() bool { return application.System.IsServer() },
	}
}

var errNativeDialogUnavailable = errors.New("dialog service unavailable")

func nativeSaveFileDialog(suggestedName string) (string, error) {
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return "", errNativeDialogUnavailable
	}
	dialog := app.Dialog.SaveFile().SetFilename(suggestedName)
	lower := strings.ToLower(suggestedName)
	if strings.HasSuffix(lower, ".csv") {
		dialog.AddFilter("CSV Files (*.csv)", "*.csv")
	} else if strings.HasSuffix(lower, ".json") {
		dialog.AddFilter("JSON Files (*.json)", "*.json")
	}
	return dialog.PromptForSingleSelection()
}

func nativeOpenDirectoryDialog(title, startIn string) (string, error) {
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return "", errNativeDialogUnavailable
	}
	dialog := app.Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true).
		SetTitle(title)
	if startIn != "" {
		dialog.SetDirectory(startIn)
	}
	return dialog.PromptForSingleSelection()
}

func nativeOpenFilesDialog(title, startIn string) ([]string, error) {
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return nil, errNativeDialogUnavailable
	}
	dialog := app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle(title)
	if startIn != "" {
		dialog.SetDirectory(startIn)
	}
	// Cancel comes back as no paths and no error; AgentHubChooseFiles turns
	// that into an empty list.
	return dialog.PromptForMultipleSelection()
}
