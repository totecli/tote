package plugin

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// maxProcScan bounds the number of /proc entries one scan reads.
const maxProcScan = 1 << 17

// procStat reads the session id and the start time of a process from /proc/<pid>/stat.
//
// Parameters:
//   - pid: process id
//
// Returns the session id, the start time in clock ticks since boot as a string, and false when
// the process does not exist or its line cannot be read.
func procStat(pid int) (session int, start string, ok bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, "", false
	}

	// The command name is in parentheses and may hold spaces and parentheses: the fields start
	// after the last closing one. The state is field 3, the session field 6, the start time 22.
	text := string(data)

	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return 0, "", false
	}

	fields := strings.Fields(text[end+1:])
	if len(fields) < 20 {
		return 0, "", false
	}

	sid, err := strconv.Atoi(fields[3])
	if err != nil {
		return 0, "", false
	}

	return sid, fields[19], true
}

// startToken identifies one incarnation of a process id: its start time.
//
// Parameters:
//   - pid: process id
//
// Returns the token, or "-" when it cannot be read.
func startToken(pid int) string {
	if _, start, ok := procStat(pid); ok {
		return start
	}

	return "-"
}

// sessionMembers lists the processes whose session id is sid.
//
// Parameters:
//   - sid: session id
func sessionMembers(sid int) []int {
	f, err := os.Open("/proc")
	if err != nil {
		return nil
	}

	defer func() { _ = f.Close() }()

	names, _ := f.Readdirnames(maxProcScan)

	var out []int

	for _, n := range names {
		pid, err := strconv.Atoi(n)
		if err != nil || pid <= 1 {
			continue
		}

		if s, _, ok := procStat(pid); ok && s == sid {
			out = append(out, pid)
		}
	}

	return out
}

// sessionAlive reports whether a recorded session still exists: its leader is the same process,
// or the leader is gone and other processes stayed in the session. A leader whose id was reused
// by another process means the old session has ended, since the system does not reuse an id that
// a session still holds.
//
// Parameters:
//   - sid: recorded session id, which is the pid of the leader
//   - token: start token recorded with it
func sessionAlive(sid int, token string) bool {
	if session, start, ok := procStat(sid); ok {
		if token == "-" {
			return session == sid
		}

		return start == token
	}

	return len(sessionMembers(sid)) > 0
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
