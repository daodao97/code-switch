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

// CliConfigService CLI 配置管理服务
// 管理 Claude Code、Codex、Gemini 的 CLI 配置文件
type CliConfigService struct {
	relayAddr string
}

// NewCliConfigService 创建 CLI 配置服务
func NewCliConfigService(relayAddr string) *CliConfigService {
	return &CliConfigService{relayAddr: relayAddr}
}

// CLIPlatform CLI 平台类型
type CLIPlatform string

const (
	PlatformClaude CLIPlatform = "claude"
	PlatformCodex  CLIPlatform = "codex"
	PlatformGemini CLIPlatform = "gemini"
)

// CLIConfigField 配置字段信息
type CLIConfigField struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Locked   bool   `json:"locked"`
	Hint     string `json:"hint,omitempty"`
	Type     string `json:"type"` // "string", "boolean", "object"
	Required bool   `json:"required,omitempty"`
}

// CLIConfigFile 配置文件预览（用于前端显示原始内容）
type CLIConfigFile struct {
	Path    string `json:"path"`
	Format  string `json:"format,omitempty"` // "json", "toml", "env"
	Content string `json:"content"`
}

// CLIConfig CLI 配置数据
type CLIConfig struct {
	Platform     CLIPlatform               `json:"platform"`
	Fields       []CLIConfigField          `json:"fields"`
	RawContent   string                    `json:"rawContent,omitempty"`   // 原始文件内容（用于高级编辑）
	RawFiles     []CLIConfigFile           `json:"rawFiles,omitempty"`     // 多文件内容预览
	ConfigFormat string                    `json:"configFormat,omitempty"` // "json" 或 "toml"
	EnvContent   map[string]string         `json:"envContent,omitempty"`   // Gemini .env 内容
	FilePath     string                    `json:"filePath,omitempty"`     // 配置文件路径
	Editable     map[string]interface{}    `json:"editable,omitempty"`     // 可编辑字段的当前值
}

// CLITemplate CLI 配置模板
type CLITemplate struct {
	Template        map[string]interface{} `json:"template"`
	IsGlobalDefault bool                   `json:"isGlobalDefault"`
}

// CLITemplates 所有平台的模板存储
type CLITemplates struct {
	Claude CLITemplate `json:"claude"`
	Codex  CLITemplate `json:"codex"`
	Gemini CLITemplate `json:"gemini"`
}

// getTemplatesPath 获取模板存储路径
func (s *CliConfigService) getTemplatesPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".code-switch", "cli-templates.json")
}

// GetConfig 获取指定平台的 CLI 配置
func (s *CliConfigService) GetConfig(platform string) (*CLIConfig, error) {
	p := CLIPlatform(platform)
	switch p {
	case PlatformClaude:
		return s.getClaudeConfig()
	case PlatformCodex:
		return s.getCodexConfig()
	case PlatformGemini:
		return s.getGeminiConfig()
	default:
		return nil, fmt.Errorf("不支持的平台: %s", platform)
	}
}

// SaveConfig 保存 CLI 配置
func (s *CliConfigService) SaveConfig(platform string, editable map[string]interface{}) error {
	p := CLIPlatform(platform)
	switch p {
	case PlatformClaude:
		return s.saveClaudeConfig(editable)
	case PlatformCodex:
		return s.saveCodexConfig(editable)
	case PlatformGemini:
		return s.saveGeminiConfig(editable)
	default:
		return fmt.Errorf("不支持的平台: %s", platform)
	}
}

// GetTemplate 获取指定平台的全局模板
func (s *CliConfigService) GetTemplate(platform string) (*CLITemplate, error) {
	templates, err := s.loadTemplates()
	if err != nil {
		return nil, err
	}

	switch CLIPlatform(platform) {
	case PlatformClaude:
		return &templates.Claude, nil
	case PlatformCodex:
		return &templates.Codex, nil
	case PlatformGemini:
		return &templates.Gemini, nil
	default:
		return nil, fmt.Errorf("不支持的平台: %s", platform)
	}
}

// SetTemplate 设置指定平台的全局模板
func (s *CliConfigService) SetTemplate(platform string, template map[string]interface{}, isGlobalDefault bool) error {
	templates, err := s.loadTemplates()
	if err != nil {
		// 如果文件不存在，创建新的模板
		templates = &CLITemplates{}
	}

	tpl := CLITemplate{
		Template:        template,
		IsGlobalDefault: isGlobalDefault,
	}

	switch CLIPlatform(platform) {
	case PlatformClaude:
		templates.Claude = tpl
	case PlatformCodex:
		templates.Codex = tpl
	case PlatformGemini:
		templates.Gemini = tpl
	default:
		return fmt.Errorf("不支持的平台: %s", platform)
	}

	return s.saveTemplates(templates)
}

