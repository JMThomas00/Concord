//go:build windows || cgo

package client

import "github.com/sqweek/dialog"

var errSaveDialogCancelled = dialog.ErrCancelled

func promptSavePath(filename string) (string, error) {
	return dialog.File().SetStartFile(filename).Title("Save " + filename + " as").Save()
}
