//go:build windows

package services

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procMoveFileExW = kernel32.NewProc("MoveFileExW")
)

const (
	// MOVEFILE_REPLACE_EXISTING 允许覆盖已存在的目标文件
	MOVEFILE_REPLACE_EXISTING = 0x1
)

// atomicRename Windows 平台原子重命名
// 使用 MoveFileExW 替代 os.Rename，支持覆盖已存在的文件
// 并在遇到 ERROR_SHARING_VIOLATION 时重试
func atomicRename(src, dst string) error {
	srcPtr, err := syscall.UTF16PtrFromString(src)
	if err != nil {
		os.Remove(src)
		return fmt.Errorf("原子替换失败 %s -> %s: 源路径编码失败: %w", src, dst, err)
	}

	dstPtr, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		os.Remove(src)
		return fmt.Errorf("原子替换失败 %s -> %s: 目标路径编码失败: %w", src, dst, err)
	}

	const maxAttempts = 3
	var lastErr error

	// 最多重试 3 次（处理文件被临时锁定的情况）
	for attempt := 0; attempt < maxAttempts; attempt++ {
		ret, _, callErr := procMoveFileExW.Call(
			uintptr(unsafe.Pointer(srcPtr)),
			uintptr(unsafe.Pointer(dstPtr)),
			MOVEFILE_REPLACE_EXISTING,
		)

		if ret != 0 {
			return nil // 成功
		}
		lastErr = callErr

		// ERROR_SHARING_VIOLATION = 32（文件被其他进程锁定）
		if callErr == syscall.Errno(32) {
			if attempt < maxAttempts-1 {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			break // 重试耗尽
		}

		os.Remove(src)
		return fmt.Errorf("原子替换失败 %s -> %s: MoveFileExW 失败: %w", src, dst, callErr)
	}

	os.Remove(src)
	if lastErr == nil {
		return fmt.Errorf("原子替换失败 %s -> %s: MoveFileExW 重试耗尽", src, dst)
	}
	return fmt.Errorf("原子替换失败 %s -> %s: MoveFileExW 重试耗尽，最后错误: %w", src, dst, lastErr)
}