// GetLockedFields 获取指定平台的锁定字段列表
func (s *CliConfigService) GetLockedFields(platform string) []string {
	switch CLIPlatform(platform) {
	case PlatformClaude:
		return []string{"env.ANTHROPIC_BASE_URL", "env.ANTHROPIC_AUTH_TOKEN"}
	case PlatformCodex:
		return []string{"model_provider", "model_providers.code-switch.base_url", "model_providers.code-switch.env_key"}
	case PlatformGemini:
		return []string{"GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY"}
	default:
		return []string{}
	}
}

// RestoreDefault 恢复默认配置
func (s *CliConfigService) RestoreDefault(platform string) error {
	p := CLIPlatform(platform)

	// 从备份恢复
	var configPath string
	switch p {
	case PlatformClaude:
		home, _ := os.UserHomeDir()
		configPath = filepath.Join(home, ".claude", "settings.json")
	case PlatformCodex:
		home, _ := os.UserHomeDir()
		configPath = filepath.Join(home, ".codex", "config.toml")
	case PlatformGemini:
		home, _ := os.UserHomeDir()
		configPath = filepath.Join(home, ".gemini", ".env")
	default:
		return fmt.Errorf("不支持的平台: %s", platform)
	}

	// 查找最新的备份文件（支持 *.bak.<timestamp> 格式）
	backupPath, err := FindLatestBackup(configPath)
	if err != nil {
		// 尝试兼容旧格式的备份文件
		switch p {
		case PlatformCodex:
			legacy := filepath.Join(filepath.Dir(configPath), "cc-studio.back.config.toml")
			if FileExists(legacy) {
				backupPath, err = legacy, nil
			}
		case PlatformGemini:
			legacy := configPath + ".code-switch.backup"
			if FileExists(legacy) {
				backupPath, err = legacy, nil
			}
		}
	}
	if err != nil {
		return err
	}

	return RestoreBackup(backupPath, configPath)
}

