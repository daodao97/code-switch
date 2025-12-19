package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// AtomicWriteJSON 原子写入 JSON 文件
// 写入临时文件后重命名，避免半写状态导致文件损坏
func AtomicWriteJSON(path string, data interface{}) error {
	// 序列化 JSON
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON 序列化失败: %w", err)
	}

	return AtomicWriteBytes(path, bytes)
}

// AtomicWriteBytes 原子写入字节数据
func AtomicWriteBytes(path string, data []byte) error {
	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %s: %w", dir, err)
	}

	// 生成临时文件路径（同目录下，避免跨文件系统问题）
	tmpPath := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())

	// 写入临时文件
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("写入临时文件失败 %s: %w", tmpPath, err)
	}

	// Windows: rename 目标存在时会失败，需要先删除
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(path); err == nil {
			if err := os.Remove(path); err != nil {
				// 删除失败，清理临时文件
				os.Remove(tmpPath)
				return fmt.Errorf("删除目标文件失败 %s: %w", path, err)
			}
		}
	}

	// 原子重命名
	if err := os.Rename(tmpPath, path); err != nil {
		// 重命名失败，清理临时文件
		os.Remove(tmpPath)
		return fmt.Errorf("原子替换失败 %s -> %s: %w", tmpPath, path, err)
	}

	return nil
}

// AtomicWriteText 原子写入文本文件
func AtomicWriteText(path string, text string) error {
	return AtomicWriteBytes(path, []byte(text))
}

// CreateBackup 创建文件备份
// 返回备份文件路径，使用纳秒级时间戳 + O_EXCL 避免并发碰撞
func CreateBackup(path string) (string, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", nil // 文件不存在，无需备份
	}
	if err != nil {
		return "", fmt.Errorf("检查原文件失败 %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("无法为目录创建备份: %s", path)
	}

	// 读取原文件
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取原文件失败 %s: %w", path, err)
	}

	// 重试最多 3 次，使用 O_EXCL 避免覆盖
	for attempt := 0; attempt < 3; attempt++ {
		backupPath := fmt.Sprintf("%s.bak.%d", path, time.Now().UnixNano())
		f, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			// 纳秒级时间戳碰撞，短暂延迟后重试
			time.Sleep(time.Microsecond)
			continue
		}
		if err != nil {
			return "", fmt.Errorf("创建备份文件失败 %s: %w", backupPath, err)
		}

		// 写入内容
		if _, err := f.Write(content); err != nil {
			_ = f.Close()
			_ = os.Remove(backupPath)
			return "", fmt.Errorf("写入备份文件失败 %s: %w", backupPath, err)
		}

		// 关闭文件
		if err := f.Close(); err != nil {
			_ = os.Remove(backupPath)
			return "", fmt.Errorf("关闭备份文件失败 %s: %w", backupPath, err)
		}

		return backupPath, nil
	}

	return "", fmt.Errorf("创建备份失败：文件名冲突过多，请稍后重试")
}

// RestoreBackup 从备份恢复文件
func RestoreBackup(backupPath, targetPath string) error {
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("备份文件不存在: %s", backupPath)
	}

	// 读取备份文件
	content, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("读取备份文件失败 %s: %w", backupPath, err)
	}

	// 原子写入目标文件
	return AtomicWriteBytes(targetPath, content)
}

// ReadJSONFile 读取 JSON 文件到指定结构
func ReadJSONFile(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// FileExists 检查文件是否存在
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// EnsureDir 确保目录存在
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// FindLatestBackup 按时间戳查找最新的备份文件（*.bak.<timestamp>）
func FindLatestBackup(configPath string) (string, error) {
	dir := filepath.Dir(configPath)
	base := filepath.Base(configPath)
	pattern := base + ".bak.*"

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("没有找到备份文件")
		}
		return "", err
	}

	var latestPath string
	var latestMod time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matched, _ := filepath.Match(pattern, entry.Name())
		if !matched {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		if latestPath == "" || info.ModTime().After(latestMod) {
			latestPath = filepath.Join(dir, entry.Name())
			latestMod = info.ModTime()
		}
	}

	if latestPath == "" {
		return "", fmt.Errorf("没有找到备份文件")
	}

	return latestPath, nil
}
