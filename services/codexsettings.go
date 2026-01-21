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
	authPath, authBackupPath, err := css.authPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}

	// 幂等化检查：状态文件存在则视为已启用，不覆盖基线
	stateExists, err := ProxyStateExists("codex")
	if err != nil {
		return err
	}

	// 读取现有配置（最小侵入模式：保留用户的其他配置）
	var raw map[string]any
	fileExisted := false
	if _, statErr := os.Stat(settingsPath); statErr == nil {
		fileExisted = true
		content, readErr := os.ReadFile(settingsPath)
		if readErr != nil {
			return readErr
		}
		// 仅首次启用时创建备份，避免重复 Enable 覆盖基线
		if !stateExists {
			if err := os.WriteFile(backupPath, content, 0o600); err != nil {
				return err
			}
		}
		if err := toml.Unmarshal(content, &raw); err != nil {
			// TOML 解析失败，使用空配置继续（备份已保存）
			fmt.Printf("[警告] config.toml 格式无效，已备份到 %s，将使用空配置: %v\n", backupPath, err)
			raw = make(map[string]any)
		}
	} else {
		raw = make(map[string]any)
	}
	if raw == nil {
		raw = make(map[string]any)
	}

	// 首次启用：记录启用前的关键字段基线到状态文件
	if !stateExists {
		// 检查 auth.json 是否存在
		authFileExisted := false
		var originalAuthKey *string
		if _, authStatErr := os.Stat(authPath); authStatErr == nil {
			authFileExisted = true
			// 备份 auth.json
			if authContent, authReadErr := os.ReadFile(authPath); authReadErr == nil {
				if err := os.WriteFile(authBackupPath, authContent, 0o600); err != nil {
					fmt.Printf("[警告] auth.json 备份失败: %v\n", err)
				}
				// 读取原始 API Key
				var authPayload map[string]string
				if json.Unmarshal(authContent, &authPayload) == nil {
					if v, ok := authPayload[codexEnvKey]; ok {
						originalAuthKey = &v
					}
				}
			}
		}

		// 检查 model_providers.code-switch-r 是否已存在
		modelProvidersKeyExisted := false
		if mpRaw, ok := raw["model_providers"]; ok {
			if mp, ok := mpRaw.(map[string]any); ok {
				_, modelProvidersKeyExisted = mp[codexProviderKey]
			}
		}

		state := &ProxyState{
			TargetPath:               settingsPath,
			FileExisted:              fileExisted,
			InjectedBaseURL:          css.baseURL(),
			InjectedAuthToken:        codexTokenValue,
			AuthFilePath:             authPath,
			AuthFileExisted:          authFileExisted,
			InjectedProviderKey:      codexProviderKey,
			ModelProvidersKeyExisted: modelProvidersKeyExisted,
		}

		// 记录原始 model_provider
		if v, ok := raw["model_provider"]; ok {
			if s, ok := v.(string); ok {
				state.OriginalModelProvider = &s
			}
		}
		// 记录原始 preferred_auth_method
		if v, ok := raw["preferred_auth_method"]; ok {
			if s, ok := v.(string); ok {
				state.OriginalPreferredAuth = &s
			}
		}
		// 记录原始 auth key
		state.OriginalAuthToken = originalAuthKey

		if err := SaveProxyState("codex", state); err != nil {
			return err
		}
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
	settingsPath, _, err := css.paths()
	if err != nil {
		return err
	}

	// 读取当前 config.toml（保留用户在代理期间的所有编辑）
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 配置文件不存在，清理状态文件和 auth.json 后返回
			_ = css.surgicalRestoreAuthFile(nil)
			return DeleteProxyState("codex")
		}
		return err
	}

	var raw map[string]any
	if len(content) > 0 {
		if err := toml.Unmarshal(content, &raw); err != nil {
			return fmt.Errorf("config.toml 解析失败，请检查文件格式: %w", err)
		}
	}
	if raw == nil {
		raw = make(map[string]any)
	}

	// 尝试加载状态文件
	state, stateErr := LoadProxyState("codex")
	if stateErr != nil {
		// 兜底：状态文件缺失/损坏时，仅在字段仍等于代理值时才删除
		// 避免误删用户自定义的直连配置
		changed := css.fallbackCleanupConfig(raw)
		if changed {
			if err := css.writeConfigToml(settingsPath, raw); err != nil {
				return err
			}
		}
		// 兜底清理 auth.json（仅删除代理 token）
		_ = css.surgicalRestoreAuthFile(nil)
		return DeleteProxyState("codex")
	}

	// 有状态文件：按基线做"手术式"恢复

	// 1. 恢复或删除 model_provider
	if state.OriginalModelProvider != nil {
		raw["model_provider"] = *state.OriginalModelProvider
	} else {
		delete(raw, "model_provider")
	}

	// 2. 恢复或删除 preferred_auth_method
	if state.OriginalPreferredAuth != nil {
		raw["preferred_auth_method"] = *state.OriginalPreferredAuth
	} else {
		delete(raw, "preferred_auth_method")
	}

	// 3. 删除注入的 model_providers.{key} 段（如果启用前不存在）
	if !state.ModelProvidersKeyExisted && state.InjectedProviderKey != "" {
		if mpRaw, ok := raw["model_providers"]; ok {
			if mp, ok := mpRaw.(map[string]any); ok {
				delete(mp, state.InjectedProviderKey)
				// 如果 model_providers 变空，删除整个段
				if len(mp) == 0 {
					delete(raw, "model_providers")
				}
			} else if mpTyped, ok := mpRaw.(map[string]map[string]any); ok {
				delete(mpTyped, state.InjectedProviderKey)
				if len(mpTyped) == 0 {
					delete(raw, "model_providers")
				}
			}
		}
	}

	// 写入配置
	if err := css.writeConfigToml(settingsPath, raw); err != nil {
		return err
	}

	// 手术式恢复 auth.json
	if err := css.surgicalRestoreAuthFile(state); err != nil {
		return err
	}

	return DeleteProxyState("codex")
}

