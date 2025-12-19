package services

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	_ "modernc.org/sqlite" // SQLite driver
)

// stringMap 是一个宽容的 map[string]string 类型
// 在 JSON 反序列化时自动将数字、布尔等类型转为字符串
// 用于兼容旧版 cc-switch 配置中 env 值为数字的情况
type stringMap map[string]string

func (m *stringMap) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch t := v.(type) {
		case string:
			out[k] = t
		case float64:
			// JSON 中所有数字都是 float64，智能格式化（整数不带小数点）
			if t == float64(int64(t)) {
				out[k] = strconv.FormatInt(int64(t), 10)
			} else {
				out[k] = strconv.FormatFloat(t, 'f', -1, 64)
			}
		case bool:
			out[k] = strconv.FormatBool(t)
		case nil:
			out[k] = ""
		default:
			// 其他复杂类型转为 JSON 字符串
			b, _ := json.Marshal(t)
			out[k] = string(b)
		}
	}
	*m = out
	return nil
}

type ConfigImportStatus struct {
	ConfigExists         bool   `json:"config_exists"`
	ConfigPath           string `json:"config_path,omitempty"`
	PendingProviders     bool   `json:"pending_providers"`
	PendingMCP           bool   `json:"pending_mcp"`
	PendingProviderCount int    `json:"pending_provider_count"`
	PendingMCPCount      int    `json:"pending_mcp_count"`
}

type ConfigImportResult struct {
	Status            ConfigImportStatus `json:"status"`
	ImportedProviders int                `json:"imported_providers"`
	ImportedMCP       int                `json:"imported_mcp"`
}

type ImportService struct {
	providerService *ProviderService
	mcpService      *MCPService
}

func NewImportService(ps *ProviderService, ms *MCPService) *ImportService {
	return &ImportService{providerService: ps, mcpService: ms}
}

func (is *ImportService) Start() error { return nil }
func (is *ImportService) Stop() error  { return nil }

// IsFirstRun 检查是否首次使用（用于显示导入提示）
func (is *ImportService) IsFirstRun() bool {
	marker, err := firstRunMarkerPath()
	if err != nil {
		log.Printf("⚠️  cc-switch: 获取首次使用标记路径失败: %v", err)
		return true
	}
	if _, err := os.Stat(marker); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		log.Printf("⚠️  cc-switch: 检查首次使用标记失败: %v", err)
		return true
	}
	return false
}

// MarkFirstRunDone 标记首次使用已完成（不再显示导入提示）
func (is *ImportService) MarkFirstRunDone() error {
	marker, err := firstRunMarkerPath()
	if err != nil {
		log.Printf("⚠️  cc-switch: 获取首次使用标记路径失败: %v", err)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		log.Printf("⚠️  cc-switch: 创建首次使用标记目录失败: %v", err)
		return err
	}
	if err := os.WriteFile(marker, []byte("1"), 0644); err != nil {
		log.Printf("⚠️  cc-switch: 写入首次使用标记失败: %v", err)
		return err
	}
	log.Printf("✅ cc-switch: 首次使用标记已创建: %s", marker)
	return nil
}

func (is *ImportService) GetStatus() (ConfigImportStatus, error) {
	status := ConfigImportStatus{}
	// 填充配置文件路径，便于前端展示
	if path, err := ccSwitchConfigPath(); err == nil {
		status.ConfigPath = path
	}
	cfg, exists, err := loadCcSwitchConfig()
	if err != nil {
		return status, err
	}
	status.ConfigExists = exists
	if !exists || cfg == nil {
		return status, nil
	}
	return is.evaluateStatus(cfg)
}

// ImportFromPath 从指定路径导入 cc-switch 配置
func (is *ImportService) ImportFromPath(path string) (ConfigImportResult, error) {
	result := ConfigImportResult{}
	path = strings.TrimSpace(path)
	if path == "" {
		err := errors.New("cc-switch: 导入路径为空")
		log.Printf("⚠️  %v", err)
		return result, err
	}
	path = filepath.Clean(path)
	result.Status.ConfigPath = path

	cfg, exists, err := loadCcSwitchConfigFromPath(path)
	if err != nil {
		return result, err
	}
	result.Status.ConfigExists = exists
	if !exists || cfg == nil {
		return result, nil
	}
	pendingProviders, err := is.pendingProviders(cfg)
	if err != nil {
		return result, err
	}
	addedProviders, err := is.importProviders(cfg, pendingProviders)
	if err != nil {
		return result, err
	}
	result.ImportedProviders = addedProviders

	pendingServers, err := is.pendingMCPCandidates(cfg)
	if err != nil {
		return result, err
	}
	addedServers, err := is.importMCPServers(pendingServers)
	if err != nil {
		return result, err
	}
	result.ImportedMCP = addedServers

	status, err := is.evaluateStatus(cfg)
	if err != nil {
		return result, err
	}
	status.ConfigPath = path
	result.Status = status
	return result, nil
}

// ImportAll 从默认路径导入 cc-switch 配置
func (is *ImportService) ImportAll() (ConfigImportResult, error) {
	path, err := ccSwitchConfigPath()
	if err != nil {
		return ConfigImportResult{}, err
	}
	return is.ImportFromPath(path)
}

