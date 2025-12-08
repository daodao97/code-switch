package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
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

func ccSwitchConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	// 优先检查新版配置文件，回退到旧版
	candidates := []string{
		filepath.Join(home, ".cc-switch", "config.json.migrated"),
		filepath.Join(home, ".cc-switch", "config.json"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err // 权限/IO 等异常立即暴露
		}
	}
	// 未找到现有文件时，默认使用新路径
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

// ========== MCP JSON 导入功能 ==========

// MCPParseResult JSON 解析结果
type MCPParseResult struct {
	Servers   []MCPServer `json:"servers"`   // 解析出的服务器列表
	Conflicts []string    `json:"conflicts"` // 与现有配置冲突的名称
	NeedName  bool        `json:"needName"`  // 是否需要用户提供名称（单服务器无名时）
}

// ParseMCPJSON 解析 JSON 字符串为 MCP 服务器列表
// 支持三种格式：
// 1. {"mcpServers": {"name": {...}}} - Claude Desktop 格式
// 2. [{"name": "...", "command": "..."}] - 数组格式
// 3. {"command": "...", "args": [...]} - 单服务器格式
func (is *ImportService) ParseMCPJSON(jsonStr string) (*MCPParseResult, error) {
	jsonStr = strings.TrimSpace(jsonStr)
	if jsonStr == "" {
		return nil, errors.New("JSON 内容为空")
	}

	// 先解析为 interface{} 判断类型
	var raw interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, errors.New("JSON 格式无效: " + err.Error())
	}

	var servers []MCPServer
	var needName bool

	switch v := raw.(type) {
	case map[string]interface{}:
		// 检查是否是 Claude Desktop 格式
		if mcpServers, ok := v["mcpServers"]; ok {
			if serversMap, ok := mcpServers.(map[string]interface{}); ok {
				parsed, err := parseMCPServersMap(serversMap)
				if err != nil {
					return nil, err
				}
				servers = parsed
			} else {
				return nil, errors.New("mcpServers 字段格式无效，应为对象")
			}
		} else {
			// 尝试解析为单服务器
			server, need, err := parseSingleServer(v, "")
			if err != nil {
				return nil, err
			}
			servers = []MCPServer{server}
			needName = need
		}

	case []interface{}:
		// 数组格式
		parsed, err := parseMCPServersArray(v)
		if err != nil {
			return nil, err
		}
		servers = parsed

	default:
		return nil, errors.New("JSON 格式无效，应为对象或数组")
	}

	if len(servers) == 0 {
		return nil, errors.New("未找到有效的 MCP 服务器配置")
	}

	// 检查与现有配置的冲突
	conflicts := is.checkMCPConflicts(servers)

	return &MCPParseResult{
		Servers:   servers,
		Conflicts: conflicts,
		NeedName:  needName,
	}, nil
}

// parseMCPServersMap 解析 {"name": {...}, ...} 格式
func parseMCPServersMap(serversMap map[string]interface{}) ([]MCPServer, error) {
	servers := make([]MCPServer, 0, len(serversMap))
	for name, serverData := range serversMap {
		serverMap, ok := serverData.(map[string]interface{})
		if !ok {
			continue
		}
		server, _, err := parseSingleServer(serverMap, name)
		if err != nil {
			return nil, errors.New("服务器 '" + name + "' 配置无效: " + err.Error())
		}
		servers = append(servers, server)
	}
	return servers, nil
}

// parseMCPServersArray 解析数组格式
func parseMCPServersArray(arr []interface{}) ([]MCPServer, error) {
	servers := make([]MCPServer, 0, len(arr))
	for i, item := range arr {
		idx := i + 1 // 使用 1 基索引，对用户更友好
		serverMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("数组第 %d 项不是有效的对象", idx)
		}
		// 从数组元素中提取 name
		name := ""
		if n, ok := serverMap["name"].(string); ok {
			name = strings.TrimSpace(n)
		}
		server, _, err := parseSingleServer(serverMap, name)
		if err != nil {
			if name != "" {
				return nil, fmt.Errorf("服务器 '%s' 配置无效: %v", name, err)
			}
			return nil, fmt.Errorf("数组第 %d 项配置无效: %v", idx, err)
		}
		// 如果数组元素没有 name，生成一个
		if server.Name == "" {
			server.Name = fmt.Sprintf("imported-%d", idx)
		}
		servers = append(servers, server)
	}
	return servers, nil
}

