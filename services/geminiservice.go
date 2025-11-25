package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GeminiAuthType 认证类型
type GeminiAuthType string

const (
	GeminiAuthOAuth      GeminiAuthType = "oauth-personal"   // Google 官方 OAuth
	GeminiAuthAPIKey     GeminiAuthType = "gemini-api-key"   // API Key 认证
	GeminiAuthPackycode  GeminiAuthType = "packycode"        // PackyCode 合作方
	GeminiAuthGeneric    GeminiAuthType = "generic"          // 通用第三方
)

// GeminiProvider Gemini 供应商配置
type GeminiProvider struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	WebsiteURL          string            `json:"websiteUrl,omitempty"`
	APIKeyURL           string            `json:"apiKeyUrl,omitempty"`
	BaseURL             string            `json:"baseUrl,omitempty"`
	APIKey              string            `json:"apiKey,omitempty"`
	Model               string            `json:"model,omitempty"`
	Description         string            `json:"description,omitempty"`
	Category            string            `json:"category,omitempty"`            // official, third_party, custom
	PartnerPromotionKey string            `json:"partnerPromotionKey,omitempty"` // 用于识别供应商类型
	Enabled             bool              `json:"enabled"`
	EnvConfig           map[string]string `json:"envConfig,omitempty"`           // .env 配置
	SettingsConfig      map[string]any    `json:"settingsConfig,omitempty"`      // settings.json 配置
}

// GeminiPreset 预设供应商
type GeminiPreset struct {
	Name                string            `json:"name"`
	WebsiteURL          string            `json:"websiteUrl"`
	APIKeyURL           string            `json:"apiKeyUrl,omitempty"`
	BaseURL             string            `json:"baseUrl,omitempty"`
	Model               string            `json:"model,omitempty"`
	Description         string            `json:"description,omitempty"`
	Category            string            `json:"category"`
	PartnerPromotionKey string            `json:"partnerPromotionKey,omitempty"`
	EnvConfig           map[string]string `json:"envConfig,omitempty"`
}

// GeminiStatus Gemini 配置状态
type GeminiStatus struct {
	Enabled        bool           `json:"enabled"`
	CurrentProvider string         `json:"currentProvider,omitempty"`
	AuthType       GeminiAuthType `json:"authType"`
	HasAPIKey      bool           `json:"hasApiKey"`
	HasBaseURL     bool           `json:"hasBaseUrl"`
	Model          string         `json:"model,omitempty"`
}

// GeminiService Gemini 配置管理服务
type GeminiService struct {
	mu        sync.Mutex
	providers []GeminiProvider
	presets   []GeminiPreset
	relayAddr string
}

// NewGeminiService 创建 Gemini 服务
func NewGeminiService(relayAddr string) *GeminiService {
	if relayAddr == "" {
		relayAddr = ":18100"
	}
	svc := &GeminiService{
		presets:   getGeminiPresets(),
		relayAddr: relayAddr,
	}
	// 加载已保存的供应商配置
	_ = svc.loadProviders()
	return svc
}

// getGeminiPresets 获取预设供应商列表
func getGeminiPresets() []GeminiPreset {
	return []GeminiPreset{
		{
			Name:                "Google Official",
			WebsiteURL:          "https://ai.google.dev/",
			APIKeyURL:           "https://aistudio.google.com/apikey",
			Description:         "Google 官方 Gemini API (OAuth)",
			Category:            "official",
			PartnerPromotionKey: "google-official",
			EnvConfig:           map[string]string{}, // 空 env，使用 OAuth
		},
		{
			Name:                "PackyCode",
			WebsiteURL:          "https://www.packyapi.com",
			APIKeyURL:           "https://www.packyapi.com/register?aff=cc-switch",
			BaseURL:             "https://www.packyapi.com",
			Model:               "gemini-2.5-pro-preview",
			Description:         "PackyCode 中转服务",
			Category:            "third_party",
			PartnerPromotionKey: "packycode",
			EnvConfig: map[string]string{
				"GOOGLE_GEMINI_BASE_URL": "https://www.packyapi.com",
				"GEMINI_MODEL":           "gemini-2.5-pro-preview",
			},
		},
		{
			Name:        "自定义",
			WebsiteURL:  "",
			Description: "自定义 Gemini API 端点",
			Category:    "custom",
			EnvConfig: map[string]string{
				"GOOGLE_GEMINI_BASE_URL": "",
				"GEMINI_MODEL":           "gemini-2.5-pro-preview",
			},
		},
	}
}