func (is *ImportService) evaluateStatus(cfg *ccSwitchConfig) (ConfigImportStatus, error) {
	status := ConfigImportStatus{ConfigExists: true}
	pendingProviders, err := is.pendingProviders(cfg)
	if err != nil {
		return status, err
	}
	providerCount := len(pendingProviders["claude"]) + len(pendingProviders["codex"])
	status.PendingProviders = providerCount > 0
	status.PendingProviderCount = providerCount

	pendingServers, err := is.pendingMCPCandidates(cfg)
	if err != nil {
		return status, err
	}
	status.PendingMCPCount = len(pendingServers)
	status.PendingMCP = status.PendingMCPCount > 0
	return status, nil
}

func loadCcSwitchConfig() (*ccSwitchConfig, bool, error) {
	path, err := ccSwitchConfigPath()
	if err != nil {
		log.Printf("⚠️  cc-switch: 获取配置路径失败: %v", err)
		return nil, false, err
	}
	return loadCcSwitchConfigFromPath(path)
}

func loadCcSwitchConfigFromPath(path string) (*ccSwitchConfig, bool, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		err := errors.New("cc-switch: 配置路径为空")
		log.Printf("⚠️  %v", err)
		return nil, false, err
	}

	// 检测是否为 SQLite 文件
	if isSQLiteFile(path) {
		log.Printf("ℹ️  cc-switch: 检测到 SQLite 数据库: %s", path)
		return loadCcSwitchConfigFromSQLite(path)
	}

	// JSON 文件处理（原有逻辑）
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("ℹ️  cc-switch: 配置文件不存在: %s", path)
			return nil, false, nil
		}
		log.Printf("⚠️  cc-switch: 读取配置文件失败: %s - %v", path, err)
		return nil, false, err
	}
	if len(data) == 0 {
		log.Printf("ℹ️  cc-switch: 配置文件为空: %s", path)
		return &ccSwitchConfig{}, true, nil
	}
	var cfg ccSwitchConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("⚠️  cc-switch: JSON 解析失败: %s - %v", path, err)
		return nil, true, err
	}
	log.Printf("✅ cc-switch: 配置文件加载成功: %s", path)
	return &cfg, true, nil
}

// isSQLiteFile 检测文件是否为 SQLite 数据库
// 必须同时满足：文件存在 + 文件头为 SQLite 魔数
func isSQLiteFile(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	// 检查文件头（SQLite 魔数: "SQLite format 3\x00"）
	header := make([]byte, 16)
	n, err := file.Read(header)
	if err != nil || n < 16 {
		return false
	}

	return bytes.HasPrefix(header, []byte("SQLite format 3"))
}

// loadCcSwitchConfigFromSQLite 从 SQLite 数据库加载 cc-switch 配置
func loadCcSwitchConfigFromSQLite(path string) (*ccSwitchConfig, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		log.Printf("⚠️  cc-switch: 打开 SQLite 失败: %v", err)
		return nil, true, err
	}
	defer db.Close()

	cfg := &ccSwitchConfig{
		Claude: ccProviderSection{Providers: map[string]ccProviderEntry{}},
		Codex:  ccProviderSection{Providers: map[string]ccProviderEntry{}},
		MCP: ccMCPSection{
			Claude: ccMCPPlatform{Servers: map[string]ccMCPServerEntry{}},
			Codex:  ccMCPPlatform{Servers: map[string]ccMCPServerEntry{}},
		},
	}

	// 1. 读取 providers
	if err := loadProvidersFromSQLite(db, cfg); err != nil {
		log.Printf("⚠️  cc-switch: 读取 providers 失败: %v", err)
		return nil, true, err
	}

	// 2. 读取 MCP servers
	if err := loadMCPServersFromSQLite(db, cfg); err != nil {
		log.Printf("⚠️  cc-switch: 读取 MCP servers 失败: %v", err)
		// MCP 失败不阻断，继续导入 providers
	}

	log.Printf("✅ cc-switch: SQLite 数据库加载成功: %s", path)
	return cfg, true, nil
}

// loadProvidersFromSQLite 从 SQLite 读取 providers 数据
func loadProvidersFromSQLite(db *sql.DB, cfg *ccSwitchConfig) error {
	// 读取 provider_endpoints 作为 URL 补充
	endpoints := make(map[string]string) // key: "app_type|provider_id" -> url
	epRows, err := db.Query(`SELECT provider_id, app_type, url FROM provider_endpoints`)
	if err == nil {
		defer epRows.Close()
		for epRows.Next() {
			var pid, appType, url string
			if err := epRows.Scan(&pid, &appType, &url); err == nil {
				url = strings.TrimSpace(url)
				if url != "" {
					key := strings.ToLower(appType) + "|" + pid
					endpoints[key] = url
				}
			}
		}
	}

	// 读取 providers
	rows, err := db.Query(`SELECT id, app_type, name, settings_config, COALESCE(website_url, '') FROM providers`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, appType, name, settingsJSON, website string
		if err := rows.Scan(&id, &appType, &name, &settingsJSON, &website); err != nil {
			log.Printf("⚠️  cc-switch: 扫描 provider 行失败: %v", err)
			continue
		}

		entry := ccProviderEntry{
			ID:         id,
			Name:       name,
			WebsiteURL: website,
			Settings: ccProviderSetting{
				Env:  stringMap{},
				Auth: stringMap{},
			},
		}

		// 解析 settings_config JSON
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(settingsJSON), &raw); err == nil {
			// 解析 env
			if env, ok := raw["env"].(map[string]interface{}); ok {
				for k, v := range env {
					entry.Settings.Env[k] = fmt.Sprint(v)
				}
			}
			// 解析 auth
			if auth, ok := raw["auth"].(map[string]interface{}); ok {
				for k, v := range auth {
					entry.Settings.Auth[k] = fmt.Sprint(v)
				}
			}
			// 解析 config (Codex TOML)
			if cfgStr, ok := raw["config"].(string); ok {
				entry.Settings.Config = cfgStr
			}
		}

		// 从 provider_endpoints 补充 URL
		kind := strings.ToLower(strings.TrimSpace(appType))
		if url := endpoints[kind+"|"+id]; url != "" {
			if kind == "claude" {
				// Claude: 补充 ANTHROPIC_BASE_URL
				if entry.Settings.Env["ANTHROPIC_BASE_URL"] == "" {
					entry.Settings.Env["ANTHROPIC_BASE_URL"] = url
				}
			} else if kind == "codex" {
				// Codex: 如果没有 Config，生成最小 TOML
				if entry.Settings.Config == "" {
					entry.Settings.Config = fmt.Sprintf(
						"model_provider = \"db\"\n[model_providers.db]\nbase_url = \"%s\"\nname = \"%s\"",
						url, name,
					)
				}
			}
		}

		// 添加到对应平台
		if kind == "codex" {
			cfg.Codex.Providers[id] = entry
		} else {
			cfg.Claude.Providers[id] = entry
		}
	}

	return rows.Err()
}

