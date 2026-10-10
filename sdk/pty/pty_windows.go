//go:build windows

package pty

import "errors"

func startProcess(Options, string, int, int) (process, error) {
	return nil, errors.New("running programs in a Concord channel isn't supported on Windows servers yet")
}