// Start Wails生命周期方法
func (s *GeminiService) Start() error {
	return nil
}

// Stop Wails生命周期方法
func (s *GeminiService) Stop() error {
	return nil
}

// GetPresets 获取预设供应商列表
func (s *GeminiService) GetPresets() []GeminiPreset {
	return s.presets
}

// GetProviders 获取已配置的供应商列表
func (s *GeminiService) GetProviders() []GeminiProvider {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.providers
}

// AddProvider 添加供应商
func (s *GeminiService) AddProvider(provider GeminiProvider) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查 ID 是否重复
	for _, p := range s.providers {
		if p.ID == provider.ID {
			return fmt.Errorf("供应商 ID '%s' 已存在", provider.ID)
		}
	}

	// 生成 ID（如果没有）
	if provider.ID == "" {
		provider.ID = fmt.Sprintf("gemini-%d", len(s.providers)+1)
	}

	s.providers = append(s.providers, provider)
	return s.saveProviders()
}

// UpdateProvider 更新供应商
func (s *GeminiService) UpdateProvider(provider GeminiProvider) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, p := range s.providers {
		if p.ID == provider.ID {
			s.providers[i] = provider
			return s.saveProviders()
		}
	}
	return fmt.Errorf("未找到 ID 为 '%s' 的供应商", provider.ID)
}

// DeleteProvider 删除供应商
func (s *GeminiService) DeleteProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, p := range s.providers {
		if p.ID == id {
			s.providers = append(s.providers[:i], s.providers[i+1:]...)
			return s.saveProviders()
		}
	}
	return fmt.Errorf("未找到 ID 为 '%s' 的供应商", id)
}

// SwitchProvider 切换到指定供应商
func (s *GeminiService) SwitchProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var provider *GeminiProvider
	for i := range s.providers {
		if s.providers[i].ID == id {
			provider = &s.providers[i]
			break
		}
	}
	if provider == nil {
		return fmt.Errorf("未找到 ID 为 '%s' 的供应商", id)
	}

	// 检测认证类型
	authType := detectGeminiAuthType(provider)

	// 根据认证类型写入配置
	switch authType {
	case GeminiAuthOAuth:
		// OAuth：清空 .env
		if err := writeGeminiEnv(map[string]string{}); err != nil {
			return fmt.Errorf("写入 .env 失败: %w", err)
		}
		// 写入 OAuth 认证标志
		if err := writeGeminiSettings(map[string]any{
			"security": map[string]any{
				"auth": map[string]any{
					"selectedType": string(GeminiAuthOAuth),
				},
			},
		}); err != nil {
			return fmt.Errorf("写入 settings.json 失败: %w", err)
		}

	case GeminiAuthPackycode, GeminiAuthAPIKey, GeminiAuthGeneric:
		// API Key 认证：写入 .env
		envConfig := provider.EnvConfig
		if envConfig == nil {
			envConfig = make(map[string]string)
		}
		// 确保必要字段
		if provider.BaseURL != "" && envConfig["GOOGLE_GEMINI_BASE_URL"] == "" {
			envConfig["GOOGLE_GEMINI_BASE_URL"] = provider.BaseURL
		}
		if provider.APIKey != "" && envConfig["GEMINI_API_KEY"] == "" {
			envConfig["GEMINI_API_KEY"] = provider.APIKey
		}
		if provider.Model != "" && envConfig["GEMINI_MODEL"] == "" {
			envConfig["GEMINI_MODEL"] = provider.Model
		}

		if err := writeGeminiEnv(envConfig); err != nil {
			return fmt.Errorf("写入 .env 失败: %w", err)
		}

		// 写入 API Key 认证标志
		if err := writeGeminiSettings(map[string]any{
			"security": map[string]any{
				"auth": map[string]any{
					"selectedType": string(GeminiAuthAPIKey),
				},
			},
		}); err != nil {
			return fmt.Errorf("写入 settings.json 失败: %w", err)
		}
	}

	// 更新启用状态
	for i := range s.providers {
		s.providers[i].Enabled = (s.providers[i].ID == id)
	}

	return s.saveProviders()
}