// baseURL 获取代理 URL
func (s *CliConfigService) baseURL() string {
	addr := strings.TrimSpace(s.relayAddr)
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

// geminiBaseURL 获取 Gemini 代理 URL（包含 /gemini 前缀）
func (s *CliConfigService) geminiBaseURL() string {
	return s.baseURL() + "/gemini"
}

// ========== Claude 配置操作 ==========

func (s *CliConfigService) getClaudeConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

func (s *CliConfigService) getClaudeConfig() (*CLIConfig, error) {
	configPath := s.getClaudeConfigPath()
	config := &CLIConfig{
		Platform:     PlatformClaude,
		ConfigFormat: "json",
		FilePath:     configPath,
		Fields:       []CLIConfigField{},
		Editable:     make(map[string]interface{}),
	}

	// 读取现有配置
	var data map[string]interface{}
	if content, err := os.ReadFile(configPath); err == nil {
		raw := string(content)
		config.RawContent = raw
		config.RawFiles = append(config.RawFiles, CLIConfigFile{
			Path:    configPath,
			Format:  "json",
			Content: raw,
		})
		if err := json.Unmarshal(content, &data); err != nil {
			return nil, fmt.Errorf("解析 Claude 配置失败: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("读取 Claude 配置失败: %w", err)
	}

	// 构建字段列表
	baseURL := s.baseURL()

	// 锁定字段
	config.Fields = append(config.Fields,
		CLIConfigField{
			Key:    "env.ANTHROPIC_BASE_URL",
			Value:  baseURL,
			Locked: true,
			Hint:   "由代理管理，指向本地代理服务",
			Type:   "string",
		},
		CLIConfigField{
			Key:    "env.ANTHROPIC_AUTH_TOKEN",
			Value:  "code-switch",
			Locked: true,
			Hint:   "代理认证令牌",
			Type:   "string",
		},
	)

	// 可编辑字段
	env, _ := data["env"].(map[string]interface{})

	model := ""
	if m, ok := data["model"].(string); ok {
		model = m
	}
	config.Fields = append(config.Fields, CLIConfigField{
		Key:    "model",
		Value:  model,
		Locked: false,
		Type:   "string",
	})
	config.Editable["model"] = model

	alwaysThinking := false
	if at, ok := data["alwaysThinkingEnabled"].(bool); ok {
		alwaysThinking = at
	}
	config.Fields = append(config.Fields, CLIConfigField{
		Key:    "alwaysThinkingEnabled",
		Value:  fmt.Sprintf("%v", alwaysThinking),
		Locked: false,
		Type:   "boolean",
	})
	config.Editable["alwaysThinkingEnabled"] = alwaysThinking

	plugins := make(map[string]interface{})
	if ep, ok := data["enabledPlugins"].(map[string]interface{}); ok {
		plugins = ep
	}
	pluginsJSON, _ := json.Marshal(plugins)
	config.Fields = append(config.Fields, CLIConfigField{
		Key:    "enabledPlugins",
		Value:  string(pluginsJSON),
		Locked: false,
		Type:   "object",
	})
	config.Editable["enabledPlugins"] = plugins

	// 检查是否有其他未知的 env 变量（排除锁定的）
	if env != nil {
		for k, v := range env {
			if k != "ANTHROPIC_BASE_URL" && k != "ANTHROPIC_AUTH_TOKEN" {
				config.Fields = append(config.Fields, CLIConfigField{
					Key:    "env." + k,
					Value:  fmt.Sprintf("%v", v),
					Locked: false,
					Type:   "string",
				})
				if config.Editable["env"] == nil {
					config.Editable["env"] = make(map[string]interface{})
				}
				config.Editable["env"].(map[string]interface{})[k] = v
			}
		}
	}

	return config, nil
}

func (s *CliConfigService) saveClaudeConfig(editable map[string]interface{}) error {
	configPath := s.getClaudeConfigPath()

	// 读取现有配置（保留用户的其他设置）
	var data map[string]interface{}
	if content, err := os.ReadFile(configPath); err == nil {
		// 仅当文件非空时解析
		if len(content) > 0 {
			if err := json.Unmarshal(content, &data); err != nil {
				// JSON 解析失败，使用空配置继续（后续会创建备份）
				fmt.Printf("[警告] settings.json 格式无效，将使用空配置: %v\n", err)
			}
		}
	}
	if data == nil {
		data = make(map[string]interface{})
	}

	// 创建备份
	if _, err := CreateBackup(configPath); err != nil {
		// 备份失败不阻止保存，只记录警告
		fmt.Printf("创建备份失败: %v\n", err)
	}

	// 确保 env 存在并设置锁定字段
	env, ok := data["env"].(map[string]interface{})
	if !ok {
		env = make(map[string]interface{})
	}
	env["ANTHROPIC_BASE_URL"] = s.baseURL()
	env["ANTHROPIC_AUTH_TOKEN"] = "code-switch"
	data["env"] = env

	// 更新可编辑字段
	if model, ok := editable["model"].(string); ok {
		data["model"] = model
	}
	if alwaysThinking, ok := editable["alwaysThinkingEnabled"].(bool); ok {
		data["alwaysThinkingEnabled"] = alwaysThinking
	}
	if plugins, ok := editable["enabledPlugins"].(map[string]interface{}); ok {
		data["enabledPlugins"] = plugins
	}

	// 处理自定义 env 变量
	if customEnv, ok := editable["env"].(map[string]interface{}); ok {
		for k, v := range customEnv {
			if k != "ANTHROPIC_BASE_URL" && k != "ANTHROPIC_AUTH_TOKEN" {
				env[k] = v
			}
		}
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(configPath)); err != nil {
		return err
	}

	// 原子写入
	return AtomicWriteJSON(configPath, data)
}

// ========== Codex 配置操作 ==========

func (s *CliConfigService) getCodexConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "config.toml")
}

func (s *CliConfigService) getCodexAuthPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "auth.json")
}

