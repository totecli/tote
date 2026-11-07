package plugin

import (
	"io/fs"
	"syscall"
)

// statID is the identity of a file for the fingerprint cache: device, inode, size, modification
// and change times in nanoseconds, owner, mode and link count. The change time moves on every
// chmod, chown, rename and write, so a file restored to its old size and modification time still
// differs.
type statID struct {
	Dev     uint64
	Ino     uint64
	Size    int64
	MtimeNs int64
	CtimeNs int64
	UID     uint32
	Mode    fs.FileMode
	Nlink   uint64
}

// statIDOf reads the identity of a file from the result of Lstat or Stat.
//
// Parameters:
//   - info: file information from the operating system
//
// Returns the identity and true, or the zero value and false when the system data is missing.
func statIDOf(info fs.FileInfo) (statID, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return statID{}, false
	}

	return statID{
		Dev:     uint64(st.Dev),
		Ino:     st.Ino,
		Size:    info.Size(),
		MtimeNs: st.Mtimespec.Nano(),
		CtimeNs: st.Ctimespec.Nano(),
		UID:     st.Uid,
		Mode:    info.Mode(),
		Nlink:   uint64(st.Nlink),
	}, true
}
