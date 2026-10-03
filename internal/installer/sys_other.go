//go:build !windows

package installer

func setRunAtLogin(name, command string) error { return nil }

func addToUserPath(dir string) (bool, error) { return false, nil }
