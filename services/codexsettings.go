package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	codexSettingsDir      = ".codex"
	codexConfigFileName   = "config.toml"
	codexBackupConfigName = "cc-studio.back.config.toml"
	codexAuthFileName     = "auth.json"
	codexBackupAuthName   = "cc-studio.back.auth.json"
	codexPreferredAuth    = "apikey"
	codexDefaultModel     = "gpt-5-codex"
	codexProviderKey      = "code-switch-r"
	codexEnvKey           = "OPENAI_API_KEY"
	codexWireAPI          = "responses"
	codexTokenValue       = "code-switch-r"
)

type CodexSettingsService struct {
	relayAddr string
}

func NewCodexSettingsService(relayAddr string) *CodexSettingsService {
	return &CodexSettingsService{relayAddr: relayAddr}
}

func (css *CodexSettingsService) ProxyStatus() (ClaudeProxyStatus, error) {
	status := ClaudeProxyStatus{Enabled: false, BaseURL: css.baseURL()}
	config, err := css.readConfig()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status, nil
		}
		return status, err
	}

	// 向后兼容：同时检查 code-switch-r（新）和 code-switch（旧）两个 key
	proxyKeys := []string{codexProviderKey, "code-switch"}
	baseURL := css.baseURL()

	for _, key := range proxyKeys {
		provider, ok := config.ModelProviders[key]
		if !ok {
			continue
		}
		if strings.EqualFold(config.ModelProvider, key) && strings.EqualFold(provider.BaseURL, baseURL) {
			status.Enabled = true
			return status, nil
		}
	}

	return status, nil
}

func (css *CodexSettingsService) EnableProxy() error {
	settingsPath, backupPath, err := css.paths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	var raw map[string]any
	if _, err := os.Stat(settingsPath); err == nil {
		content, readErr := os.ReadFile(settingsPath)
		if readErr != nil {
			return readErr
		}
		if err := os.WriteFile(backupPath, content, 0o600); err != nil {
			return err
		}
		if err := toml.Unmarshal(content, &raw); err != nil {
			return err
		}
	} else {
		raw = make(map[string]any)
	}
	if raw == nil {
		raw = make(map[string]any)
	}

	// 最小侵入模式：只设置必需的代理相关字段
	raw["preferred_auth_method"] = codexPreferredAuth
	raw["model_provider"] = codexProviderKey

	// 保留用户的 model 设置，只在不存在时才使用默认值
	if _, exists := raw["model"]; !exists {
		raw["model"] = codexDefaultModel
	}

	modelProviders := ensureTomlTable(raw, "model_providers")
	provider := ensureProviderTable(modelProviders, codexProviderKey)
	provider["name"] = codexProviderKey
	provider["base_url"] = css.baseURL()
	provider["wire_api"] = codexWireAPI
	provider["requires_openai_auth"] = false
	modelProviders[codexProviderKey] = provider

	data, err := toml.Marshal(raw)
	if err != nil {
		return err
	}
	cleaned := stripModelProvidersHeader(data)

	// 原子写入
	if err := AtomicWriteBytes(settingsPath, cleaned); err != nil {
		return err
	}
	return css.writeAuthFile()
}

func (css *CodexSettingsService) DisableProxy() error {
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
	}
	return css.restoreAuthFile()
}

func (css *CodexSettingsService) readConfig() (*codexConfig, error) {
	settingsPath, _, err := css.paths()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, err
	}
	var cfg codexConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.ModelProviders == nil {
		cfg.ModelProviders = make(map[string]codexProvider)
	}
	return &cfg, nil
}

func (css *CodexSettingsService) paths() (settingsPath string, backupPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, codexSettingsDir)
	return filepath.Join(dir, codexConfigFileName), filepath.Join(dir, codexBackupConfigName), nil
}

func (css *CodexSettingsService) authPaths() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, codexSettingsDir)
	return filepath.Join(dir, codexAuthFileName), filepath.Join(dir, codexBackupAuthName), nil
}

