package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// CliConfigService CLI 配置管理服务
// 管理 Claude Code、Codex、Gemini 的 CLI 配置文件
type CliConfigService struct {
	relayAddr string
	homeDir   string // 缓存的用户家目录（已校验）
	homeErr   error  // 家目录获取错误
}

// NewCliConfigService 创建 CLI 配置服务
func NewCliConfigService(relayAddr string) *CliConfigService {
	home, err := getUserHomeDir()
	return &CliConfigService{
		relayAddr: relayAddr,
		homeDir:   home,
		homeErr:   err,
	}
}

// requireHome 校验家目录是否可用
func (s *CliConfigService) requireHome() error {
	if s.homeErr != nil {
		return fmt.Errorf("无法获取用户家目录: %w", s.homeErr)
	}
	if s.homeDir == "" || s.homeDir == "." || !filepath.IsAbs(s.homeDir) {
		return fmt.Errorf("无法获取用户家目录: homeDir 未初始化或无效")
	}
	return nil
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

// CLIConfigSnapshots CLI 配置快照（用于前端对比：当前 vs 预览）
type CLIConfigSnapshots struct {
	CurrentFiles []CLIConfigFile `json:"currentFiles"`
	PreviewFiles []CLIConfigFile `json:"previewFiles"`
	Mode         string          `json:"mode"` // "proxy" | "direct"
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
	return filepath.Join(s.homeDir, ".code-switch", "cli-templates.json")
}

// GetConfig 获取指定平台的 CLI 配置
func (s *CliConfigService) GetConfig(platform string) (*CLIConfig, error) {
	if err := s.requireHome(); err != nil {
		return nil, err
	}

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

// GetConfigSnapshots 获取指定平台的配置快照，用于前端展示"当前(磁盘)"与"预览(激活后)"对比。
// 这是纯 dry-run 接口：不会对任何文件进行写入。
//
// previewMode 参数：
//   - "current": Preview = Current（不做任何注入，适用于新建供应商空输入）
//   - "direct": 模拟直连应用 ApplySingleProvider() 的写入结果
//   - "proxy": 模拟启用代理 EnableProxy() 的写入结果
//   - "" (空字符串): 兼容旧逻辑，若 apiUrl/apiKey 任一非空则为 direct，否则为 proxy
func (s *CliConfigService) GetConfigSnapshots(platform string, apiUrl string, apiKey string, previewMode string) (*CLIConfigSnapshots, error) {
	if err := s.requireHome(); err != nil {
		return nil, err
	}

	p := CLIPlatform(platform)

	readText := func(path string) (string, error) {
		content, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", nil
			}
			return "", err
		}
		return string(content), nil
	}

	// 解析 previewMode 参数
	// effectiveMode 取值: "current", "direct", "proxy"
	var effectiveMode string
	previewModeTrim := strings.ToLower(strings.TrimSpace(previewMode))
	switch previewModeTrim {
	case "":
		// 兼容旧逻辑：任一非空 => direct，否则 => proxy
		if strings.TrimSpace(apiUrl) != "" || strings.TrimSpace(apiKey) != "" {
			effectiveMode = "direct"
		} else {
			effectiveMode = "proxy"
		}
	case "current", "direct", "proxy":
		effectiveMode = previewModeTrim
	default:
		return nil, fmt.Errorf("无效的 previewMode: %s（允许值: current, direct, proxy）", previewMode)
	}

	// 用于旧代码兼容的布尔标志
	previewDirect := effectiveMode == "direct"

	switch p {
	case PlatformClaude:
		configPath := s.getClaudeConfigPath()

		currentContent, err := readText(configPath)
		if err != nil {
			return nil, fmt.Errorf("读取 Claude 配置失败: %w", err)
		}

		currentFiles := []CLIConfigFile{
			{Path: configPath, Format: "json", Content: currentContent},
		}

		// 计算当前模式：是否指向本地代理
		currentMode := "direct"
		if strings.TrimSpace(currentContent) != "" {
			var payload map[string]any
			if err := json.Unmarshal([]byte(currentContent), &payload); err == nil {
				env, _ := payload["env"].(map[string]any)
				if env != nil {
					baseURLVal := anyToString(env["ANTHROPIC_BASE_URL"])
					enabled := strings.EqualFold(
						strings.TrimSuffix(strings.TrimSpace(baseURLVal), "/"),
						strings.TrimSuffix(strings.TrimSpace(s.baseURL()), "/"),
					)
					if enabled {
						currentMode = "proxy"
					}
				}
			}
		}

		// current 模式：Preview = Current（不做任何注入）
		if effectiveMode == "current" {
			// 深拷贝 currentFiles 避免引用共享
			previewFiles := make([]CLIConfigFile, len(currentFiles))
			copy(previewFiles, currentFiles)
			return &CLIConfigSnapshots{
				CurrentFiles: currentFiles,
				PreviewFiles: previewFiles,
				Mode:         currentMode,
			}, nil
		}

		// 构造预览：最小侵入，仅更新锁定字段
		previewData := make(map[string]any)
		if strings.TrimSpace(currentContent) != "" {
			if err := json.Unmarshal([]byte(currentContent), &previewData); err != nil {
				previewData = make(map[string]any)
			}
		}
		env, _ := previewData["env"].(map[string]any)
		if env == nil {
			env = make(map[string]any)
		}
		if previewDirect {
			env["ANTHROPIC_BASE_URL"] = normalizeURLTrimSlash(apiUrl)
			env["ANTHROPIC_AUTH_TOKEN"] = apiKey
		} else {
			env["ANTHROPIC_BASE_URL"] = s.baseURL()
			env["ANTHROPIC_AUTH_TOKEN"] = "code-switch-r"
		}
		previewData["env"] = env

		previewBytes, err := json.MarshalIndent(previewData, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("序列化 Claude 预览配置失败: %w", err)
		}

		previewFiles := []CLIConfigFile{
			{Path: configPath, Format: "json", Content: string(previewBytes)},
		}

		return &CLIConfigSnapshots{
			CurrentFiles: currentFiles,
			PreviewFiles: previewFiles,
			Mode:         currentMode,
		}, nil

	case PlatformCodex:
		configPath := s.getCodexConfigPath()
		authPath := s.getCodexAuthPath()

		currentConfig, err := readText(configPath)
		if err != nil {
			return nil, fmt.Errorf("读取 Codex 配置失败: %w", err)
		}
		currentAuth, err := readText(authPath)
		if err != nil {
			return nil, fmt.Errorf("读取 Codex 认证文件失败: %w", err)
		}

		currentFiles := []CLIConfigFile{
			{Path: configPath, Format: "toml", Content: currentConfig},
			{Path: authPath, Format: "json", Content: currentAuth},
		}

		// 计算当前模式：是否指向本地代理
		// 向后兼容：同时检查 code-switch-r（新）和 code-switch（旧）两个 key
		currentMode := "direct"
		if strings.TrimSpace(currentConfig) != "" {
			var cfg codexConfig
			if err := toml.Unmarshal([]byte(currentConfig), &cfg); err == nil {
				proxyKeys := []string{codexProviderKey, "code-switch"}
				for _, key := range proxyKeys {
					provider, ok := cfg.ModelProviders[key]
					if ok && strings.EqualFold(cfg.ModelProvider, key) && strings.EqualFold(provider.BaseURL, s.baseURL()) {
						currentMode = "proxy"
						break
					}
				}
			}
		}

		// current 模式：Preview = Current（不做任何注入）
		if effectiveMode == "current" {
			// 深拷贝 currentFiles 避免引用共享
			previewFiles := make([]CLIConfigFile, len(currentFiles))
			copy(previewFiles, currentFiles)
			return &CLIConfigSnapshots{
				CurrentFiles: currentFiles,
				PreviewFiles: previewFiles,
				Mode:         currentMode,
			}, nil
		}

		// 解析现有 TOML
		raw := make(map[string]any)
		if strings.TrimSpace(currentConfig) != "" {
			if err := toml.Unmarshal([]byte(currentConfig), &raw); err != nil {
				raw = make(map[string]any)
			}
		}

		// 解析现有 auth.json（用于 proxy 模式保留其他字段）
		authPayload := make(map[string]any)
		if strings.TrimSpace(currentAuth) != "" {
			if err := json.Unmarshal([]byte(currentAuth), &authPayload); err != nil {
				authPayload = make(map[string]any)
			}
		}

		if previewDirect {
			// 复用 provider 快照推导 providerKey
			providerKey := "preview-provider"
			if providers, err := loadProviderSnapshot("codex"); err == nil {
				for _, p := range providers {
					if urlsEqualFold(p.APIURL, apiUrl) && p.APIKey == apiKey {
						providerKey = sanitizeProviderKey(p.Name, int(p.ID))
						break
					}
				}
			}

			raw["preferred_auth_method"] = "apikey"
			raw["model_provider"] = providerKey

			modelProviders := ensureTomlTable(raw, "model_providers")
			providerCfg := ensureProviderTable(modelProviders, providerKey)
			providerCfg["name"] = providerKey
			providerCfg["base_url"] = normalizeURLTrimSlash(apiUrl)
			providerCfg["wire_api"] = "responses"
			providerCfg["requires_openai_auth"] = false
			modelProviders[providerKey] = providerCfg
			raw["model_providers"] = modelProviders

			// direct 模式：只保留 OPENAI_API_KEY（与 writeDirectApplyAuthFile 一致）
			authPayload = map[string]any{"OPENAI_API_KEY": apiKey}
		} else {
			raw["preferred_auth_method"] = "apikey"
			raw["model_provider"] = "code-switch-r"

			if _, exists := raw["model"]; !exists {
				raw["model"] = "gpt-5-codex"
			}

			modelProviders := ensureTomlTable(raw, "model_providers")
			providerCfg := ensureProviderTable(modelProviders, "code-switch-r")
			providerCfg["name"] = "code-switch-r"
			providerCfg["base_url"] = s.baseURL()
			providerCfg["wire_api"] = "responses"
			providerCfg["requires_openai_auth"] = false
			modelProviders["code-switch-r"] = providerCfg
			raw["model_providers"] = modelProviders

			// proxy 模式：保留其他字段，只更新 OPENAI_API_KEY（与 writeAuthFile 一致）
			authPayload["OPENAI_API_KEY"] = "code-switch-r"
		}

		tomlBytes, err := toml.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("序列化 Codex 预览配置失败: %w", err)
		}
		cleaned := stripModelProvidersHeader(tomlBytes)

		authBytes, err := json.MarshalIndent(authPayload, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("序列化 Codex auth 预览失败: %w", err)
		}

		previewFiles := []CLIConfigFile{
			{Path: configPath, Format: "toml", Content: string(cleaned)},
			{Path: authPath, Format: "json", Content: string(authBytes)},
		}

		return &CLIConfigSnapshots{
			CurrentFiles: currentFiles,
			PreviewFiles: previewFiles,
			Mode:         currentMode,
		}, nil

	case PlatformGemini:
		envPath := s.getGeminiEnvPath()
		currentEnv, err := readText(envPath)
		if err != nil {
			return nil, fmt.Errorf("读取 Gemini .env 失败: %w", err)
		}

		currentFiles := []CLIConfigFile{
			{Path: envPath, Format: "env", Content: currentEnv},
		}

		// 计算当前模式：是否指向本地代理
		currentMode := "direct"
		if strings.TrimSpace(currentEnv) != "" {
			envMap := parseEnvFile(currentEnv)
			if strings.EqualFold(strings.TrimSpace(envMap["GOOGLE_GEMINI_BASE_URL"]), strings.TrimSpace(s.geminiBaseURL())) {
				currentMode = "proxy"
			}
		}

		// current 模式：Preview = Current（不做任何注入）
		if effectiveMode == "current" {
			// 深拷贝 currentFiles 避免引用共享
			previewFiles := make([]CLIConfigFile, len(currentFiles))
			copy(previewFiles, currentFiles)
			return &CLIConfigSnapshots{
				CurrentFiles: currentFiles,
				PreviewFiles: previewFiles,
				Mode:         currentMode,
			}, nil
		}

		envMap := parseEnvFile(currentEnv)
		if envMap == nil {
			envMap = make(map[string]string)
		}

		if previewDirect {
			if strings.TrimSpace(apiUrl) != "" {
				envMap["GOOGLE_GEMINI_BASE_URL"] = strings.TrimSpace(apiUrl)
			} else {
				delete(envMap, "GOOGLE_GEMINI_BASE_URL")
			}
			if strings.TrimSpace(apiKey) != "" {
				envMap["GEMINI_API_KEY"] = strings.TrimSpace(apiKey)
			} else {
				delete(envMap, "GEMINI_API_KEY")
			}
		} else {
			envMap["GOOGLE_GEMINI_BASE_URL"] = s.geminiBaseURL()
			envMap["GEMINI_API_KEY"] = "code-switch-r"
		}

		previewFiles := []CLIConfigFile{
			{Path: envPath, Format: "env", Content: buildGeminiEnvContent(envMap)},
		}

		return &CLIConfigSnapshots{
			CurrentFiles: currentFiles,
			PreviewFiles: previewFiles,
			Mode:         currentMode,
		}, nil

	default:
		return nil, fmt.Errorf("不支持的平台: %s", platform)
	}
}

// SaveConfig 保存 CLI 配置
func (s *CliConfigService) SaveConfig(platform string, editable map[string]interface{}) error {
	if err := s.requireHome(); err != nil {
		return err
	}

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

// SaveConfigFileContent 保存指定配置文件内容（预览区高级编辑）
// 为避免越权写文件，只允许写入本服务管理的固定路径文件
func (s *CliConfigService) SaveConfigFileContent(platform string, filePath string, content string) error {
	if err := s.requireHome(); err != nil {
		return err
	}

	p := CLIPlatform(platform)
	cleaned := filepath.Clean(filePath)

	switch p {
	case PlatformClaude:
		expected := filepath.Clean(s.getClaudeConfigPath())
		if !samePath(cleaned, expected) {
			return fmt.Errorf("非法文件路径: %s", filePath)
		}
		return s.saveClaudeConfigContent(expected, content)
	case PlatformCodex:
		configPath := filepath.Clean(s.getCodexConfigPath())
		authPath := filepath.Clean(s.getCodexAuthPath())
		if samePath(cleaned, configPath) {
			return s.saveCodexConfigContent(configPath, content)
		}
		if samePath(cleaned, authPath) {
			return s.saveCodexAuthContent(authPath, content)
		}
		return fmt.Errorf("非法文件路径: %s", filePath)
	case PlatformGemini:
		envPath := filepath.Clean(s.getGeminiEnvPath())
		if !samePath(cleaned, envPath) {
			return fmt.Errorf("非法文件路径: %s", filePath)
		}
		return s.saveGeminiEnvContent(envPath, content)
	default:
		return fmt.Errorf("不支持的平台: %s", platform)
	}
}

// GetTemplate 获取指定平台的全局模板
func (s *CliConfigService) GetTemplate(platform string) (*CLITemplate, error) {
	if err := s.requireHome(); err != nil {
		return nil, err
	}

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
	if err := s.requireHome(); err != nil {
		return err
	}

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
		return []string{"model_provider", "preferred_auth_method", "model_providers.code-switch-r.base_url", "model_providers.code-switch-r.name", "model_providers.code-switch-r.wire_api"}
	case PlatformGemini:
		return []string{"GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY"}
	default:
		return []string{}
	}
}

