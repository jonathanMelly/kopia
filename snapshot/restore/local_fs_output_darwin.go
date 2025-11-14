package restore

import (
	"os"
	"time"
	"unsafe"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

func symlinkChown(path string, uid, gid int) error {
	//nolint:wrapcheck
	return unix.Lchown(path, uid, gid)
}

func symlinkChmod(path string, mode os.FileMode) error {
	//nolint:wrapcheck
	return unix.Fchmodat(unix.AT_FDCWD, path, uint32(mode), unix.AT_SYMLINK_NOFOLLOW)
}

func symlinkChtimes(linkPath string, btime, atime, mtime time.Time) error {
	// macOS Lutimes only supports atime and mtime, birth time cannot be set on symlinks
	//nolint:wrapcheck
	return unix.Lutimes(linkPath, []unix.Timeval{
		unix.NsecToTimeval(atime.UnixNano()),
		unix.NsecToTimeval(mtime.UnixNano()),
	})
}

func chtimes(path string, btime, atime, mtime time.Time) error {
	return setFileTimes(path, btime, atime, mtime, false)
}

// ChtimesExact is exported for testing purposes - sets times exactly as provided.
func ChtimesExact(path string, btime, atime, mtime time.Time) error {
	return setFileTimes(path, btime, atime, mtime, true)
}

// setFileTimes sets the birth, access, and modification times for a file on macOS.
func setFileTimes(path string, btime, atime, mtime time.Time, setBirthTimeIfZero bool) error {
	// First set atime and mtime using standard os.Chtimes
	if err := os.Chtimes(path, atime, mtime); err != nil {
		return errors.Wrap(err, "unable to set atime/mtime")
	}

	// Set birth time if appropriate
	if setBirthTimeIfZero || !btime.IsZero() {
		if err := setBirthTime(path, btime); err != nil {
			// Don't fail if we can't set birth time, just continue
			// Birth time setting is best-effort on macOS
			return nil
		}
	}

	return nil
}

// macOS setattrlist structures and constants
const (
	ATTR_BIT_MAP_COUNT = 5
	ATTR_CMN_CRTIME    = 0x00000200
)

type attrlist struct {
	bitmapcount uint16
	reserved    uint16
	commonattr  uint32
	volattr     uint32
	dirattr     uint32
	fileattr    uint32
	forkattr    uint32
}

type attrbuf struct {
	length uint32
	crtime unix.Timespec
}

func setBirthTime(path string, btime time.Time) error {
	// Prepare the attribute list to specify we're setting creation time
	attrs := attrlist{
		bitmapcount: ATTR_BIT_MAP_COUNT,
		commonattr:  ATTR_CMN_CRTIME,
	}

	// Prepare the attribute buffer with the new creation time
	buf := attrbuf{
		length: uint32(unsafe.Sizeof(attrbuf{})),
		crtime: unix.NsecToTimespec(btime.UnixNano()),
	}

	// Call setattrlist
	pathPtr, err := unix.BytePtrFromString(path)
	if err != nil {
		return errors.Wrap(err, "unable to convert path to C string")
	}

	_, _, errno := unix.Syscall6(
		unix.SYS_SETATTRLIST,
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&attrs)),
		uintptr(unsafe.Pointer(&buf)),
		unsafe.Sizeof(buf),
		0, // options
		0,
	)

	if errno != 0 {
		return errors.Wrapf(errno, "setattrlist failed for %s", path)
	}

	return nil
}
