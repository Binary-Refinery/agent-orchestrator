//go:build !windows

package backbone

import "os"

func linked(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }
