package plugin

import "syscall"

// startToken identifies one incarnation of a process id. There is no /proc on this system, so the
// token is unknown and a recorded session is trusted only while its leader is the session
// leader.
//
// Parameters:
//   - pid: process id, unused
func startToken(_ int) string { return "-" }

// sessionMembers lists the processes of a session. This system cannot enumerate them without
// /proc, so the recorded process group is all that is known.
//
// Parameters:
//   - sid: session id, unused
func sessionMembers(_ int) []int { return nil }

// sessionAlive reports whether a recorded session still exists: its leader is still the leader of
// the session, or the group has members.
//
// Parameters:
//   - sid: recorded session id, which is the pid of the leader
//   - token: start token recorded with it, unused
func sessionAlive(sid int, _ string) bool {
	if got, err := getsid(sid); err == nil {
		return got == sid
	}

	return syscall.Kill(-sid, 0) == nil
}

// getsid returns the session id of a process.
//
// Parameters:
//   - pid: process id, 0 for the caller
func getsid(pid int) (int, error) {
	r, _, errno := syscall.Syscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno != 0 {
		return 0, errno
	}

	return int(r), nil
}