func (css *CodexSettingsService) baseURL() string {
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

type codexConfig struct {
	PreferredAuthMethod string                   `toml:"preferred_auth_method"`
	Model               string                   `toml:"model"`
	ModelProvider       string                   `toml:"model_provider"`
	ModelProviders      map[string]codexProvider `toml:"model_providers"`
}

type codexProvider struct {
	Name               string `toml:"name"`
	BaseURL            string `toml:"base_url"`
	EnvKey             string `toml:"env_key"`
	WireAPI            string `toml:"wire_api"`
	RequiresOpenAIAuth bool   `toml:"requires_openai_auth"`
}

func ensureTomlTable(raw map[string]any, key string) map[string]map[string]any {
	val, ok := raw[key]
	if ok {
		if mp, ok := val.(map[string]map[string]any); ok {
			return mp
		}
		if generic, ok := val.(map[string]any); ok {
			result := make(map[string]map[string]any)
			for k, v := range generic {
				if inner, ok := v.(map[string]any); ok {
					result[k] = inner
				}
			}
			raw[key] = result
			return result
		}
	}
	mp := make(map[string]map[string]any)
	raw[key] = mp
	return mp
}

func ensureProviderTable(mp map[string]map[string]any, key string) map[string]any {
	if provider, ok := mp[key]; ok {
		return provider
	}
	provider := make(map[string]any)
	mp[key] = provider
	return provider
}

func stripModelProvidersHeader(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "[model_providers]" {
			continue
		}
		result = append(result, line)
	}
	return []byte(strings.Join(result, "\n"))
}

func (css *CodexSettingsService) writeAuthFile() error {
	authPath, backupPath, err := css.authPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(authPath); err == nil {
		content, readErr := os.ReadFile(authPath)
		if readErr != nil {
			return readErr
		}
		if err := os.WriteFile(backupPath, content, 0o600); err != nil {
			return err
		}
	}
	payload := map[string]string{
		codexEnvKey: codexTokenValue,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(authPath, data, 0o600)
}

func (css *CodexSettingsService) restoreAuthFile() error {
	authPath, backupPath, err := css.authPaths()
	if err != nil {
		return err
	}
	if err := os.Remove(authPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Rename(backupPath, authPath); err != nil {
			return err
		}
	}
	return nil
}

// ApplySingleProvider 直连应用单一供应商（仅在代理关闭时可用）
// 将指定 provider 的配置直接写入 Codex 的 config.toml 和 auth.json
func (css *CodexSettingsService) ApplySingleProvider(providerID int) error {
	// 1. 检查代理状态：代理启用时禁止直连应用
	proxyStatus, err := css.ProxyStatus()
	if err != nil {
		return fmt.Errorf("检查代理状态失败: %w", err)
	}
	if proxyStatus.Enabled {
		return fmt.Errorf("本地代理已启用，请先关闭代理再进行直接应用")
	}

	// 2. 加载 provider 列表
	providers, err := loadProviderSnapshot("codex")
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
	configPath, _, err := css.paths()
	if err != nil {
		return fmt.Errorf("获取配置路径失败: %w", err)
	}

	// 6. 创建备份
	if _, err := CreateBackup(configPath); err != nil {
		fmt.Printf("[CodexSettingsService] 配置文件备份失败（非阻塞）: %v\n", err)
	}

	// 7. 读取现有配置
	var raw map[string]any
	if data, readErr := os.ReadFile(configPath); readErr == nil && len(data) > 0 {
		if unmarshalErr := toml.Unmarshal(data, &raw); unmarshalErr != nil {
			return fmt.Errorf("config.toml 解析失败，请检查文件格式: %w", unmarshalErr)
		}
	}
	if raw == nil {
		raw = make(map[string]any)
	}

	// 8. 使用供应商名称作为 provider key（处理特殊字符）
	providerKey := sanitizeProviderKey(provider.Name, int(provider.ID))

	// 9. 设置 model_provider 和认证方式
	raw["preferred_auth_method"] = codexPreferredAuth
	raw["model_provider"] = providerKey

	// 10. 设置 model_providers 配置
	modelProviders := ensureTomlTable(raw, "model_providers")
	providerConfig := ensureProviderTable(modelProviders, providerKey)
	providerConfig["name"] = providerKey
	providerConfig["base_url"] = normalizeURLTrimSlash(provider.APIURL)
	providerConfig["wire_api"] = codexWireAPI
	providerConfig["requires_openai_auth"] = false
	modelProviders[providerKey] = providerConfig

	// 11. 序列化并写入 config.toml
	data, err := toml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	cleaned := stripModelProvidersHeader(data)
	if err := AtomicWriteBytes(configPath, cleaned); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}

	// 12. 写入 auth.json
	if err := css.writeDirectApplyAuthFile(provider.APIKey); err != nil {
		return fmt.Errorf("写入认证文件失败: %w", err)
	}

	return nil
}