// parseSingleServer 解析单个服务器配置
func parseSingleServer(data map[string]interface{}, name string) (MCPServer, bool, error) {
	server := MCPServer{
		Name:           strings.TrimSpace(name),
		EnablePlatform: []string{platClaudeCode, platCodex}, // 默认启用两个平台
	}
	needName := false

	// 解析 type
	if t, ok := data["type"].(string); ok {
		server.Type = strings.TrimSpace(t)
	}

	// 解析 command
	if cmd, ok := data["command"].(string); ok {
		server.Command = strings.TrimSpace(cmd)
	}

	// 解析 args
	if args, ok := data["args"].([]interface{}); ok {
		for _, arg := range args {
			if s, ok := arg.(string); ok {
				server.Args = append(server.Args, s)
			}
		}
	}

	// 解析 env
	if env, ok := data["env"].(map[string]interface{}); ok {
		server.Env = make(map[string]string)
		for k, v := range env {
			if s, ok := v.(string); ok {
				server.Env[k] = s
			}
		}
	}

	// 解析 url
	if url, ok := data["url"].(string); ok {
		server.URL = strings.TrimSpace(url)
	}

	// 解析可选字段
	if website, ok := data["website"].(string); ok {
		server.Website = strings.TrimSpace(website)
	}
	if tips, ok := data["tips"].(string); ok {
		server.Tips = strings.TrimSpace(tips)
	}

	// 自动推断类型
	if server.Type == "" {
		if server.URL != "" {
			server.Type = "http"
		} else if server.Command != "" {
			server.Type = "stdio"
		}
	}

	// 验证必需字段
	if server.Type == "" {
		return server, false, errors.New("缺少 type 字段，且无法从 command/url 推断")
	}
	if server.Type == "stdio" && server.Command == "" {
		return server, false, errors.New("stdio 类型需要 command 字段")
	}
	if server.Type == "http" && server.URL == "" {
		return server, false, errors.New("http 类型需要 url 字段")
	}

	// 检查是否需要名称
	if server.Name == "" {
		needName = true
	}

	return server, needName, nil
}

// checkMCPConflicts 检查与现有配置的冲突
func (is *ImportService) checkMCPConflicts(servers []MCPServer) []string {
	existing, err := is.mcpService.ListServers()
	if err != nil {
		return nil
	}
	existingNames := make(map[string]struct{}, len(existing))
	for _, s := range existing {
		existingNames[strings.ToLower(s.Name)] = struct{}{}
	}

	var conflicts []string
	for _, s := range servers {
		if _, exists := existingNames[strings.ToLower(s.Name)]; exists {
			conflicts = append(conflicts, s.Name)
		}
	}
	return conflicts
}

// ImportMCPFromJSON 导入解析后的 MCP 服务器（用于多服务器批量导入）
func (is *ImportService) ImportMCPFromJSON(servers []MCPServer, conflictStrategy string) (int, error) {
	if len(servers) == 0 {
		return 0, nil
	}

	existing, err := is.mcpService.ListServers()
	if err != nil {
		return 0, err
	}

	// 使用 map 追踪所有已知名称（现有 + 新增）
	existingMap := make(map[string]int, len(existing))
	for i, s := range existing {
		existingMap[strings.ToLower(s.Name)] = i
	}

	// 追踪本次导入中已使用的名称，防止导入列表内部重复
	usedNames := make(map[string]struct{})
	for name := range existingMap {
		usedNames[name] = struct{}{}
	}

	merged := make([]MCPServer, len(existing))
	copy(merged, existing)
	imported := 0

	for _, server := range servers {
		lowerName := strings.ToLower(server.Name)

		// 检查是否与导入列表内已处理的项重复
		if _, used := usedNames[lowerName]; used && conflictStrategy != "overwrite" {
			// 非覆盖模式下，跳过导入列表内的重复项
			if conflictStrategy == "skip" {
				continue
			}
			// rename 模式也需要重命名
		}

		if idx, exists := existingMap[lowerName]; exists {
			switch conflictStrategy {
			case "overwrite":
				// 覆盖
				merged[idx] = server
				imported++
			case "skip":
				// 跳过
				continue
			case "rename":
				// 重命名：生成唯一名称
				newName := generateUniqueName(server.Name, usedNames)
				server.Name = newName
				usedNames[strings.ToLower(newName)] = struct{}{}
				merged = append(merged, server)
				imported++
			default:
				// 默认跳过
				continue
			}
		} else {
			// 检查是否与本次导入中的其他项重名
			if _, used := usedNames[lowerName]; used {
				if conflictStrategy == "rename" {
					newName := generateUniqueName(server.Name, usedNames)
					server.Name = newName
					usedNames[strings.ToLower(newName)] = struct{}{}
				} else {
					// skip 或其他策略：跳过重复
					continue
				}
			} else {
				usedNames[lowerName] = struct{}{}
			}
			merged = append(merged, server)
			imported++
		}
	}

	if err := is.mcpService.SaveServers(merged); err != nil {
		return 0, err
	}
	return imported, nil
}

// generateUniqueName 生成唯一名称，避免与已有名称冲突
func generateUniqueName(baseName string, usedNames map[string]struct{}) string {
	candidate := baseName + "-imported"
	if _, exists := usedNames[strings.ToLower(candidate)]; !exists {
		return candidate
	}
	for i := 2; i <= 100; i++ {
		candidate = fmt.Sprintf("%s-imported-%d", baseName, i)
		if _, exists := usedNames[strings.ToLower(candidate)]; !exists {
			return candidate
		}
	}
	// 极端情况：返回带时间戳的名称
	return fmt.Sprintf("%s-imported-%d", baseName, time.Now().UnixNano())
}
