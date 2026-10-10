//go:build !windows

package installer

import "os"

func isAdmin() bool { return os.Geteuid() == 0 }