// RestoreDefault 恢复默认配置
func (s *CliConfigService) RestoreDefault(platform string) error {
	if err := s.requireHome(); err != nil {
		return err
	}

	p := CLIPlatform(platform)

	// 从备份恢复
	var configPath string
	switch p {
	case PlatformClaude:
		configPath = s.getClaudeConfigPath()
	case PlatformCodex:
		configPath = s.getCodexConfigPath()
	case PlatformGemini:
		configPath = s.getGeminiEnvPath()
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
	return filepath.Join(s.homeDir, ".claude", "settings.json")
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
			Value:  "code-switch-r",
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
	env["ANTHROPIC_AUTH_TOKEN"] = "code-switch-r"
	data["env"] = env

	// 锁定字段列表（这些字段不允许用户覆盖）
	lockedFields := map[string]bool{
		"env.ANTHROPIC_BASE_URL":   true,
		"env.ANTHROPIC_AUTH_TOKEN": true,
	}

	// 合并用户编辑的所有字段（除了锁定字段）
	for k, v := range editable {
		// 跳过锁定字段
		if lockedFields[k] || lockedFields["env."+k] {
			continue
		}

		// 特殊处理 env：合并而不是覆盖
		if k == "env" {
			if customEnv, ok := v.(map[string]interface{}); ok {
				for ek, ev := range customEnv {
					if ek != "ANTHROPIC_BASE_URL" && ek != "ANTHROPIC_AUTH_TOKEN" {
						env[ek] = ev
					}
				}
			}
			continue
		}

		// 其他字段直接覆盖
		data[k] = v
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(configPath)); err != nil {
		return err
	}

	// 原子写入
	return AtomicWriteJSON(configPath, data)
}

// saveClaudeConfigContent 将预览区编辑的 settings.json 写入磁盘，并强制覆盖代理锁定字段
func (s *CliConfigService) saveClaudeConfigContent(configPath string, content string) error {
	data := make(map[string]interface{})
	// 空内容允许，视为从空配置开始
	if strings.TrimSpace(content) != "" {
		if err := json.Unmarshal([]byte(content), &data); err != nil {
			return fmt.Errorf("解析 Claude 配置失败: %w", err)
		}
	}
	if data == nil {
		data = make(map[string]interface{})
	}

	// 强制写入锁定字段
	env, _ := data["env"].(map[string]interface{})
	if env == nil {
		env = make(map[string]interface{})
	}
	env["ANTHROPIC_BASE_URL"] = s.baseURL()
	env["ANTHROPIC_AUTH_TOKEN"] = "code-switch-r"
	data["env"] = env

	// 创建备份（文件不存在时 CreateBackup 会返回空路径并忽略）
	if _, err := CreateBackup(configPath); err != nil {
		fmt.Printf("创建备份失败: %v\n", err)
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(configPath)); err != nil {
		return err
	}

	return AtomicWriteJSON(configPath, data)
}

// ========== Codex 配置操作 ==========

func (s *CliConfigService) getCodexConfigPath() string {
	return filepath.Join(s.homeDir, ".codex", "config.toml")
}

func (s *CliConfigService) getCodexAuthPath() string {
	return filepath.Join(s.homeDir, ".codex", "auth.json")
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
			Value:  "code-switch-r",
			Locked: true,
			Hint:   "代理提供商标识",
			Type:   "string",
		},
		CLIConfigField{
			Key:    "preferred_auth_method",
			Value:  "apikey",
			Locked: true,
			Hint:   "代理认证方式",
			Type:   "string",
		},
		CLIConfigField{
			Key:    "model_providers.code-switch-r.base_url",
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
	raw["model_provider"] = "code-switch-r"
	raw["preferred_auth_method"] = "apikey"

	// 确保 model_providers.code-switch-r 存在
	modelProviders, ok := raw["model_providers"].(map[string]interface{})
	if !ok {
		modelProviders = make(map[string]interface{})
	}
	provider, ok := modelProviders["code-switch-r"].(map[string]interface{})
	if !ok {
		provider = make(map[string]interface{})
	}
	provider["name"] = "code-switch-r"
	provider["base_url"] = s.baseURL()
	provider["wire_api"] = "responses"
	provider["requires_openai_auth"] = false
	modelProviders["code-switch-r"] = provider
	raw["model_providers"] = modelProviders

	// 锁定字段列表（这些字段不允许用户覆盖）
	lockedFields := map[string]bool{
		"model_provider":        true,
		"preferred_auth_method": true,
		"model_providers":       true,
	}

	// 合并用户编辑的所有字段（除了锁定字段）
	for k, v := range editable {
		// 跳过锁定字段（包括点号路径的嵌套键）
		if lockedFields[k] || strings.HasPrefix(k, "model_providers.") {
			continue
		}
		// 其他字段直接覆盖
		raw[k] = v
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

// saveCodexConfigContent 将预览区编辑的 config.toml 写入磁盘，并强制覆盖代理锁定字段
func (s *CliConfigService) saveCodexConfigContent(configPath string, content string) error {
	raw := make(map[string]interface{})
	// 空内容允许，视为从空配置开始
	if strings.TrimSpace(content) != "" {
		if err := toml.Unmarshal([]byte(content), &raw); err != nil {
			return fmt.Errorf("解析 Codex 配置失败: %w", err)
		}
	}
	if raw == nil {
		raw = make(map[string]interface{})
	}

	if _, err := CreateBackup(configPath); err != nil {
		fmt.Printf("创建备份失败: %v\n", err)
	}

	// 强制写入锁定字段
	raw["model_provider"] = "code-switch-r"
	raw["preferred_auth_method"] = "apikey"

	// 确保 model_providers.code-switch-r 存在并写入锁定字段
	modelProviders, ok := raw["model_providers"].(map[string]interface{})
	if !ok || modelProviders == nil {
		modelProviders = make(map[string]interface{})
	}
	provider, ok := modelProviders["code-switch-r"].(map[string]interface{})
	if !ok || provider == nil {
		provider = make(map[string]interface{})
	}
	provider["name"] = "code-switch-r"
	provider["base_url"] = s.baseURL()
	provider["wire_api"] = "responses"
	provider["requires_openai_auth"] = false
	modelProviders["code-switch-r"] = provider
	raw["model_providers"] = modelProviders

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(configPath)); err != nil {
		return err
	}

	tomlData, err := toml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("序列化 TOML 失败: %w", err)
	}
	cleaned := stripModelProvidersHeader(tomlData)
	return AtomicWriteBytes(configPath, cleaned)
}

// saveCodexAuthContent 保存 Codex auth.json（仅做 JSON 校验，不强制覆盖内容）
func (s *CliConfigService) saveCodexAuthContent(authPath string, content string) error {
	data := make(map[string]interface{})
	// 空内容允许（可用于清空/重建）
	if strings.TrimSpace(content) != "" {
		if err := json.Unmarshal([]byte(content), &data); err != nil {
			return fmt.Errorf("解析 Codex auth.json 失败: %w", err)
		}
	}
	if data == nil {
		data = make(map[string]interface{})
	}

	if _, err := CreateBackup(authPath); err != nil {
		fmt.Printf("创建备份失败: %v\n", err)
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(authPath)); err != nil {
		return err
	}

	return AtomicWriteJSON(authPath, data)
}

// ========== Gemini 配置操作 ==========

func (s *CliConfigService) getGeminiEnvPath() string {
	return filepath.Join(s.homeDir, ".gemini", ".env")
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

	// 读取现有内容（保留用户的其他设置）
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

	// 锁定字段列表（这些字段不允许用户覆盖）
	lockedFields := map[string]bool{
		"GOOGLE_GEMINI_BASE_URL": true,
		"GEMINI_API_KEY":         true,
	}

	// 合并用户编辑的所有字段（除了锁定字段）
	for k, v := range editable {
		// 跳过锁定字段
		if lockedFields[k] {
			continue
		}
		// 将值转换为字符串（.env 格式只支持字符串值）
		if str, ok := v.(string); ok {
			envMap[k] = str
		} else {
			// 对于非字符串类型，转换为字符串表示
			envMap[k] = fmt.Sprintf("%v", v)
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

// saveGeminiEnvContent 将预览区编辑的 .env 写入磁盘，并强制覆盖代理锁定字段
func (s *CliConfigService) saveGeminiEnvContent(envPath string, content string) error {
	envMap := parseEnvFile(content)

	// 强制写入锁定字段
	envMap["GOOGLE_GEMINI_BASE_URL"] = s.geminiBaseURL()

	// GEMINI_API_KEY 为系统锁定字段：优先保留磁盘中的现有值；不存在时写入占位值
	existingAPIKey := ""
	if oldContent, err := os.ReadFile(envPath); err == nil {
		oldMap := parseEnvFile(string(oldContent))
		existingAPIKey = oldMap["GEMINI_API_KEY"]
	}
	if existingAPIKey != "" {
		envMap["GEMINI_API_KEY"] = existingAPIKey
	} else if envMap["GEMINI_API_KEY"] == "" {
		envMap["GEMINI_API_KEY"] = "code-switch-r"
	}

	if _, err := CreateBackup(envPath); err != nil {
		fmt.Printf("创建备份失败: %v\n", err)
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(envPath)); err != nil {
		return err
	}

	// 原子写入
	return AtomicWriteText(envPath, serializeEnvFile(envMap))
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

// samePath 跨平台路径比较（Windows 大小写不敏感）
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// 注意: parseEnvFile 和 isValidEnvKey 已在 geminiservice.go 中定义
