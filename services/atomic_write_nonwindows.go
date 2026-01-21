//go:build !windows

package services

import (
	"fmt"
	"os"
)

// atomicRename Unix 平台原子重命名
// os.Rename 在 POSIX 系统上本身就是原子操作
func atomicRename(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		os.Remove(src)
		return fmt.Errorf("原子替换失败 %s -> %s: os.Rename 失败: %w", src, dst, err)
	}
	return nil
}
