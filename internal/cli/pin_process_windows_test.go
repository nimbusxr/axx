//go:build windows

package cli

// Temporary: names what holds the installed axx during an upgrade on Windows.

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	rstrtmgr            = windows.NewLazySystemDLL("rstrtmgr.dll")
	rmStartSession      = rstrtmgr.NewProc("RmStartSession")
	rmRegisterResources = rstrtmgr.NewProc("RmRegisterResources")
	rmGetList           = rstrtmgr.NewProc("RmGetList")
	rmEndSession        = rstrtmgr.NewProc("RmEndSession")
)

type rmProcessInfo struct {
	PID         uint32
	StartTime   windows.Filetime
	AppName     [256]uint16
	Service     [64]uint16
	AppType     uint32
	AppStatus   uint32
	SessionID   uint32
	Restartable int32
}

func processImage(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "?"
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "?"
	}
	s := windows.UTF16ToString(buf[:n])
	return s[strings.LastIndex(s, `\`)+1:]
}

func holders(path string) []string {
	var session uint32
	key := make([]uint16, 33)
	if r, _, _ := rmStartSession.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0]))); r != 0 {
		return []string{fmt.Sprintf("start:%d", r)}
	}
	defer rmEndSession.Call(uintptr(session))
	p, _ := windows.UTF16PtrFromString(path)
	files := []*uint16{p}
	if r, _, _ := rmRegisterResources.Call(uintptr(session), 1, uintptr(unsafe.Pointer(&files[0])), 0, 0, 0, 0); r != 0 {
		return []string{fmt.Sprintf("register:%d", r)}
	}
	var needed, reasons uint32
	count := uint32(16)
	infos := make([]rmProcessInfo, count)
	if r, _, _ := rmGetList.Call(uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&infos[0])), uintptr(unsafe.Pointer(&reasons))); r != 0 {
		return []string{fmt.Sprintf("list:%d", r)}
	}
	var out []string
	for _, in := range infos[:count] {
		out = append(out, fmt.Sprintf("%s pid %d type %d svc %q", processImage(in.PID), in.PID, in.AppType, windows.UTF16ToString(in.Service[:])))
	}
	return out
}

func TestWhoHoldsTheInstalledAxx(t *testing.T) {
	release := buildRelease(t, "0.9.1")
	home, _ := aMachine(t)
	serveReleases(t, http.NewServeMux(), map[string]string{"0.9.1": release})
	exe := installedAxx(t, home)
	start := time.Now()
	type span struct{ first, last time.Duration }
	seen := map[string]*span{}
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(25 * time.Millisecond):
			}
			at := time.Since(start)
			for _, h := range holders(exe) {
				mu.Lock()
				if s := seen[h]; s != nil {
					s.last = at
				} else {
					seen[h] = &span{at, at}
				}
				mu.Unlock()
			}
		}
	}()
	stdout, stderr, code := runInstalled(t, exe, projectNaming(t, "0.9.0"), "", "upgrade")
	took := time.Since(start)
	close(done)
	mu.Lock()
	defer mu.Unlock()
	var lines []string
	for h, s := range seen {
		lines = append(lines, fmt.Sprintf("%s: %v to %v", h, s.first.Round(time.Millisecond), s.last.Round(time.Millisecond)))
	}
	sort.Strings(lines)
	problem := ""
	for _, l := range strings.Split(stdout+stderr, "\n") {
		if strings.Contains(l, "cannot replace") {
			problem = l
		}
	}
	t.Skipf("exit %d in %v, problem %q, holders:\n%s", code, took.Round(time.Millisecond), problem, strings.Join(lines, "\n"))
}
