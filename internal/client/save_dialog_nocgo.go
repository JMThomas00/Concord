//go:build !windows && !cgo

package client

import "errors"

// sqweek/dialog has no pure-Go backend outside Windows (GTK3 via cgo on
// Linux, Cocoa via cgo on macOS), so CGO-free builds -- `make dist` -- report
// the dialog as unavailable and /download falls back to the Downloads folder.

var errSaveDialogCancelled = errors.New("save dialog cancelled")

func promptSavePath(string) (string, error) {
	return "", errors.New("native save dialog requires a cgo build on this platform")
}
