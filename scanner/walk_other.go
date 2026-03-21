//go:build !darwin

package scanner

import "syscall"

func isFirmlink(_ *syscall.Stat_t) bool {
	return false
}