// loadMCPServersFromSQLite 从 SQLite 读取 MCP servers 数据
func loadMCPServersFromSQLite(db *sql.DB, cfg *ccSwitchConfig) error {
	rows, err := db.Query(`
		SELECT id, name, server_config,
		       COALESCE(description, ''), COALESCE(homepage, ''),
		       enabled_claude, enabled_codex
		FROM mcp_servers
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, name, serverConfigJSON, description, homepage string
		var enabledClaude, enabledCodex bool
		if err := rows.Scan(&id, &name, &serverConfigJSON, &description, &homepage, &enabledClaude, &enabledCodex); err != nil {
			log.Printf("⚠️  cc-switch: 扫描 MCP server 行失败: %v", err)
			continue
		}

		// 解析 server_config JSON
		var serverCfg ccMCPServerConfig
		if err := json.Unmarshal([]byte(serverConfigJSON), &serverCfg); err != nil {
			log.Printf("⚠️  cc-switch: 解析 MCP server_config 失败: %v", err)
			continue
		}

		entry := ccMCPServerEntry{
			ID:          id,
			Name:        name,
			Homepage:    homepage,
			Description: description,
			Server:      serverCfg,
		}

		// 根据启用状态添加到对应平台
		if enabledClaude {
			entry.Enabled = true
			cfg.MCP.Claude.Servers[name] = entry
		}
		if enabledCodex {
			entry.Enabled = true
			cfg.MCP.Codex.Servers[name] = entry
		}
		// 如果两个平台都未启用，默认添加到两个平台（保持可见性）
		if !enabledClaude && !enabledCodex {
			cfg.MCP.Claude.Servers[name] = entry
			cfg.MCP.Codex.Servers[name] = entry
		}
	}

	return rows.Err()
}

func ccSwitchConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	// 优先检查 SQLite 数据库（新版 cc-switch），然后是 JSON 配置文件
	candidates := []string{
		filepath.Join(home, ".cc-switch", "cc-switch.db"),       // 新版 SQLite
		filepath.Join(home, ".cc-switch", "config.json.migrated"), // 旧版迁移后的 JSON
		filepath.Join(home, ".cc-switch", "config.json"),          // 旧版 JSON
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err // 权限/IO 等异常立即暴露
		}
	}
	// 未找到现有文件时，默认使用 SQLite 路径
	return candidates[0], nil
}

func firstRunMarkerPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".code-switch", ".import_prompted"), nil
}

type ccSwitchConfig struct {
	Claude ccProviderSection `json:"claude"`
	Codex  ccProviderSection `json:"codex"`
	MCP    ccMCPSection      `json:"mcp"`
}

type ccProviderSection struct {
	Providers map[string]ccProviderEntry `json:"providers"`
}

type ccProviderEntry struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	WebsiteURL string            `json:"websiteUrl"`
	Settings   ccProviderSetting `json:"settingsConfig"`
}

type ccProviderSetting struct {
	Env    stringMap `json:"env"`  // 使用 stringMap 兼容旧配置中数字类型的值
	Auth   stringMap `json:"auth"` // 使用 stringMap 兼容旧配置中数字类型的值
	Config string    `json:"config"`
}

type ccMCPSection struct {
	Claude ccMCPPlatform `json:"claude"`
	Codex  ccMCPPlatform `json:"codex"`
}

type ccMCPPlatform struct {
	Servers map[string]ccMCPServerEntry `json:"servers"`
}

type ccMCPServerEntry struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Enabled     bool              `json:"enabled"`
	Homepage    string            `json:"homepage"`
	Description string            `json:"description"`
	Server      ccMCPServerConfig `json:"server"`
}

type ccMCPServerConfig struct {
	Type    string    `json:"type"`
	Command string    `json:"command"`
	Args    []string  `json:"args"`
	Env     stringMap `json:"env"` // 使用 stringMap 兼容旧配置中数字类型的值
	URL     string    `json:"url"`
}

type providerCandidate struct {
	Name   string
	APIURL string
	APIKey string
	Site   string
	Icon   string
}

func (is *ImportService) pendingProviders(cfg *ccSwitchConfig) (map[string][]providerCandidate, error) {
	result := map[string][]providerCandidate{
		"claude": {},
		"codex":  {},
	}
	claudeExisting, err := is.providerService.LoadProviders("claude")
	if err != nil {
		return nil, err
	}
	codexExisting, err := is.providerService.LoadProviders("codex")
	if err != nil {
		return nil, err
	}
	result["claude"] = diffProviderCandidates("claude", cfg.Claude.Providers, claudeExisting)
	result["codex"] = diffProviderCandidates("codex", cfg.Codex.Providers, codexExisting)
	return result, nil
}

func diffProviderCandidates(kind string, entries map[string]ccProviderEntry, existing []Provider) []providerCandidate {
	if len(entries) == 0 {
		return []providerCandidate{}
	}
	existingURL := make(map[string]struct{})
	existingNames := make(map[string]struct{})
	for _, provider := range existing {
		if url := normalizeURL(provider.APIURL); url != "" {
			existingURL[url] = struct{}{}
		}
		if name := normalizeName(provider.Name); name != "" {
			existingNames[name] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	candidates := make([]providerCandidate, 0, len(entries))
	for key, entry := range entries {
		candidate, ok := parseProviderEntry(kind, key, entry)
		if !ok {
			continue
		}
		if url := normalizeURL(candidate.APIURL); url != "" {
			if _, exists := existingURL[url]; exists {
				continue
			}
			if _, dup := seen[url]; dup {
				continue
			}
		}
		if name := normalizeName(candidate.Name); name != "" {
			if _, exists := existingNames[name]; exists {
				continue
			}
		}
		dedupKey := normalizeURL(candidate.APIURL)
		if dedupKey == "" {
			dedupKey = normalizeName(candidate.Name)
		}
		if dedupKey != "" {
			seen[dedupKey] = struct{}{}
		}
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i].Name) < strings.ToLower(candidates[j].Name)
	})
	return candidates
}

func parseProviderEntry(kind, key string, entry ccProviderEntry) (providerCandidate, bool) {
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = strings.TrimSpace(entry.ID)
	}
	if name == "" {
		name = strings.TrimSpace(key)
	}
	site := strings.TrimSpace(entry.WebsiteURL)
	switch strings.ToLower(kind) {
	case "claude":
		apiURL := strings.TrimSpace(entry.Settings.Env["ANTHROPIC_BASE_URL"])
		apiKey := strings.TrimSpace(entry.Settings.Env["ANTHROPIC_AUTH_TOKEN"])
		if apiURL == "" || apiKey == "" {
			log.Printf("ℹ️  cc-switch: 跳过 claude provider [%s]: 缺少 ANTHROPIC_BASE_URL 或 ANTHROPIC_AUTH_TOKEN", key)
			return providerCandidate{}, false
		}
		return providerCandidate{Name: name, APIURL: apiURL, APIKey: apiKey, Site: site}, true
	case "codex":
		apiKey := pickFirstNonEmpty(
			entry.Settings.Auth["OPENAI_API_KEY"],
			entry.Settings.Auth["OPENAI_API_KEY_1"],
			entry.Settings.Auth["OPENAI_API_KEY_V2"],
			entry.Settings.Env["OPENAI_API_KEY"],
		)
		if apiKey == "" {
			log.Printf("ℹ️  cc-switch: 跳过 codex provider [%s]: 缺少 OPENAI_API_KEY", key)
			return providerCandidate{}, false
		}
		apiURL := resolveCodexAPIURL(entry.Settings.Config)
		if apiURL == "" {
			log.Printf("ℹ️  cc-switch: 跳过 codex provider [%s]: 无法解析 API URL (TOML 配置无效或缺失)", key)
			return providerCandidate{}, false
		}
		return providerCandidate{Name: name, APIURL: apiURL, APIKey: apiKey, Site: site}, true
	default:
		return providerCandidate{}, false
	}
}

type ccImportCodexConfig struct {
	ModelProvider    string                                 `toml:"model_provider"`
	AltModelProvider string                                 `toml:"nmodel_provider"`
	Providers        map[string]ccImportCodexProviderConfig `toml:"model_providers"`
}

type ccImportCodexProviderConfig struct {
	Name    string `toml:"name"`
	BaseURL string `toml:"base_url"`
}

func resolveCodexAPIURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var cfg ccImportCodexConfig
	if err := toml.Unmarshal([]byte(raw), &cfg); err != nil {
		return ""
	}
	providerKey := cfg.ModelProvider
	if providerKey == "" {
		providerKey = cfg.AltModelProvider
	}
	if providerKey != "" {
		if provider, ok := cfg.Providers[providerKey]; ok {
			return strings.TrimSpace(provider.BaseURL)
		}
		lower := strings.ToLower(providerKey)
		for key, provider := range cfg.Providers {
			if strings.ToLower(key) == lower {
				return strings.TrimSpace(provider.BaseURL)
			}
			if strings.ToLower(strings.TrimSpace(provider.Name)) == lower {
				return strings.TrimSpace(provider.BaseURL)
			}
		}
	}
	for _, provider := range cfg.Providers {
		if url := strings.TrimSpace(provider.BaseURL); url != "" {
			return url
		}
	}
	return ""
}

func pickFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeURL(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimRight(trimmed, "/")
	return strings.ToLower(trimmed)
}

func normalizeName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (is *ImportService) importProviders(cfg *ccSwitchConfig, pending map[string][]providerCandidate) (int, error) {
	total := 0
	if candidates := pending["claude"]; len(candidates) > 0 {
		added, err := is.saveProviders("claude", candidates)
		if err != nil {
			return total, err
		}
		total += added
	}
	if candidates := pending["codex"]; len(candidates) > 0 {
		added, err := is.saveProviders("codex", candidates)
		if err != nil {
			return total, err
		}
		total += added
	}
	return total, nil
}

func (is *ImportService) saveProviders(kind string, candidates []providerCandidate) (int, error) {
	existing, err := is.providerService.LoadProviders(kind)
	if err != nil {
		return 0, err
	}
	nextID := nextProviderID(existing)
	merged := make([]Provider, 0, len(existing)+len(candidates))
	merged = append(merged, existing...)
	accent, tint := defaultVisual(kind)
	for _, candidate := range candidates {
		provider := Provider{
			ID:      nextID,
			Name:    candidate.Name,
			APIURL:  candidate.APIURL,
			APIKey:  candidate.APIKey,
			Site:    candidate.Site,
			Icon:    candidate.Icon,
			Tint:    tint,
			Accent:  accent,
			Enabled: true,
		}
		merged = append(merged, provider)
		nextID++
	}
	if err := is.providerService.SaveProviders(kind, merged); err != nil {
		return 0, err
	}
	return len(candidates), nil
}

func nextProviderID(list []Provider) int64 {
	maxID := int64(0)
	for _, provider := range list {
		if provider.ID > maxID {
			maxID = provider.ID
		}
	}
	return maxID + 1
}

func defaultVisual(kind string) (accent, tint string) {
	switch strings.ToLower(kind) {
	case "codex":
		return "#ec4899", "rgba(236, 72, 153, 0.16)"
	default:
		return "#0a84ff", "rgba(15, 23, 42, 0.12)"
	}
}

func (is *ImportService) pendingMCPCandidates(cfg *ccSwitchConfig) ([]MCPServer, error) {
	existing, err := is.mcpService.ListServers()
	if err != nil {
		return nil, err
	}
	existingNames := make(map[string]struct{}, len(existing))
	for _, server := range existing {
		if name := normalizeName(server.Name); name != "" {
			existingNames[name] = struct{}{}
		}
	}
	candidates := collectMCPServers(cfg)
	result := make([]MCPServer, 0, len(candidates))
	seen := make(map[string]struct{})
	for _, server := range candidates {
		name := normalizeName(server.Name)
		if name == "" {
			continue
		}
		if _, exists := existingNames[name]; exists {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		result = append(result, server)
		seen[name] = struct{}{}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

func (is *ImportService) importMCPServers(candidates []MCPServer) (int, error) {
	if len(candidates) == 0 {
		return 0, nil
	}
	existing, err := is.mcpService.ListServers()
	if err != nil {
		return 0, err
	}
	merged := make([]MCPServer, 0, len(existing)+len(candidates))
	merged = append(merged, existing...)
	merged = append(merged, candidates...)
	if err := is.mcpService.SaveServers(merged); err != nil {
		return 0, err
	}
	return len(candidates), nil
}

func collectMCPServers(cfg *ccSwitchConfig) []MCPServer {
	stores := map[string]*MCPServer{}
	appendMCPEntries(stores, cfg.MCP.Claude.Servers, platClaudeCode)
	appendMCPEntries(stores, cfg.MCP.Codex.Servers, platCodex)
	servers := make([]MCPServer, 0, len(stores))
	for _, server := range stores {
		server.EnabledInClaude = containsPlatform(server.EnablePlatform, platClaudeCode)
		server.EnabledInCodex = containsPlatform(server.EnablePlatform, platCodex)
		servers = append(servers, *server)
	}
	return servers
}

func appendMCPEntries(target map[string]*MCPServer, entries map[string]ccMCPServerEntry, platform string) {
	if len(entries) == 0 {
		return
	}
	for key, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = strings.TrimSpace(entry.ID)
		}
		if name == "" {
			name = strings.TrimSpace(key)
		}
		if name == "" {
			continue
		}
		serverCfg := entry.Server
		serverType := strings.TrimSpace(serverCfg.Type)
		command := strings.TrimSpace(serverCfg.Command)
		url := strings.TrimSpace(serverCfg.URL)
		if serverType == "" {
			if url != "" {
				serverType = "http"
			} else if command != "" {
				serverType = "stdio"
			}
		}
		if serverType == "" {
			continue
		}
		if serverType == "http" && url == "" {
			continue
		}
		if serverType == "stdio" && command == "" {
			continue
		}
		normalizedName := strings.ToLower(name)
		existing := target[normalizedName]
		if existing == nil {
			existing = &MCPServer{
				Name:           name,
				Type:           serverType,
				Command:        command,
				Args:           cloneStringSlice(serverCfg.Args),
				Env:            cloneStringMap(serverCfg.Env),
				URL:            url,
				Website:        strings.TrimSpace(entry.Homepage),
				Tips:           strings.TrimSpace(entry.Description),
				EnablePlatform: []string{},
			}
			target[normalizedName] = existing
		} else {
			if existing.Type == "http" && existing.URL == "" {
				existing.URL = url
			}
			if existing.Type == "stdio" && existing.Command == "" {
				existing.Command = command
			}
			if len(existing.Args) == 0 {
				existing.Args = cloneStringSlice(serverCfg.Args)
			}
			if len(existing.Env) == 0 {
				existing.Env = cloneStringMap(serverCfg.Env)
			}
			if existing.Website == "" {
				existing.Website = strings.TrimSpace(entry.Homepage)
			}
			if existing.Tips == "" {
				existing.Tips = strings.TrimSpace(entry.Description)
			}
		}
		if entry.Enabled {
			if !containsPlatform(existing.EnablePlatform, platform) {
				existing.EnablePlatform = append(existing.EnablePlatform, platform)
			}
		}
	}
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func cloneStringMap(values stringMap) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func containsPlatform(list []string, platform string) bool {
	platform = strings.TrimSpace(platform)
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), platform) {
			return true
		}
	}
	return false
}

// stringSlice 是一个宽容的 []string 类型
// 在 JSON 反序列化时自动将数字、布尔等类型转为字符串，并过滤空白项
// 同时兼容 args 字段被写成单个字符串的情况
type stringSlice []string

func (s *stringSlice) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*s = nil
		return nil
	}
	var raw []interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		// 兼容 args 被写成单个字符串
		var single string
		if err2 := json.Unmarshal(data, &single); err2 == nil {
			single = strings.TrimSpace(single)
			if single == "" {
				*s = []string{}
				return nil
			}
			*s = []string{single}
			return nil
		}
		return err
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		switch t := v.(type) {
		case string:
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				out = append(out, trimmed)
			}
		case nil:
			continue
		default:
			if trimmed := strings.TrimSpace(fmt.Sprint(t)); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	*s = out
	return nil
}

// MCPParseResult MCP JSON 解析结果（供前端批量导入向导使用）
type MCPParseResult struct {
	Servers   []MCPServer `json:"servers"`
	Conflicts []string    `json:"conflicts"`
	NeedName  bool        `json:"needName"`
}

type mcpImportServer struct {
	Name    string      `json:"name,omitempty"`
	Type    string      `json:"type,omitempty"`
	Command string      `json:"command,omitempty"`
	Args    stringSlice `json:"args,omitempty"`
	Env     stringMap   `json:"env,omitempty"`
	URL     string      `json:"url,omitempty"`
	Website string      `json:"website,omitempty"`
	Tips    string      `json:"tips,omitempty"`

	EnablePlatform  []string `json:"enable_platform,omitempty"`
	EnabledInClaude bool     `json:"enabled_in_claude,omitempty"`
	EnabledInCodex  bool     `json:"enabled_in_codex,omitempty"`
}

func (s mcpImportServer) hasDefinitionFields() bool {
	if strings.TrimSpace(s.Type) != "" {
		return true
	}
	if strings.TrimSpace(s.Command) != "" {
		return true
	}
	if len(s.Args) > 0 {
		return true
	}
	if len(s.Env) > 0 {
		return true
	}
	if strings.TrimSpace(s.URL) != "" {
		return true
	}
	return false
}

// ParseMCPJSON 解析用户粘贴的 MCP JSON，返回可供前端预览/选择的结构化结果。
// 支持格式：
// 1) Claude Desktop: {"mcpServers": {"name": {...}}}
// 2) 数组: [{"name": "...", ...}, ...]
// 3) 单服务器: {"command": "...", "args": [...] }（name 可选，缺失时 needName=true）
// 4) 服务器映射: {"name": {...}, "name2": {...}}
func (is *ImportService) ParseMCPJSON(jsonStr string) (*MCPParseResult, error) {
	jsonStr = strings.TrimSpace(jsonStr)
	if jsonStr == "" {
		return nil, nil
	}

	existing, err := is.mcpService.ListServers()
	if err != nil {
		return nil, err
	}
	existingNames := make(map[string]struct{}, len(existing))
	for _, server := range existing {
		if name := normalizeName(server.Name); name != "" {
			existingNames[name] = struct{}{}
		}
	}

	data := []byte(jsonStr)

	// 先按对象解析，便于检测是否存在 mcpServers 字段
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err == nil {
		if raw, ok := object["mcpServers"]; ok {
			var serversMap map[string]json.RawMessage
			if err := json.Unmarshal(raw, &serversMap); err != nil {
				return nil, fmt.Errorf("mcpServers 字段必须是对象: %w", err)
			}
			servers, err := is.parseMCPServerMap(serversMap, []string{platClaudeCode})
			if err != nil {
				return nil, err
			}
			return &MCPParseResult{
				Servers:   servers,
				Conflicts: collectMCPConflicts(existingNames, servers),
				NeedName:  false,
			}, nil
		}
	}

	// 数组格式
	var array []json.RawMessage
	if err := json.Unmarshal(data, &array); err == nil {
		servers, err := is.parseMCPServerArray(array)
		if err != nil {
			return nil, err
		}
		return &MCPParseResult{
			Servers:   servers,
			Conflicts: collectMCPConflicts(existingNames, servers),
			NeedName:  false,
		}, nil
	}

	// 顶层必须是对象或数组
	if object == nil {
		if err := json.Unmarshal(data, &object); err != nil {
			return nil, fmt.Errorf("MCP JSON 顶层必须是对象或数组: %w", err)
		}
	}

	// 单服务器格式（用于快速粘贴单条配置）
	var single mcpImportServer
	if err := json.Unmarshal(data, &single); err == nil && single.hasDefinitionFields() {
		server, err := buildMCPServerFromImport("", single, nil)
		if err != nil {
			return nil, err
		}
		needName := strings.TrimSpace(server.Name) == ""
		conflicts := []string{}
		if !needName {
			if _, ok := existingNames[normalizeName(server.Name)]; ok {
				conflicts = []string{server.Name}
			}
		}
		return &MCPParseResult{
			Servers:   []MCPServer{server},
			Conflicts: conflicts,
			NeedName:  needName,
		}, nil
	}

	// 服务器映射格式：{"name": {...}}
	servers, err := is.parseMCPServerMap(object, nil)
	if err != nil {
		return nil, err
	}
	return &MCPParseResult{
		Servers:   servers,
		Conflicts: collectMCPConflicts(existingNames, servers),
		NeedName:  false,
	}, nil
}

// ImportMCPServers 将服务器写入配置，并同步到 Claude/Codex。
// strategy:
// - "skip": 已存在则跳过
// - "overwrite": 用导入内容更新（保留既有 enable_platform 的并集）
func (is *ImportService) ImportMCPServers(servers []MCPServer, strategy string) (int, error) {
	strategy = strings.ToLower(strings.TrimSpace(strategy))
	if strategy == "" {
		strategy = "skip"
	}
	if strategy != "skip" && strategy != "overwrite" {
		return 0, fmt.Errorf("未知冲突策略: %s", strategy)
	}
	if len(servers) == 0 {
		return 0, nil
	}

	existing, err := is.mcpService.ListServers()
	if err != nil {
		return 0, err
	}

	merged := make([]MCPServer, len(existing))
	copy(merged, existing)

	indexByName := make(map[string]int, len(existing))
	for i, server := range merged {
		if key := normalizeName(server.Name); key != "" {
			indexByName[key] = i
		}
	}

	imported := 0
	seen := make(map[string]struct{}, len(servers))
	for _, raw := range servers {
		candidate, err := normalizeIncomingMCPServer(raw)
		if err != nil {
			return 0, err
		}
		key := normalizeName(candidate.Name)
		if key == "" {
			return 0, errors.New("MCP server name 不能为空")
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		if idx, exists := indexByName[key]; exists {
			if strategy == "skip" {
				continue
			}
			merged[idx] = mergeMCPServerOverwrite(merged[idx], candidate)
			imported++
			continue
		}
		merged = append(merged, candidate)
		indexByName[key] = len(merged) - 1
		imported++
	}

	if imported == 0 {
		return 0, nil
	}
	if err := is.mcpService.SaveServers(merged); err != nil {
		return 0, err
	}
	return imported, nil
}

func (is *ImportService) parseMCPServerMap(entries map[string]json.RawMessage, defaultPlatforms []string) ([]MCPServer, error) {
	if len(entries) == 0 {
		return []MCPServer{}, nil
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	seen := make(map[string]struct{}, len(entries))
	servers := make([]MCPServer, 0, len(entries))
	for _, name := range names {
		var entry mcpImportServer
		if err := json.Unmarshal(entries[name], &entry); err != nil {
			return nil, fmt.Errorf("解析 MCP server [%s] 失败: %w", strings.TrimSpace(name), err)
		}
		server, err := buildMCPServerFromImport(name, entry, defaultPlatforms)
		if err != nil {
			return nil, err
		}
		key := normalizeName(server.Name)
		if key == "" {
			return nil, errors.New("MCP server name 不能为空")
		}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("输入中存在重复的 MCP server 名称: %s", server.Name)
		}
		seen[key] = struct{}{}
		servers = append(servers, server)
	}
	sort.SliceStable(servers, func(i, j int) bool {
		return strings.ToLower(servers[i].Name) < strings.ToLower(servers[j].Name)
	})
	return servers, nil
}

func (is *ImportService) parseMCPServerArray(entries []json.RawMessage) ([]MCPServer, error) {
	if len(entries) == 0 {
		return []MCPServer{}, nil
	}
	seen := make(map[string]struct{}, len(entries))
	servers := make([]MCPServer, 0, len(entries))
	for i, raw := range entries {
		var entry mcpImportServer
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("解析 MCP server 数组第 %d 项失败: %w", i+1, err)
		}
		if strings.TrimSpace(entry.Name) == "" {
			return nil, fmt.Errorf("MCP server 数组第 %d 项缺少 name", i+1)
		}
		server, err := buildMCPServerFromImport("", entry, nil)
		if err != nil {
			return nil, err
		}
		key := normalizeName(server.Name)
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("输入中存在重复的 MCP server 名称: %s", server.Name)
		}
		seen[key] = struct{}{}
		servers = append(servers, server)
	}
	sort.SliceStable(servers, func(i, j int) bool {
		return strings.ToLower(servers[i].Name) < strings.ToLower(servers[j].Name)
	})
	return servers, nil
}

func buildMCPServerFromImport(name string, entry mcpImportServer, defaultPlatforms []string) (MCPServer, error) {
	if strings.TrimSpace(name) == "" {
		name = entry.Name
	}
	name = strings.TrimSpace(name)

	typeHint := strings.TrimSpace(entry.Type)
	command := strings.TrimSpace(entry.Command)
	url := strings.TrimSpace(entry.URL)
	if typeHint == "" {
		if url != "" {
			typeHint = "http"
		} else {
			typeHint = "stdio"
		}
	}
	typ := normalizeServerType(typeHint)

	label := name
	if label == "" {
		label = "MCP server"
	}
	if typ == "http" && url == "" {
		return MCPServer{}, fmt.Errorf("%s 需要提供 url", label)
	}
	if typ == "stdio" && command == "" {
		return MCPServer{}, fmt.Errorf("%s 需要提供 command", label)
	}

	platforms := make([]string, 0, len(defaultPlatforms)+len(entry.EnablePlatform)+2)
	platforms = append(platforms, defaultPlatforms...)
	platforms = append(platforms, entry.EnablePlatform...)
	if entry.EnabledInClaude {
		platforms = append(platforms, platClaudeCode)
	}
	if entry.EnabledInCodex {
		platforms = append(platforms, platCodex)
	}
	platforms = normalizePlatforms(platforms)

	server := MCPServer{
		Name:           name,
		Type:           typ,
		Command:        command,
		Args:           cleanArgs([]string(entry.Args)),
		Env:            cleanEnv(cloneStringMap(entry.Env)),
		URL:            url,
		Website:        strings.TrimSpace(entry.Website),
		Tips:           strings.TrimSpace(entry.Tips),
		EnablePlatform: platforms,
	}
	server.MissingPlaceholders = detectPlaceholders(server.URL, server.Args)
	if len(server.MissingPlaceholders) > 0 {
		server.EnablePlatform = []string{}
	}
	return server, nil
}

func collectMCPConflicts(existing map[string]struct{}, servers []MCPServer) []string {
	if len(existing) == 0 || len(servers) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{})
	conflicts := make([]string, 0)
	for _, server := range servers {
		key := normalizeName(server.Name)
		if key == "" {
			continue
		}
		if _, ok := existing[key]; !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		conflicts = append(conflicts, server.Name)
	}
	sort.SliceStable(conflicts, func(i, j int) bool {
		return strings.ToLower(conflicts[i]) < strings.ToLower(conflicts[j])
	})
	return conflicts
}

func normalizeIncomingMCPServer(server MCPServer) (MCPServer, error) {
	name := strings.TrimSpace(server.Name)
	if name == "" {
		return MCPServer{}, errors.New("MCP server name 不能为空")
	}

	typeHint := strings.TrimSpace(server.Type)
	command := strings.TrimSpace(server.Command)
	url := strings.TrimSpace(server.URL)
	if typeHint == "" {
		if url != "" {
			typeHint = "http"
		} else {
			typeHint = "stdio"
		}
	}
	typ := normalizeServerType(typeHint)
	if typ == "http" && url == "" {
		return MCPServer{}, fmt.Errorf("%s 需要提供 url", name)
	}
	if typ == "stdio" && command == "" {
		return MCPServer{}, fmt.Errorf("%s 需要提供 command", name)
	}

	platforms := make([]string, 0, len(server.EnablePlatform)+2)
	platforms = append(platforms, server.EnablePlatform...)
	if server.EnabledInClaude {
		platforms = append(platforms, platClaudeCode)
	}
	if server.EnabledInCodex {
		platforms = append(platforms, platCodex)
	}
	platforms = normalizePlatforms(platforms)

	out := MCPServer{
		Name:           name,
		Type:           typ,
		Command:        command,
		Args:           cleanArgs(server.Args),
		Env:            cleanEnv(server.Env),
		URL:            url,
		Website:        strings.TrimSpace(server.Website),
		Tips:           strings.TrimSpace(server.Tips),
		EnablePlatform: platforms,
	}
	out.MissingPlaceholders = detectPlaceholders(out.URL, out.Args)
	if len(out.MissingPlaceholders) > 0 {
		out.EnablePlatform = []string{}
	}
	return out, nil
}

func mergeMCPServerOverwrite(existing MCPServer, incoming MCPServer) MCPServer {
	result := existing

	if strings.TrimSpace(result.Name) == "" {
		result.Name = incoming.Name
	}

	// 类型变更时清理不兼容字段
	oldType := normalizeServerType(result.Type)
	newType := normalizeServerType(incoming.Type)
	if strings.TrimSpace(incoming.Type) != "" {
		result.Type = newType
	}
	typeChanged := oldType != "" && newType != "" && oldType != newType

	if newType == "http" {
		// 切换到 http 时清除 stdio 字段
		if typeChanged || strings.TrimSpace(incoming.URL) != "" {
			result.URL = strings.TrimSpace(incoming.URL)
		}
		if typeChanged {
			result.Command = ""
			result.Args = nil
		}
	} else {
		// 切换到 stdio 时清除 http 字段
		if typeChanged || strings.TrimSpace(incoming.Command) != "" {
			result.Command = strings.TrimSpace(incoming.Command)
		}
		if len(incoming.Args) > 0 || typeChanged {
			result.Args = incoming.Args
		}
		if typeChanged {
			result.URL = ""
		}
	}

	if len(incoming.Env) > 0 {
		result.Env = incoming.Env
	}
	if strings.TrimSpace(incoming.Website) != "" {
		result.Website = incoming.Website
	}
	if strings.TrimSpace(incoming.Tips) != "" {
		result.Tips = incoming.Tips
	}

	result.EnablePlatform = unionPlatforms(existing.EnablePlatform, incoming.EnablePlatform)

	result.MissingPlaceholders = detectPlaceholders(result.URL, result.Args)
	if len(result.MissingPlaceholders) > 0 {
		result.EnablePlatform = []string{}
	}
	return result
}
