package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	claudeSettingsDir      = ".claude"
	claudeSettingsFileName = "settings.json"
	claudeBackupFileName   = "cc-studio.back.settings.json"
	claudeAuthTokenValue   = "code-switch-r"
)

type ClaudeProxyStatus struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
}

type ClaudeSettingsService struct {
	relayAddr string
}

func NewClaudeSettingsService(relayAddr string) *ClaudeSettingsService {
	return &ClaudeSettingsService{relayAddr: relayAddr}
}

func (css *ClaudeSettingsService) ProxyStatus() (ClaudeProxyStatus, error) {
	status := ClaudeProxyStatus{Enabled: false, BaseURL: css.baseURL()}
	settingsPath, _, err := css.paths()
	if err != nil {
		return status, err
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status, nil
		}
		return status, err
	}
	// 使用 map[string]any 宽容解析，避免 env 中非字符串值导致整体解析失败
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return status, nil
	}
	env, _ := payload["env"].(map[string]any)
	if env == nil {
		return status, nil
	}
	// 将 env 值转为字符串进行比较（nil 时返回空字符串）
	baseURLVal := anyToString(env["ANTHROPIC_BASE_URL"])
	baseURL := css.baseURL()
	// 只检查 base_url 是否指向本地代理，因为：
	// 1. base_url 是决定代理是否生效的关键字段
	// 2. auth_token 可能被 Claude CLI 覆盖，但不影响代理功能
	// 去除尾随斜杠以避免用户手动编辑时的小差异导致状态误判
	enabled := strings.EqualFold(
		strings.TrimSuffix(strings.TrimSpace(baseURLVal), "/"),
		strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
	)
	status.Enabled = enabled
	return status, nil
}

func (css *ClaudeSettingsService) EnableProxy() error {
	settingsPath, backupPath, err := css.paths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}

	// 读取现有配置（最小侵入模式：保留用户的其他配置）
	var existingData map[string]interface{}
	if _, statErr := os.Stat(settingsPath); statErr == nil {
		content, readErr := os.ReadFile(settingsPath)
		if readErr != nil {
			return readErr
		}
		// 创建备份
		if err := os.WriteFile(backupPath, content, 0o600); err != nil {
			return err
		}
		// 解析现有配置（仅当文件非空时）
		if len(content) > 0 {
			if err := json.Unmarshal(content, &existingData); err != nil {
				// JSON 解析失败，使用空配置继续（备份已保存）
				fmt.Printf("[警告] settings.json 格式无效，已备份到 %s，将使用空配置: %v\n", backupPath, err)
				existingData = make(map[string]interface{})
			}
		}
		if existingData == nil {
			existingData = make(map[string]interface{})
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		// 文件不存在，使用空配置
		existingData = make(map[string]interface{})
	} else {
		// 其他 stat 错误（权限等），返回错误避免意外覆盖
		return fmt.Errorf("无法读取 settings.json: %w", statErr)
	}

	// 仅更新代理相关字段，保留其他配置（如 model, alwaysThinkingEnabled, enabledPlugins）
	env, ok := existingData["env"].(map[string]interface{})
	if !ok {
		env = make(map[string]interface{})
	}
	env["ANTHROPIC_AUTH_TOKEN"] = claudeAuthTokenValue
	env["ANTHROPIC_BASE_URL"] = css.baseURL()
	existingData["env"] = env

	// 原子写入
	return AtomicWriteJSON(settingsPath, existingData)
}

