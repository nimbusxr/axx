package uia

// descendants are the processes under pid in kids (each process's children,
// by their parent's number): a child counts only when it started after its
// parent, since Windows gives a finished process's number to a new one, which
// may then look like its own ancestor's child. born is when a process
// started; a process whose start is not known counts.
func descendants(kids map[int][]int, pid int, born func(int) (int64, bool)) []int {
	seen := map[int]bool{pid: true}
	var out []int
	var add func(p int)
	add = func(p int) {
		pb, pok := born(p)
		for _, c := range kids[p] {
			if seen[c] {
				continue
			}
			if cb, cok := born(c); pok && cok && cb < pb {
				continue // an older process, whose parent's number was reused
			}
			seen[c] = true
			out = append(out, c)
			add(c)
		}
	}
	add(pid)
	return out
}
