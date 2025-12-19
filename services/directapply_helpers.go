package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// providerFilePathNoCreate 返回 provider 配置文件路径（不创建目录）
// 用于只读操作场景，避免副作用
func providerFilePathNoCreate(kind string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(home, ".code-switch")
	var filename string

	switch strings.ToLower(kind) {
	case "claude", "claude-code", "claude_code":
		filename = "claude-code.json"
	case "codex":
		filename = "codex.json"
	default:
		return "", nil
	}

	return filepath.Join(dir, filename), nil
}

// loadProviderSnapshot 只读加载 provider 列表（不触发迁移和保存）
// 返回当前磁盘上的快照，用于直连应用的 provider 查找
func loadProviderSnapshot(kind string) ([]Provider, error) {
	path, err := providerFilePathNoCreate(kind)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	if len(data) == 0 {
		return []Provider{}, nil
	}

	var envelope providerEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}

	return envelope.Providers, nil
}

// findProviderByID 在 provider 列表中按 ID 查找
// 返回找到的 Provider 和是否找到
func findProviderByID(providers []Provider, id int64) (Provider, bool) {
	for _, p := range providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// normalizeURLTrimSlash 标准化 URL：去除所有尾部斜杠和空白
func normalizeURLTrimSlash(url string) string {
	return strings.TrimRight(strings.TrimSpace(url), "/")
}

// urlsEqualFold 不区分大小写比较两个 URL（已标准化）
func urlsEqualFold(a, b string) bool {
	return strings.EqualFold(normalizeURLTrimSlash(a), normalizeURLTrimSlash(b))
}
