package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// UpdateTask 更新任务配置
type UpdateTask struct {
	MainPID      int      `json:"main_pid"`       // 主程序 PID
	TargetExe    string   `json:"target_exe"`     // 目标可执行文件路径
	NewExePath   string   `json:"new_exe_path"`   // 新版本文件路径
	BackupPath   string   `json:"backup_path"`    // 备份路径
	CleanupPaths []string `json:"cleanup_paths"`  // 需要清理的临时文件
	TimeoutSec   int      `json:"timeout_sec"`    // 必填：等待超时（秒），由主程序动态计算
}

// isElevated 检查当前进程是否具有管理员权限
func isElevated() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

// ensureElevation 确保以管理员权限运行，如果没有则请求 UAC 提权
func ensureElevation() {
	if isElevated() {
		return // 已有管理员权限
	}

	log.Println("[UAC] 未检测到管理员权限，正在请求提权...")

	// 获取当前可执行文件路径
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("[UAC] 获取可执行文件路径失败: %v", err)
	}

	// 使用 ShellExecute 请求 UAC 提权
	verb := windows.StringToUTF16Ptr("runas")
	file := windows.StringToUTF16Ptr(exePath)
	args := windows.StringToUTF16Ptr(strings.Join(os.Args[1:], " "))
	dir := windows.StringToUTF16Ptr(filepath.Dir(exePath))

	err = windows.ShellExecute(0, verb, file, args, dir, windows.SW_SHOWNORMAL)
	if err != nil {
		log.Fatalf("[UAC] 请求管理员权限失败: %v", err)
	}

	// 当前非提权进程退出，提权后的新进程会继续执行
	os.Exit(0)
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: updater.exe <task-file>")
	}

	taskFile := os.Args[1]

	// 设置日志文件
	logPath := filepath.Join(filepath.Dir(taskFile), "update.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		// 无法创建日志文件时使用标准输出
		log.Printf("[警告] 无法创建日志文件: %v", err)
	} else {
		defer logFile.Close()
		log.SetOutput(logFile)
	}

	log.Println("========================================")
	log.Printf("CodeSwitch Updater 启动")
	log.Printf("任务文件: %s", taskFile)

	// UAC 自检：确保以管理员权限运行
	if isElevated() {
		log.Println("权限状态: 管理员")
	} else {
		log.Println("权限状态: 普通用户")
		log.Println("[UAC] 请求管理员权限...")
		ensureElevation()
		// ensureElevation 会退出当前进程，以下代码不会执行
	}

	log.Println("========================================")

	// 读取任务配置
	data, err := os.ReadFile(taskFile)
	if err != nil {
		log.Fatalf("读取任务文件失败: %v", err)
	}

	var task UpdateTask
	if err := json.Unmarshal(data, &task); err != nil {
		log.Fatalf("解析任务配置失败: %v", err)
	}

	log.Printf("任务配置:")
	log.Printf("  - MainPID: %d", task.MainPID)
	log.Printf("  - TargetExe: %s", task.TargetExe)
	log.Printf("  - NewExePath: %s", task.NewExePath)
	log.Printf("  - BackupPath: %s", task.BackupPath)
	log.Printf("  - TimeoutSec: %d", task.TimeoutSec)
	log.Printf("  - CleanupPaths: %v", task.CleanupPaths)

	// 等待主程序退出（使用任务配置的超时值，禁止硬编码）
	timeout := time.Duration(task.TimeoutSec) * time.Second
	if task.TimeoutSec <= 0 {
		timeout = 30 * time.Second // 兜底默认值（仅当任务文件异常时）
		log.Println("[警告] timeout_sec 未设置或无效，使用默认 30 秒")
	}

	log.Printf("等待主程序退出（PID=%d, 超时=%v）...", task.MainPID, timeout)
	if err := waitForProcessExit(task.MainPID, timeout); err != nil {
		log.Fatalf("等待主程序退出超时（%ds）: %v", task.TimeoutSec, err)
	}
	log.Println("主程序已退出")

	// 执行更新
	log.Println("开始执行更新操作...")
	if err := performUpdate(task); err != nil {
		log.Printf("更新失败: %v", err)
		log.Println("执行回滚操作...")
		rollback(task)
		log.Println("回滚完成，更新器退出（失败）")
		os.Exit(1)
	}

	log.Println("更新成功！")

	// 启动新版本
	log.Printf("启动新版本: %s", task.TargetExe)
	cmd := exec.Command(task.TargetExe)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Dir = filepath.Dir(task.TargetExe)
	if err := cmd.Start(); err != nil {
		log.Printf("[警告] 启动新版本失败: %v", err)
		log.Println("请手动启动应用程序")
	} else {
		log.Printf("新版本已启动 (PID=%d)", cmd.Process.Pid)
	}

	// 延迟清理临时文件
	log.Println("等待 3 秒后清理临时文件...")
	time.Sleep(3 * time.Second)

	for _, path := range task.CleanupPaths {
		if err := os.RemoveAll(path); err != nil {
			log.Printf("[警告] 清理失败: %s - %v", path, err)
		} else {
			log.Printf("已清理: %s", path)
		}
	}

	// 删除任务文件
	if err := os.Remove(taskFile); err != nil {
		log.Printf("[警告] 删除任务文件失败: %v", err)
	} else {
		log.Printf("已删除任务文件: %s", taskFile)
	}

	log.Println("========================================")
	log.Println("更新器退出（成功）")
	log.Println("========================================")
}