// GetStatus 获取当前 Gemini 配置状态
func (s *GeminiService) GetStatus() (*GeminiStatus, error) {
	status := &GeminiStatus{}

	// 读取 .env
	envConfig, err := readGeminiEnv()
	if err != nil {
		// 文件不存在时返回默认状态
		return status, nil
	}

	status.HasAPIKey = envConfig["GEMINI_API_KEY"] != ""
	status.HasBaseURL = envConfig["GOOGLE_GEMINI_BASE_URL"] != ""
	status.Model = envConfig["GEMINI_MODEL"]

	// 读取 settings.json 判断认证类型
	settings, err := readGeminiSettings()
	if err == nil {
		if security, ok := settings["security"].(map[string]any); ok {
			if auth, ok := security["auth"].(map[string]any); ok {
				if selectedType, ok := auth["selectedType"].(string); ok {
					status.AuthType = GeminiAuthType(selectedType)
				}
			}
		}
	}

	// 判断是否启用
	status.Enabled = status.HasAPIKey || status.AuthType == GeminiAuthOAuth

	// 查找当前启用的供应商
	s.mu.Lock()
	for _, p := range s.providers {
		if p.Enabled {
			status.CurrentProvider = p.Name
			break
		}
	}
	s.mu.Unlock()

	return status, nil
}

// detectGeminiAuthType 检测供应商认证类型
func detectGeminiAuthType(provider *GeminiProvider) GeminiAuthType {
	// 优先级 1: 检查 partner_promotion_key
	switch strings.ToLower(provider.PartnerPromotionKey) {
	case "google-official":
		return GeminiAuthOAuth
	case "packycode":
		return GeminiAuthPackycode
	}

	// 优先级 2: 检查供应商名称
	nameLower := strings.ToLower(provider.Name)
	if nameLower == "google" || strings.HasPrefix(nameLower, "google ") {
		return GeminiAuthOAuth
	}

	// 优先级 3: 检查 PackyCode 关键词
	keywords := []string{"packycode", "packyapi", "packy"}
	for _, kw := range keywords {
		if strings.Contains(nameLower, kw) {
			return GeminiAuthPackycode
		}
		if strings.Contains(strings.ToLower(provider.WebsiteURL), kw) {
			return GeminiAuthPackycode
		}
		if strings.Contains(strings.ToLower(provider.BaseURL), kw) {
			return GeminiAuthPackycode
		}
	}

	// 默认：通用 API Key 认证
	return GeminiAuthGeneric
}

// getConfigDir 获取 CodeSwitch 配置目录
func getConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".code-switch")
}

// getGeminiDir 获取 Gemini 配置目录
func getGeminiDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini")
}

// getGeminiEnvPath 获取 .env 文件路径
func getGeminiEnvPath() string {
	return filepath.Join(getGeminiDir(), ".env")
}

// getGeminiSettingsPath 获取 settings.json 路径
func getGeminiSettingsPath() string {
	return filepath.Join(getGeminiDir(), "settings.json")
}

// getGeminiProvidersPath 获取供应商配置文件路径
func getGeminiProvidersPath() string {
	return filepath.Join(getConfigDir(), "gemini-providers.json")
}

// readGeminiEnv 读取 .env 文件
func readGeminiEnv() (map[string]string, error) {
	path := getGeminiEnvPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parseEnvFile(string(data)), nil
}

// parseEnvFile 解析 .env 文件内容
func parseEnvFile(content string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 解析 KEY=VALUE
		idx := strings.Index(line, "=")
		if idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			// 验证 key 有效性
			if key != "" && isValidEnvKey(key) {
				result[key] = value
			}
		}
	}

	return result
}