// fallbackCleanupConfig 兜底清理：仅删除仍等于代理值的字段
// 注意：只有当 model_provider 仍指向代理时，才删除 preferred_auth_method
// 避免误删用户正常的 "apikey" 认证配置
func (css *CodexSettingsService) fallbackCleanupConfig(raw map[string]any) bool {
	changed := false
	isProxyActive := false

	// 首先检查 model_provider 是否仍指向代理
	if v, ok := raw["model_provider"]; ok {
		if s, ok := v.(string); ok && (s == codexProviderKey || s == "code-switch") {
			isProxyActive = true
			delete(raw, "model_provider")
			changed = true
		}
	}

	// 只有当 model_provider 指向代理时，才删除 preferred_auth_method
	// 这样可以避免误删用户正常的 "apikey" 认证配置
	if isProxyActive {
		if v, ok := raw["preferred_auth_method"]; ok {
			if s, ok := v.(string); ok && s == codexPreferredAuth {
				delete(raw, "preferred_auth_method")
				changed = true
			}
		}
	}

	// 删除代理专用的 model_providers.code-switch-r
	// 只有当 base_url 仍指向代理时才删除，避免误删用户自定义的同名 provider
	proxyURL := css.baseURL()
	if mpRaw, ok := raw["model_providers"]; ok {
		if mp, ok := mpRaw.(map[string]any); ok {
			// 检查 code-switch-r
			if providerRaw, exists := mp[codexProviderKey]; exists {
				if css.isProviderPointingToProxy(providerRaw, proxyURL) {
					delete(mp, codexProviderKey)
					changed = true
				}
			}
			// 兼容旧版 key: code-switch
			if providerRaw, exists := mp["code-switch"]; exists {
				if css.isProviderPointingToProxy(providerRaw, proxyURL) {
					delete(mp, "code-switch")
					changed = true
				}
			}
			if len(mp) == 0 {
				delete(raw, "model_providers")
			}
		}
	}

	return changed
}

