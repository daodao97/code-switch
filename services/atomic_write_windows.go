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
		return fmt.Errorf("源路径编码失败: %w", err)
	}

	dstPtr, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		os.Remove(src)
		return fmt.Errorf("目标路径编码失败: %w", err)
	}

	// 最多重试 3 次（处理文件被临时锁定的情况）
	for attempt := 0; attempt < 3; attempt++ {
		ret, _, callErr := procMoveFileExW.Call(
			uintptr(unsafe.Pointer(srcPtr)),
			uintptr(unsafe.Pointer(dstPtr)),
			MOVEFILE_REPLACE_EXISTING,
		)

		if ret != 0 {
			return nil // 成功
		}

		// ERROR_SHARING_VIOLATION = 32（文件被其他进程锁定）
		if callErr == syscall.Errno(32) && attempt < 2 {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		os.Remove(src)
		return fmt.Errorf("MoveFileExW 失败: %w", callErr)
	}

	os.Remove(src)
	return fmt.Errorf("MoveFileExW 重试耗尽")
}