// isValidEnvKey 验证环境变量名是否有效
func isValidEnvKey(key string) bool {
	for _, c := range key {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

// writeGeminiEnv 写入 .env 文件（原子操作）
func writeGeminiEnv(envConfig map[string]string) error {
	dir := getGeminiDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// 构建 .env 内容
	var lines []string
	// 按固定顺序写入
	keys := []string{"GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY", "GEMINI_MODEL"}
	for _, key := range keys {
		if value, ok := envConfig[key]; ok && value != "" {
			lines = append(lines, fmt.Sprintf("%s=%s", key, value))
		}
	}
	// 写入其他键
	for key, value := range envConfig {
		if key != "GOOGLE_GEMINI_BASE_URL" && key != "GEMINI_API_KEY" && key != "GEMINI_MODEL" {
			if value != "" {
				lines = append(lines, fmt.Sprintf("%s=%s", key, value))
			}
		}
	}

	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}

	// 原子写入
	path := getGeminiEnvPath()
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// readGeminiSettings 读取 settings.json
func readGeminiSettings() (map[string]any, error) {
	path := getGeminiSettingsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// writeGeminiSettings 写入 settings.json（智能合并）
func writeGeminiSettings(newSettings map[string]any) error {
	dir := getGeminiDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path := getGeminiSettingsPath()

	// 读取现有配置
	existingSettings := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &existingSettings)
	}

	// 深度合并
	mergedSettings := deepMerge(existingSettings, newSettings)

	// 原子写入
	data, err := json.MarshalIndent(mergedSettings, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// deepMerge 深度合并两个 map
func deepMerge(dst, src map[string]any) map[string]any {
	result := make(map[string]any)

	// 复制 dst
	for k, v := range dst {
		result[k] = v
	}

	// 合并 src
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := result[k].(map[string]any); ok {
				result[k] = deepMerge(dstMap, srcMap)
			} else {
				result[k] = srcMap
			}
		} else {
			result[k] = v
		}
	}

	return result
}

// loadProviders 加载供应商配置
func (s *GeminiService) loadProviders() error {
	path := getGeminiProvidersPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.providers = []GeminiProvider{}
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &s.providers)
}

// saveProviders 保存供应商配置
func (s *GeminiService) saveProviders() error {
	path := getGeminiProvidersPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.providers, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// CreateProviderFromPreset 从预设创建供应商
func (s *GeminiService) CreateProviderFromPreset(presetName string, apiKey string) (*GeminiProvider, error) {
	var preset *GeminiPreset
	for i := range s.presets {
		if s.presets[i].Name == presetName {
			preset = &s.presets[i]
			break
		}
	}
	if preset == nil {
		return nil, fmt.Errorf("未找到预设 '%s'", presetName)
	}

	// 创建供应商
	provider := GeminiProvider{
		ID:                  fmt.Sprintf("gemini-%s-%d", strings.ToLower(strings.ReplaceAll(presetName, " ", "-")), len(s.providers)+1),
		Name:                preset.Name,
		WebsiteURL:          preset.WebsiteURL,
		APIKeyURL:           preset.APIKeyURL,
		BaseURL:             preset.BaseURL,
		APIKey:              apiKey,
		Model:               preset.Model,
		Description:         preset.Description,
		Category:            preset.Category,
		PartnerPromotionKey: preset.PartnerPromotionKey,
		Enabled:             false,
		EnvConfig:           make(map[string]string),
	}

	// 复制环境配置
	for k, v := range preset.EnvConfig {
		provider.EnvConfig[k] = v
	}

	// 设置 API Key
	if apiKey != "" {
		provider.EnvConfig["GEMINI_API_KEY"] = apiKey
	}

	// 添加供应商
	if err := s.AddProvider(provider); err != nil {
		return nil, err
	}

	return &provider, nil
}

// GeminiProxyStatus Gemini 代理状态
type GeminiProxyStatus struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
}

// ProxyStatus 获取代理状态
func (s *GeminiService) ProxyStatus() (*GeminiProxyStatus, error) {
	status := &GeminiProxyStatus{
		Enabled: false,
		BaseURL: buildProxyURL(s.relayAddr),
	}

	// 读取 .env 文件
	envConfig, err := readGeminiEnv()
	if err != nil {
		// 文件不存在时返回默认状态
		if os.IsNotExist(err) {
			return status, nil
		}
		return status, err
	}

	// 检查是否指向代理
	baseURL := envConfig["GOOGLE_GEMINI_BASE_URL"]
	proxyURL := buildProxyURL(s.relayAddr)
	status.Enabled = strings.EqualFold(baseURL, proxyURL)

	return status, nil
}

