//go:build !windows

package installer

func setRunAtLogin(name, command string) error { return nil }

func addToUserPath(dir string) (bool, error) { return false, nil }

func removeFromUserPath(dir string) error { return nil }

// removeWhenParentExits isn't needed: a running program's files can be
// deleted here.
func removeWhenParentExits(dir string) error { return nil }