func (s *CliConfigService) getCodexConfig() (*CLIConfig, error) {
	configPath := s.getCodexConfigPath()
	config := &CLIConfig{
		Platform:     PlatformCodex,
		ConfigFormat: "toml",
		FilePath:     configPath,
		Fields:       []CLIConfigField{},
		Editable:     make(map[string]interface{}),
	}

	// 读取现有配置
	var data map[string]interface{}
	if content, err := os.ReadFile(configPath); err == nil {
		raw := string(content)
		config.RawContent = raw
		config.RawFiles = append(config.RawFiles, CLIConfigFile{
			Path:    configPath,
			Format:  "toml",
			Content: raw,
		})
		if err := toml.Unmarshal(content, &data); err != nil {
			return nil, fmt.Errorf("解析 Codex 配置失败: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("读取 Codex 配置失败: %w", err)
	}

	// 读取 auth.json 预览
	authPath := s.getCodexAuthPath()
	if authContent, err := os.ReadFile(authPath); err == nil {
		config.RawFiles = append(config.RawFiles, CLIConfigFile{
			Path:    authPath,
			Format:  "json",
			Content: string(authContent),
		})
	}

	baseURL := s.baseURL()

	// 锁定字段
	config.Fields = append(config.Fields,
		CLIConfigField{
			Key:    "model_provider",
			Value:  "code-switch",
			Locked: true,
			Hint:   "代理提供商标识",
			Type:   "string",
		},
		CLIConfigField{
			Key:    "model_providers.code-switch.base_url",
			Value:  baseURL,
			Locked: true,
			Hint:   "由代理管理，指向本地代理服务",
			Type:   "string",
		},
	)

	// 可编辑字段
	model := "gpt-5-codex"
	if m, ok := data["model"].(string); ok {
		model = m
	}
	config.Fields = append(config.Fields, CLIConfigField{
		Key:    "model",
		Value:  model,
		Locked: false,
		Type:   "string",
	})
	config.Editable["model"] = model

	reasoningEffort := "xhigh"
	if re, ok := data["model_reasoning_effort"].(string); ok {
		reasoningEffort = re
	}
	config.Fields = append(config.Fields, CLIConfigField{
		Key:    "model_reasoning_effort",
		Value:  reasoningEffort,
		Locked: false,
		Type:   "string",
	})
	config.Editable["model_reasoning_effort"] = reasoningEffort

	disableStorage := true
	if ds, ok := data["disable_response_storage"].(bool); ok {
		disableStorage = ds
	}
	config.Fields = append(config.Fields, CLIConfigField{
		Key:    "disable_response_storage",
		Value:  fmt.Sprintf("%v", disableStorage),
		Locked: false,
		Type:   "boolean",
	})
	config.Editable["disable_response_storage"] = disableStorage

	return config, nil
}

func (s *CliConfigService) saveCodexConfig(editable map[string]interface{}) error {
	configPath := s.getCodexConfigPath()

	// 读取现有配置（保留用户的其他设置）
	var raw map[string]interface{}
	if content, err := os.ReadFile(configPath); err == nil {
		// 仅当文件非空时解析
		if len(content) > 0 {
			if err := toml.Unmarshal(content, &raw); err != nil {
				// TOML 解析失败，使用空配置继续（后续会创建备份）
				fmt.Printf("[警告] config.toml 格式无效，将使用空配置: %v\n", err)
			}
		}
	}
	if raw == nil {
		raw = make(map[string]interface{})
	}

	// 创建备份
	if _, err := CreateBackup(configPath); err != nil {
		fmt.Printf("创建备份失败: %v\n", err)
	}

	// 设置锁定字段
	raw["model_provider"] = "code-switch"
	raw["preferred_auth_method"] = "apikey"

	// 确保 model_providers.code-switch 存在
	modelProviders, ok := raw["model_providers"].(map[string]interface{})
	if !ok {
		modelProviders = make(map[string]interface{})
	}
	provider, ok := modelProviders["code-switch"].(map[string]interface{})
	if !ok {
		provider = make(map[string]interface{})
	}
	provider["name"] = "code-switch"
	provider["base_url"] = s.baseURL()
	provider["wire_api"] = "responses"
	provider["requires_openai_auth"] = false
	modelProviders["code-switch"] = provider
	raw["model_providers"] = modelProviders

	// 更新可编辑字段
	if model, ok := editable["model"].(string); ok {
		raw["model"] = model
	}
	if reasoningEffort, ok := editable["model_reasoning_effort"].(string); ok {
		raw["model_reasoning_effort"] = reasoningEffort
	}
	if disableStorage, ok := editable["disable_response_storage"].(bool); ok {
		raw["disable_response_storage"] = disableStorage
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(configPath)); err != nil {
		return err
	}

	// 序列化 TOML
	tomlData, err := toml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("序列化 TOML 失败: %w", err)
	}

	// 清理多余的 [model_providers] 头
	cleaned := stripModelProvidersHeader(tomlData)

	// 原子写入
	return AtomicWriteBytes(configPath, cleaned)
}

// ========== Gemini 配置操作 ==========

func (s *CliConfigService) getGeminiEnvPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", ".env")
}

