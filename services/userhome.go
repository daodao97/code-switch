package services

import (
	"fmt"
	"os"
	"path/filepath"
)

// getUserHomeDir 获取并校验用户家目录
// 确保返回值非空、绝对路径，避免相对路径导致写入到工作目录等安全问题
func getUserHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户家目录失败: %w", err)
	}

	home = filepath.Clean(home)
	if home == "" || home == "." {
		return "", fmt.Errorf("无效的家目录路径: 空路径")
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("无效的家目录路径: 非绝对路径: %s", home)
	}

	return home, nil
}
