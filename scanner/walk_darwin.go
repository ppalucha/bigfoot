//go:build darwin

package scanner

import "syscall"

// sfFirmlink is the BSD st_flags bit set on APFS firmlinks (macOS-specific).
// Firmlinks (e.g. /Users → /System/Volumes/Data/Users) appear as regular
// directories and share the same Dev as the root APFS container, so the
// cross-device check alone cannot exclude them.
const sfFirmlink = 0x00800000

func isFirmlink(stat *syscall.Stat_t) bool {
	return stat.Flags&sfFirmlink != 0
}
