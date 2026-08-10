//go:build windows

package runner

import (
	"os/exec"
	"strconv"
)

func prepareProcess(*exec.Cmd) {}

func terminateProcessTree(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return exec.Command("taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run()
}
