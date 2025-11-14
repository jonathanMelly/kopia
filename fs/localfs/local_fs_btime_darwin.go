package localfs

import (
	"syscall"
)

func platformSpecificBirthTimeFromStat(stat *syscall.Stat_t, path string) int64 {
	// macOS has Birthtimespec field
	return stat.Birthtimespec.Sec*1e9 + stat.Birthtimespec.Nsec
}