// isProviderPointingToProxy 检查 provider 配置的 base_url 是否指向代理
func (css *CodexSettingsService) isProviderPointingToProxy(providerRaw any, proxyURL string) bool {
	provider, ok := providerRaw.(map[string]any)
	if !ok {
		return false
	}
	baseURL, ok := provider["base_url"].(string)
	if !ok {
		return false
	}
	return strings.EqualFold(
		strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
		strings.TrimSuffix(strings.TrimSpace(proxyURL), "/"),
	)
}

// writeConfigToml 将配置写入 config.toml
func (css *CodexSettingsService) writeConfigToml(path string, raw map[string]any) error {
	data, err := toml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("序列化 config.toml 失败: %w", err)
	}
	cleaned := stripModelProvidersHeader(data)
	return AtomicWriteBytes(path, cleaned)
}

// surgicalRestoreAuthFile 手术式恢复 auth.json
func (css *CodexSettingsService) surgicalRestoreAuthFile(state *ProxyState) error {
	authPath, _, err := css.authPaths()
	if err != nil {
		return err
	}

	// 读取当前 auth.json
	authContent, err := os.ReadFile(authPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// auth.json 不存在，无需处理
			return nil
		}
		return err
	}

	// 使用 map[string]any 以支持非字符串值（与 writeAuthFile 保持一致）
	var payload map[string]any
	if err := json.Unmarshal(authContent, &payload); err != nil {
		// 格式无效，直接返回
		return nil
	}
	if payload == nil {
		return nil
	}

	// 获取当前 API Key（安全类型转换）
	currentKey := ""
	if v, ok := payload[codexEnvKey]; ok {
		if s, ok := v.(string); ok {
			currentKey = s
		}
	}

	if state == nil {
		// 兜底模式：仅删除代理 token
		if currentKey == codexTokenValue {
			delete(payload, codexEnvKey)
			if len(payload) == 0 {
				// 文件变空，删除文件
				return os.Remove(authPath)
			}
			return AtomicWriteJSON(authPath, payload)
		}
		return nil
	}

	// 有状态文件：按基线恢复
	if state.OriginalAuthToken != nil {
		payload[codexEnvKey] = *state.OriginalAuthToken
	} else {
		delete(payload, codexEnvKey)
	}

	// 如果 auth.json 变空且启用前不存在，则删除文件
	if len(payload) == 0 && !state.AuthFileExisted {
		return os.Remove(authPath)
	}

	return AtomicWriteJSON(authPath, payload)
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

// writeAuthFile 外科式写入 auth.json：仅更新 OPENAI_API_KEY，保留其他字段
func (css *CodexSettingsService) writeAuthFile() error {
	authPath, _, err := css.authPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		return err
	}

	// 读取现有 auth.json（如果存在），保留其他字段
	// 使用 map[string]any 以支持非字符串值（如果未来格式变化）
	payload := make(map[string]any)
	if data, readErr := os.ReadFile(authPath); readErr == nil && len(data) > 0 {
		// 解析现有内容
		if unmarshalErr := json.Unmarshal(data, &payload); unmarshalErr != nil {
			// JSON 解析失败，可能是格式损坏，使用空 map 继续
			// 但保留日志以便调试
			fmt.Printf("[警告] auth.json 解析失败，将使用空配置: %v\n", unmarshalErr)
			payload = make(map[string]any)
		}
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		// 非"文件不存在"的读取错误，返回错误避免覆盖
		return fmt.Errorf("读取 auth.json 失败: %w", readErr)
	}
	if payload == nil {
		payload = make(map[string]any)
	}

	// 仅更新代理专用的 API Key
	payload[codexEnvKey] = codexTokenValue

	return AtomicWriteJSON(authPath, payload)
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

	// 使用 map[string]any 以支持非字符串值（与 writeAuthFile/surgicalRestoreAuthFile 保持一致）
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}

	// 安全类型转换
	if v, ok := payload[codexEnvKey]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