// waitForProcessExit 等待指定 PID 的进程退出
func waitForProcessExit(pid int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// 进程可能已经退出，OpenProcess 失败时视为进程不存在
		log.Printf("进程 %d 可能已退出: %v", pid, err)
		return nil
	}
	defer windows.CloseHandle(handle)

	event, err := windows.WaitForSingleObject(handle, uint32(timeout.Milliseconds()))
	if err != nil {
		return fmt.Errorf("等待进程失败: %w", err)
	}

	// WaitForSingleObject 返回值常量
	const (
		WAIT_OBJECT_0 = 0x00000000
		WAIT_TIMEOUT  = 0x00000102
	)

	switch event {
	case WAIT_OBJECT_0:
		// 进程已退出
		return nil
	case WAIT_TIMEOUT:
		return fmt.Errorf("进程 %d 未在 %v 内退出", pid, timeout)
	default:
		return fmt.Errorf("等待进程返回未知状态: %d", event)
	}
}

// performUpdate 执行更新操作
func performUpdate(task UpdateTask) error {
	// Step 1: 验证新版本文件存在
	log.Printf("Step 1: 验证新版本文件")
	newInfo, err := os.Stat(task.NewExePath)
	if err != nil {
		return fmt.Errorf("新版本文件不存在: %w", err)
	}
	if newInfo.Size() == 0 {
		return fmt.Errorf("新版本文件大小为 0")
	}
	log.Printf("  新版本文件: %s (%d bytes)", task.NewExePath, newInfo.Size())

	// Step 2: 备份旧版本
	log.Printf("Step 2: 备份旧版本")
	log.Printf("  %s -> %s", task.TargetExe, task.BackupPath)

	// 如果备份文件已存在，先删除
	if _, err := os.Stat(task.BackupPath); err == nil {
		log.Printf("  删除已存在的备份文件...")
		if err := os.Remove(task.BackupPath); err != nil {
			return fmt.Errorf("删除旧备份失败: %w", err)
		}
	}

	if err := os.Rename(task.TargetExe, task.BackupPath); err != nil {
		return fmt.Errorf("备份旧版本失败: %w", err)
	}
	log.Println("  备份完成")

	// Step 3: 复制新版本
	log.Printf("Step 3: 复制新版本")
	log.Printf("  %s -> %s", task.NewExePath, task.TargetExe)
	if err := copyFile(task.NewExePath, task.TargetExe); err != nil {
		return fmt.Errorf("复制新版本失败: %w", err)
	}
	log.Println("  复制完成")

	// Step 4: 验证新文件
	log.Println("Step 4: 验证新版本文件")
	targetInfo, err := os.Stat(task.TargetExe)
	if err != nil {
		return fmt.Errorf("验证新版本失败: %w", err)
	}
	if targetInfo.Size() == 0 {
		return fmt.Errorf("新版本文件大小为 0，可能复制失败")
	}
	if targetInfo.Size() != newInfo.Size() {
		return fmt.Errorf("新版本文件大小不匹配: 期望 %d, 实际 %d", newInfo.Size(), targetInfo.Size())
	}
	log.Printf("  验证通过: 文件大小 = %d bytes", targetInfo.Size())

	return nil
}

// rollback 回滚更新（静默，不弹窗）
func rollback(task UpdateTask) {
	log.Println("执行回滚操作...")

	// 检查备份文件是否存在
	backupInfo, err := os.Stat(task.BackupPath)
	if err != nil {
		log.Printf("备份文件不存在，无法回滚: %v", err)
		return
	}
	log.Printf("备份文件: %s (%d bytes)", task.BackupPath, backupInfo.Size())

	// 删除可能存在的损坏新版本
	if _, err := os.Stat(task.TargetExe); err == nil {
		log.Printf("删除损坏的目标文件: %s", task.TargetExe)
		if err := os.Remove(task.TargetExe); err != nil {
			log.Printf("[警告] 删除目标文件失败: %v", err)
		}
	}

	// 恢复备份
	log.Printf("恢复备份: %s -> %s", task.BackupPath, task.TargetExe)
	if err := os.Rename(task.BackupPath, task.TargetExe); err != nil {
		log.Printf("[错误] 回滚失败: %v", err)
		log.Println("请手动将备份文件恢复为原文件名")
	} else {
		log.Println("回滚成功")
	}
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %w", err)
	}
	defer source.Close()

	dest, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer dest.Close()

	written, err := io.Copy(dest, source)
	if err != nil {
		return fmt.Errorf("复制数据失败: %w", err)
	}

	log.Printf("  已复制 %d bytes", written)
	return nil
}
