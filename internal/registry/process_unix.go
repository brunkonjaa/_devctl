//go:build !windows

package registry

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"syscall"
)

func CurrentProcessIdentity(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return "", false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return "", false
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if closing := bytes.LastIndexByte(data, ')'); closing >= 0 && closing+2 < len(data) {
			fields := strings.Fields(string(data[closing+2:]))
			if len(fields) > 19 {
				return fmt.Sprintf("pid:%d/start:%s", pid, fields[19]), true
			}
		}
	}
	return fmt.Sprintf("pid:%d", pid), true
}

func ProcessMatches(pid int, expected string) bool {
	actual, ok := CurrentProcessIdentity(pid)
	return ok && expected != "" && actual == expected
}
