package services

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile 原子写入文件（跨平台）
// 策略：写入临时文件 → fsync → 重命名
// 防止断电或崩溃导致状态文件损坏
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	// 确保目录存在
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %s（目标文件: %s）: %w", dir, path, err)
	}

	// 创建临时文件（同目录，确保同文件系统）
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败 %s（目标文件: %s）: %w", dir, path, err)
	}
	tmpPath := tmp.Name()

	// 写入数据
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("写入临时文件失败 %s（目标文件: %s）: %w", tmpPath, path, err)
	}

	// 设置文件权限
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("设置权限失败 %s（目标文件: %s）: %w", tmpPath, path, err)
	}

	// 确保数据落盘（fsync）
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("同步临时文件失败 %s（目标文件: %s）: %w", tmpPath, path, err)
	}

	// 关闭文件
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("关闭临时文件失败 %s（目标文件: %s）: %w", tmpPath, path, err)
	}

	// 原子重命名（平台特定实现）
	return atomicRename(tmpPath, path)
}