// writeDirectApplyAuthFile 写入直连应用的 auth.json
func (css *CodexSettingsService) writeDirectApplyAuthFile(apiKey string) error {
	authPath, _, err := css.authPaths()
	if err != nil {
		return err
	}

	// 备份现有 auth.json
	if _, err := CreateBackup(authPath); err != nil {
		fmt.Printf("[CodexSettingsService] auth.json 备份失败（非阻塞）: %v\n", err)
	}

	payload := map[string]string{
		codexEnvKey: apiKey,
	}

	return AtomicWriteJSON(authPath, payload)
}

// sanitizeProviderKey 将供应商名称转换为合法的 TOML key
// providerID 用于确保唯一性，避免不同 provider 生成相同 key
func sanitizeProviderKey(name string, providerID int) string {
	// 转小写，替换空格为连字符，移除特殊字符
	key := strings.ToLower(name)
	key = strings.ReplaceAll(key, " ", "-")
	// 只保留字母、数字、连字符
	var result strings.Builder
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	if result.Len() == 0 {
		// 名称无有效字符时，使用 provider ID 生成唯一 key
		return fmt.Sprintf("provider-%d", providerID)
	}
	finalKey := result.String()
	// 避免与代理模式的 key 冲突
	if finalKey == codexProviderKey {
		return fmt.Sprintf("%s-%d", finalKey, providerID)
	}
	return finalKey
}

// GetDirectAppliedProviderID 返回当前直连应用的 Provider ID
// 通过读取 CLI 配置文件反推当前使用的 provider
func (css *CodexSettingsService) GetDirectAppliedProviderID() (*int64, error) {
	// 1. 检查代理状态
	proxyStatus, err := css.ProxyStatus()
	if err != nil {
		return nil, fmt.Errorf("检查代理状态失败: %w", err)
	}
	if proxyStatus.Enabled {
		return nil, nil
	}

	// 2. 读取 config.toml
	config, err := css.readConfig()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}

	// 3. 获取当前 model_provider
	currentProviderKey := config.ModelProvider
	if currentProviderKey == "" || currentProviderKey == codexProviderKey {
		// 指向代理或未配置
		return nil, nil
	}

	// 4. 获取对应的 base_url
	provider, ok := config.ModelProviders[currentProviderKey]
	if !ok {
		return nil, nil
	}
	currentURL := provider.BaseURL

	// 5. 读取 auth.json 获取 API Key
	currentKey := css.readAuthKey()

	// 6. 加载 provider 列表并匹配
	providers, err := loadProviderSnapshot("codex")
	if err != nil {
		return nil, fmt.Errorf("加载供应商配置失败: %w", err)
	}

	// 7. 按 URL + Key 匹配 provider
	for _, p := range providers {
		if urlsEqualFold(p.APIURL, currentURL) && p.APIKey == currentKey {
			id := p.ID
			return &id, nil
		}
	}

	return nil, nil
}

// readAuthKey 读取 auth.json 中的 API Key
func (css *CodexSettingsService) readAuthKey() string {
	authPath, _, err := css.authPaths()
	if err != nil {
		return ""
	}

	data, err := os.ReadFile(authPath)
	if err != nil {
		return ""
	}

	var payload map[string]string
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}

	return payload[codexEnvKey]
}
