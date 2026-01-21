package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const proxyStateVersion = 1

// ProxyState 记录代理启用前的基线信息，用于禁用代理时做"手术式"恢复，避免回滚整文件导致用户配置丢失。
// 设计原则：
// 1. 使用指针表示"是否存在"：nil 表示启用代理前不存在该键
// 2. 记录注入值用于禁用时判断"当前值是否仍为我们注入的"
// 3. 存储在 ~/.code-switch/proxy-state/{platform}.json，与 CLI 配置文件隔离
type ProxyState struct {
	Version           int     `json:"version"`
	CreatedAt         string  `json:"created_at"`
	TargetPath        string  `json:"target_path"`
	FileExisted       bool    `json:"file_existed"`
	EnvExisted        bool    `json:"env_existed"`
	OriginalBaseURL   *string `json:"original_base_url,omitempty"`
	OriginalAuthToken *string `json:"original_auth_token,omitempty"`
	InjectedBaseURL   string  `json:"injected_base_url"`
	InjectedAuthToken string  `json:"injected_auth_token"`

	// ========== Codex 专用字段 ==========
	// Codex 使用 TOML 配置，结构更复杂，需要额外字段

	// AuthFilePath: Codex 的 auth.json 路径
	AuthFilePath string `json:"auth_file_path,omitempty"`
	// AuthFileExisted: auth.json 是否在启用代理前存在
	AuthFileExisted bool `json:"auth_file_existed,omitempty"`
	// OriginalModelProvider: model_provider 的原始值
	OriginalModelProvider *string `json:"original_model_provider,omitempty"`
	// OriginalPreferredAuth: preferred_auth_method 的原始值
	OriginalPreferredAuth *string `json:"original_preferred_auth,omitempty"`
	// InjectedProviderKey: 注入的 model_providers 键名（如 "code-switch-r"）
	InjectedProviderKey string `json:"injected_provider_key,omitempty"`
	// ModelProvidersKeyExisted: model_providers.{key} 是否在启用前存在
	ModelProvidersKeyExisted bool `json:"model_providers_key_existed,omitempty"`
}

// normalizeProxyPlatform 对 platform 做最小安全校验，避免路径穿越等问题。
func normalizeProxyPlatform(platform string) (string, error) {
	p := strings.TrimSpace(strings.ToLower(platform))
	if p == "" {
		return "", fmt.Errorf("platform 不能为空")
	}
	for _, r := range p {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("非法 platform: %s", platform)
	}
	return p, nil
}

// GetProxyStatePath 返回状态文件路径：~/.code-switch/proxy-state/{platform}.json
func GetProxyStatePath(platform string) (string, error) {
	p, err := normalizeProxyPlatform(platform)
	if err != nil {
		return "", err
	}
	home, err := getUserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".code-switch", "proxy-state", p+".json"), nil
}

// ProxyStateExists 检查指定平台的代理状态文件是否存在
func ProxyStateExists(platform string) (bool, error) {
	path, err := GetProxyStatePath(platform)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// LoadProxyState 读取并解析指定平台的代理状态文件。
func LoadProxyState(platform string) (*ProxyState, error) {
	path, err := GetProxyStatePath(platform)
	if err != nil {
		return nil, err
	}

	var state ProxyState
	if err := ReadJSONFile(path, &state); err != nil {
		return nil, err
	}

	if strings.TrimSpace(state.TargetPath) == "" {
		return nil, fmt.Errorf("代理状态文件无效: target_path 为空")
	}

	return &state, nil
}

// SaveProxyState 保存代理状态文件（原子写入）。
func SaveProxyState(platform string, state *ProxyState) error {
	if state == nil {
		return fmt.Errorf("state 不能为空")
	}

	path, err := GetProxyStatePath(platform)
	if err != nil {
		return err
	}

	if state.Version == 0 {
		state.Version = proxyStateVersion
	}
	if strings.TrimSpace(state.CreatedAt) == "" {
		state.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if strings.TrimSpace(state.TargetPath) == "" {
		return fmt.Errorf("state.target_path 不能为空")
	}

	// 确保目录存在（权限收敛：Unix 下 0700，Windows 下无影响）
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建代理状态目录失败: %w", err)
	}

	return AtomicWriteJSON(path, state)
}

// DeleteProxyState 删除指定平台的代理状态文件（不存在则忽略）。
func DeleteProxyState(platform string) error {
	path, err := GetProxyStatePath(platform)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}
