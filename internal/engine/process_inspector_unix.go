//go:build !windows

package engine

import "syscall"

type OSProcessInspector struct{}

func (OSProcessInspector) Exists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
