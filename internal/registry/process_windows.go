//go:build windows

package registry

import (
	"fmt"
	"syscall"
	"unsafe"
)

const processQueryLimitedInformation = 0x1000

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	openProcess     = kernel32.NewProc("OpenProcess")
	getProcessTimes = kernel32.NewProc("GetProcessTimes")
	closeHandle     = kernel32.NewProc("CloseHandle")
)

func CurrentProcessIdentity(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	handle, _, _ := openProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return "", false
	}
	defer closeHandle.Call(handle)
	var creation, exit, kernel, user syscall.Filetime
	result, _, _ := getProcessTimes.Call(handle, uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if result == 0 {
		return "", false
	}
	start := uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
	return fmt.Sprintf("pid:%d/start:%d", pid, start), true
}

func ProcessMatches(pid int, expected string) bool {
	actual, ok := CurrentProcessIdentity(pid)
	return ok && expected != "" && actual == expected
}
