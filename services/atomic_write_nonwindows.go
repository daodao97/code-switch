//go:build !windows

package services

import "os"

// atomicRename Unix 平台原子重命名
// os.Rename 在 POSIX 系统上本身就是原子操作
func atomicRename(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		os.Remove(src)
		return err
	}
	return nil
}
