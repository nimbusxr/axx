package uia

// descendants are the processes under pid in kids (each process's children,
// by their parent's number): a child counts only when it started after the
// nearest of its forebears whose start is known (pid's: since, when the
// caller knew it as pid ran, else born's), since Windows gives a finished
// process's number to a new one, and an older process whose parent's number
// pid got looks like pid's child. born is when a process started; a process
// whose start is not known (it ended, or is not the caller's to open) does
// not count: nothing tells it from such an older one, and there is nothing
// of it to stop. Neither do any when pid's start is not known either.
func descendants(kids map[int][]int, pid int, since int64, born func(int) (int64, bool)) []int {
	if since == 0 {
		b, ok := born(pid)
		if !ok {
			return nil
		}
		since = b
	}
	seen := map[int]bool{pid: true}
	var out []int
	var add func(p int, after int64)
	add = func(p int, after int64) {
		for _, c := range kids[p] {
			if seen[c] {
				continue
			}
			cb, ok := born(c)
			if !ok || cb < after {
				continue // an older process, whose parent's number was reused, or one gone
			}
			seen[c] = true
			out = append(out, c)
			add(c, cb)
		}
	}
	add(pid, since)
	return out
}
