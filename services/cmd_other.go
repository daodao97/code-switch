//go:build !windows

package services

import "os/exec"

// hideWindowCmd 在非 Windows 平台创建 exec.Cmd（无隐藏窗口逻辑）
func hideWindowCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
