//go:build windows

package engine

import (
	"os"
)

type OSProcessInspector struct{}

func (OSProcessInspector) Exists(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(os.Signal(nil)) == nil
}