// EnableProxy 启用代理
func (s *GeminiService) EnableProxy() error {
	dir := getGeminiDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	envPath := getGeminiEnvPath()
	backupPath := envPath + ".cc-studio.backup"

	// 备份现有 .env（如果存在）
	if _, err := os.Stat(envPath); err == nil {
		content, readErr := os.ReadFile(envPath)
		if readErr != nil {
			return fmt.Errorf("读取现有 .env 失败: %w", readErr)
		}
		if err := os.WriteFile(backupPath, content, 0600); err != nil {
			return fmt.Errorf("备份 .env 失败: %w", err)
		}
	}

	// 读取现有配置（如果有）
	existingEnv, _ := readGeminiEnv()
	if existingEnv == nil {
		existingEnv = make(map[string]string)
	}

	// 设置代理 URL
	existingEnv["GOOGLE_GEMINI_BASE_URL"] = buildProxyURL(s.relayAddr)

	// 写入 .env
	if err := writeGeminiEnv(existingEnv); err != nil {
		return fmt.Errorf("写入 .env 失败: %w", err)
	}

	return nil
}

// DisableProxy 禁用代理
func (s *GeminiService) DisableProxy() error {
	envPath := getGeminiEnvPath()
	backupPath := envPath + ".cc-studio.backup"

	// 删除当前 .env
	if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 .env 失败: %w", err)
	}

	// 恢复备份（如果存在）
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Rename(backupPath, envPath); err != nil {
			return fmt.Errorf("恢复备份失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查备份文件失败: %w", err)
	}

	return nil
}

// buildProxyURL 构建代理 URL（包含 /gemini 前缀）
func buildProxyURL(relayAddr string) string {
	addr := strings.TrimSpace(relayAddr)
	if addr == "" {
		addr = ":18100"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr + "/gemini"
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return host + "/gemini"
}

// DuplicateProvider 复制供应商
func (s *GeminiService) DuplicateProvider(sourceID string) (*GeminiProvider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. 查找源供应商
	var source *GeminiProvider
	for i := range s.providers {
		if s.providers[i].ID == sourceID {
			source = &s.providers[i]
			break
		}
	}
	if source == nil {
		return nil, fmt.Errorf("未找到 ID 为 '%s' 的供应商", sourceID)
	}

	// 2. 生成新 ID（基于时间戳保证唯一性）
	newID := fmt.Sprintf("%s-copy-%d", sourceID, time.Now().Unix())

	// 3. 克隆配置（深拷贝）
	cloned := GeminiProvider{
		ID:                  newID,
		Name:                source.Name + " (副本)",
		WebsiteURL:          source.WebsiteURL,
		APIKeyURL:           source.APIKeyURL,
		BaseURL:             source.BaseURL,
		APIKey:              source.APIKey,
		Model:               source.Model,
		Description:         source.Description,
		Category:            source.Category,
		PartnerPromotionKey: source.PartnerPromotionKey,
		Enabled:             false, // 默认禁用，避免与源供应商冲突
	}

	// 4. 深拷贝 map（避免共享引用）
	if source.EnvConfig != nil {
		cloned.EnvConfig = make(map[string]string, len(source.EnvConfig))
		for k, v := range source.EnvConfig {
			cloned.EnvConfig[k] = v
		}
	}

	if source.SettingsConfig != nil {
		cloned.SettingsConfig = make(map[string]any, len(source.SettingsConfig))
		for k, v := range source.SettingsConfig {
			// 对于 map/slice 类型的值，需要深拷贝（简化处理，直接赋值）
			cloned.SettingsConfig[k] = v
		}
	}

	// 5. 添加到列表并保存
	s.providers = append(s.providers, cloned)
	if err := s.saveProviders(); err != nil {
		return nil, fmt.Errorf("保存副本失败: %w", err)
	}

	return &cloned, nil
}