func (css *ClaudeSettingsService) DisableProxy() error {
	settingsPath, backupPath, err := css.paths()
	if err != nil {
		return err
	}
	if err := os.Remove(settingsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Rename(backupPath, settingsPath); err != nil {
			return err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return nil
}

func (css *ClaudeSettingsService) paths() (settingsPath string, backupPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, claudeSettingsDir)
	return filepath.Join(dir, claudeSettingsFileName), filepath.Join(dir, claudeBackupFileName), nil
}

func (css *ClaudeSettingsService) baseURL() string {
	addr := strings.TrimSpace(css.relayAddr)
	if addr == "" {
		addr = ":18100"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return host
}

// anyToString 将 any 类型安全转换为字符串，nil 返回空字符串
func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// ApplySingleProvider 直连应用单一供应商（仅在代理关闭时可用）
// 将指定 provider 的配置直接写入 Claude Code 的 settings.json
func (css *ClaudeSettingsService) ApplySingleProvider(providerID int) error {
	// 1. 检查代理状态：代理启用时禁止直连应用
	proxyStatus, err := css.ProxyStatus()
	if err != nil {
		return fmt.Errorf("检查代理状态失败: %w", err)
	}
	if proxyStatus.Enabled {
		return fmt.Errorf("本地代理已启用，请先关闭代理再进行直接应用")
	}

	// 2. 加载 provider 列表
	providers, err := loadProviderSnapshot("claude")
	if err != nil {
		return fmt.Errorf("加载供应商配置失败: %w", err)
	}

	// 3. 查找目标 provider
	provider, found := findProviderByID(providers, int64(providerID))
	if !found {
		return fmt.Errorf("未找到 ID 为 %d 的供应商", providerID)
	}

	// 4. 验证 provider 配置
	if provider.APIURL == "" {
		return fmt.Errorf("供应商 '%s' 未配置 API 地址", provider.Name)
	}
	if provider.APIKey == "" {
		return fmt.Errorf("供应商 '%s' 未配置 API 密钥", provider.Name)
	}

	// 5. 获取配置文件路径
	settingsPath, _, err := css.paths()
	if err != nil {
		return fmt.Errorf("获取配置路径失败: %w", err)
	}

	// 6. 创建备份
	if _, err := CreateBackup(settingsPath); err != nil {
		// 备份失败不阻塞，仅记录日志
		fmt.Printf("[ClaudeSettingsService] 备份失败（非阻塞）: %v\n", err)
	}

	// 7. 读取现有配置（最小侵入模式）
	existingData := make(map[string]interface{})
	if data, readErr := os.ReadFile(settingsPath); readErr == nil && len(data) > 0 {
		if unmarshalErr := json.Unmarshal(data, &existingData); unmarshalErr != nil {
			return fmt.Errorf("settings.json 解析失败，请检查文件格式: %w", unmarshalErr)
		}
	}

	// 8. 仅更新代理相关字段
	env, ok := existingData["env"].(map[string]interface{})
	if !ok {
		env = make(map[string]interface{})
	}
	env["ANTHROPIC_BASE_URL"] = normalizeURLTrimSlash(provider.APIURL)
	env["ANTHROPIC_AUTH_TOKEN"] = provider.APIKey
	existingData["env"] = env

	// 9. 原子写入
	if err := AtomicWriteJSON(settingsPath, existingData); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}

	return nil
}

// GetDirectAppliedProviderID 返回当前直连应用的 Provider ID
// 通过读取 CLI 配置文件反推当前使用的 provider
// 返回值：
//   - nil: 配置指向本地代理 或 无法匹配到 provider
//   - *int64: 匹配到的 provider ID
func (css *ClaudeSettingsService) GetDirectAppliedProviderID() (*int64, error) {
	// 1. 检查代理状态
	proxyStatus, err := css.ProxyStatus()
	if err != nil {
		return nil, fmt.Errorf("检查代理状态失败: %w", err)
	}
	// 代理启用时，直连状态无意义
	if proxyStatus.Enabled {
		return nil, nil
	}

	// 2. 读取当前 settings.json
	settingsPath, _, err := css.paths()
	if err != nil {
		return nil, fmt.Errorf("获取配置路径失败: %w", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, nil
	}

	env, _ := payload["env"].(map[string]interface{})
	if env == nil {
		return nil, nil
	}

	currentURL := anyToString(env["ANTHROPIC_BASE_URL"])
	currentKey := anyToString(env["ANTHROPIC_AUTH_TOKEN"])

	if currentURL == "" {
		return nil, nil
	}

	// 3. 加载 provider 列表并匹配
	providers, err := loadProviderSnapshot("claude")
	if err != nil {
		return nil, fmt.Errorf("加载供应商配置失败: %w", err)
	}

	// 4. 按 URL + Key 匹配 provider
	for _, p := range providers {
		if urlsEqualFold(p.APIURL, currentURL) && p.APIKey == currentKey {
			id := p.ID
			return &id, nil
		}
	}

	return nil, nil
}
