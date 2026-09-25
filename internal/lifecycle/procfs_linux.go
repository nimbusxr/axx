package lifecycle

import (
	"bytes"
	"os"
	"strconv"
)

// groupHasLiveMember reports whether a process group has a member that is
// not a zombie. Zombies matter when axx runs as PID 1 in a container without
// an init: orphaned grandchildren are reparented to axx and never reaped, so
// kill(-pgid, 0) keeps succeeding although nothing runs any more. When /proc
// cannot be read it answers true, so callers fall back to kill's answer.
func groupHasLiveMember(pgid int) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		state, group, ok := procStat(pid)
		if ok && group == pgid && state != 'Z' && state != 'X' {
			return true
		}
	}
	return false
}

// isZombie reports whether pid is a zombie (exited, not yet reaped).
func isZombie(pid int) bool {
	state, _, ok := procStat(pid)
	return ok && state == 'Z'
}

// procStat reads the state and process group of pid from /proc/<pid>/stat:
// "pid (comm) state ppid pgrp ...", where comm may contain spaces and ")".
func procStat(pid int) (state byte, pgrp int, ok bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, 0, false
	}
	i := bytes.LastIndexByte(data, ')')
	if i < 0 {
		return 0, 0, false
	}
	f := bytes.Fields(data[i+1:])
	if len(f) < 3 || len(f[0]) != 1 {
		return 0, 0, false
	}
	pgrp, err = strconv.Atoi(string(f[2]))
	if err != nil {
		return 0, 0, false
	}
	return f[0][0], pgrp, true
}
