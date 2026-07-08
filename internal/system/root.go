package system

import "os"

// IsRoot reports whether the effective UID is 0.
func IsRoot() bool { return os.Geteuid() == 0 }