func (s *CliConfigService) getGeminiConfig() (*CLIConfig, error) {
	envPath := s.getGeminiEnvPath()
	config := &CLIConfig{
		Platform:     PlatformGemini,
		ConfigFormat: "env",
		FilePath:     envPath,
		Fields:       []CLIConfigField{},
		Editable:     make(map[string]interface{}),
		EnvContent:   make(map[string]string),
	}

	// 读取 .env 文件
	if content, err := os.ReadFile(envPath); err == nil {
		raw := string(content)
		config.RawContent = raw
		config.RawFiles = append(config.RawFiles, CLIConfigFile{
			Path:    envPath,
			Format:  "env",
			Content: raw,
		})
		config.EnvContent = parseEnvFile(raw)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("读取 Gemini .env 失败: %w", err)
	}

	baseURL := s.geminiBaseURL()

	// 锁定字段（如果启用了代理）
	config.Fields = append(config.Fields,
		CLIConfigField{
			Key:    "GOOGLE_GEMINI_BASE_URL",
			Value:  baseURL,
			Locked: true,
			Hint:   "由代理管理，指向本地代理服务",
			Type:   "string",
		},
	)

	// API Key (锁定字段，由系统管理)
	apiKey := config.EnvContent["GEMINI_API_KEY"]
	config.Fields = append(config.Fields, CLIConfigField{
		Key:    "GEMINI_API_KEY",
		Value:  apiKey,
		Locked: true,
		Hint:   "由系统管理，请勿手动修改",
		Type:   "string",
	})

	model := config.EnvContent["GEMINI_MODEL"]
	if model == "" {
		model = "gemini-3-pro-preview"
	}
	config.Fields = append(config.Fields, CLIConfigField{
		Key:    "GEMINI_MODEL",
		Value:  model,
		Locked: false,
		Type:   "string",
	})
	config.Editable["GEMINI_MODEL"] = model

	// 其他自定义环境变量
	for k, v := range config.EnvContent {
		if k != "GOOGLE_GEMINI_BASE_URL" && k != "GEMINI_API_KEY" && k != "GEMINI_MODEL" {
			config.Fields = append(config.Fields, CLIConfigField{
				Key:    k,
				Value:  v,
				Locked: false,
				Type:   "string",
			})
			config.Editable[k] = v
		}
	}

	return config, nil
}

func (s *CliConfigService) saveGeminiConfig(editable map[string]interface{}) error {
	envPath := s.getGeminiEnvPath()

	// 读取现有内容
	envMap := make(map[string]string)
	if content, err := os.ReadFile(envPath); err == nil {
		envMap = parseEnvFile(string(content))
	}

	// 创建备份
	if _, err := CreateBackup(envPath); err != nil {
		fmt.Printf("创建备份失败: %v\n", err)
	}

	// 设置锁定字段
	envMap["GOOGLE_GEMINI_BASE_URL"] = s.geminiBaseURL()

	// 更新可编辑字段
	for k, v := range editable {
		if str, ok := v.(string); ok {
			// 不允许覆盖锁定字段
			if k != "GOOGLE_GEMINI_BASE_URL" && k != "GEMINI_API_KEY" {
				envMap[k] = str
			}
		}
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(envPath)); err != nil {
		return err
	}

	// 序列化为 .env 格式
	content := serializeEnvFile(envMap)

	// 原子写入
	return AtomicWriteText(envPath, content)
}

// ========== 模板管理 ==========

func (s *CliConfigService) loadTemplates() (*CLITemplates, error) {
	path := s.getTemplatesPath()
	var templates CLITemplates

	if err := ReadJSONFile(path, &templates); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 返回空模板
			return &CLITemplates{}, nil
		}
		return nil, err
	}

	return &templates, nil
}

func (s *CliConfigService) saveTemplates(templates *CLITemplates) error {
	path := s.getTemplatesPath()
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return AtomicWriteJSON(path, templates)
}

// ========== 辅助函数 ==========

// serializeEnvFile 将 map 序列化为 .env 格式
func serializeEnvFile(envMap map[string]string) string {
	var lines []string

	// 按键排序以保证输出稳定
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	// 简单排序
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", key, envMap[key]))
	}

	return strings.Join(lines, "\n")
}

// 注意: parseEnvFile 和 isValidEnvKey 已在 geminiservice.go 中定义
