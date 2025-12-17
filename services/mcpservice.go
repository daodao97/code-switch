package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

const (
	mcpStoreDir      = ".code-switch"
	mcpStoreFile     = "mcp.json"
	claudeMcpFile    = ".claude.json"
	codexDirName     = ".codex"
	codexConfigFile  = "config.toml"
	geminiDirName    = ".gemini"
	geminiConfigFile = "settings.json"
	platClaudeCode   = "claude-code"
	platCodex        = "codex"
	platGemini       = "gemini"
)

var builtInServers = map[string]rawMCPServer{
	"reftools": {
		Type:    "http",
		URL:     "https://api.ref.tools/mcp?apiKey={apiKey}",
		Website: "https://ref.tools",
		Tips:    "Visit ref.tools to claim your API key.",
	},
	"chrome-devtools": {
		Type:    "stdio",
		Command: "npx",
		Args:    []string{"-y", "chrome-devtools-mcp@latest"},
		Tips:    "Needs Node.js. Run once to install dependencies.",
	},
}

var placeholderPattern = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

type MCPService struct {
	mu sync.Mutex
}

func NewMCPService() *MCPService {
	return &MCPService{}
}

type MCPServer struct {
	Name                string            `json:"name"`
	Type                string            `json:"type"`
	Command             string            `json:"command,omitempty"`
	Args                []string          `json:"args,omitempty"`
	Env                 map[string]string `json:"env,omitempty"`
	URL                 string            `json:"url,omitempty"`
	Website             string            `json:"website,omitempty"`
	Tips                string            `json:"tips,omitempty"`
	EnablePlatform      []string          `json:"enable_platform"`
	EnabledInClaude     bool              `json:"enabled_in_claude"`
	EnabledInCodex      bool              `json:"enabled_in_codex"`
	EnabledInGemini     bool              `json:"enabled_in_gemini"`
	MissingPlaceholders []string          `json:"missing_placeholders"`
}

type rawMCPServer struct {
	Type           string            `json:"type"`
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	URL            string            `json:"url,omitempty"`
	Website        string            `json:"website,omitempty"`
	Tips           string            `json:"tips,omitempty"`
	EnablePlatform []string          `json:"enable_platform"`
}

type mcpStorePayload struct {
	Servers         map[string]rawMCPServer `json:"servers"`
	DeletedBuiltins []string                `json:"deletedBuiltins,omitempty"`
}

type claudeMcpFilePayload struct {
	Servers map[string]json.RawMessage `json:"mcpServers"`
}

type geminiMcpFilePayload struct {
	Servers map[string]json.RawMessage `json:"mcpServers"`
}

type codexMcpFilePayload struct {
	Servers map[string]map[string]any `toml:"mcp_servers"`
}

type claudeDesktopServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

func (ms *MCPService) ListServers() ([]MCPServer, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	config, err := ms.loadConfig()
	if err != nil {
		return nil, err
	}

	claudeEnabled := loadClaudeEnabledServers()
	codexEnabled := loadCodexEnabledServers()
	geminiEnabled := loadGeminiEnabledServers()

	names := make([]string, 0, len(config))
	for name := range config {
		names = append(names, name)
	}
	sort.Strings(names)

	servers := make([]MCPServer, 0, len(names))
	for _, name := range names {
		entry := config[name]
		typ := normalizeServerType(entry.Type)
		platforms := normalizePlatforms(entry.EnablePlatform)
		server := MCPServer{
			Name:            name,
			Type:            typ,
			Command:         strings.TrimSpace(entry.Command),
			Args:            cloneArgs(entry.Args),
			Env:             cloneEnv(entry.Env),
			URL:             strings.TrimSpace(entry.URL),
			Website:         strings.TrimSpace(entry.Website),
			Tips:            strings.TrimSpace(entry.Tips),
			EnablePlatform:  platforms,
			EnabledInClaude: containsNormalized(claudeEnabled, name),
			EnabledInCodex:  containsNormalized(codexEnabled, name),
			EnabledInGemini: containsNormalized(geminiEnabled, name),
		}
		server.MissingPlaceholders = detectPlaceholders(server.URL, server.Args)
		servers = append(servers, server)
	}

	return servers, nil
}

func (ms *MCPService) SaveServers(servers []MCPServer) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	normalized := make([]MCPServer, len(servers))
	raw := make(map[string]rawMCPServer, len(servers))
	for i := range servers {
		server := servers[i]
		name := strings.TrimSpace(server.Name)
		if name == "" {
			return fmt.Errorf("server name 不能为空")
		}
		typ := normalizeServerType(server.Type)
		platforms := normalizePlatforms(server.EnablePlatform)
		args := cleanArgs(server.Args)
		env := cleanEnv(server.Env)
		command := strings.TrimSpace(server.Command)
		url := strings.TrimSpace(server.URL)
		if typ == "stdio" && command == "" {
			return fmt.Errorf("%s 需要提供 command", name)
		}
		if typ == "http" && url == "" {
			return fmt.Errorf("%s 需要提供 url", name)
		}
		normalized[i] = MCPServer{
			Name:            name,
			Type:            typ,
			Command:         command,
			Args:            args,
			Env:             env,
			URL:             url,
			Website:         strings.TrimSpace(server.Website),
			Tips:            strings.TrimSpace(server.Tips),
			EnablePlatform:  platforms,
			EnabledInClaude: server.EnabledInClaude,
			EnabledInCodex:  server.EnabledInCodex,
			EnabledInGemini: server.EnabledInGemini,
		}
		raw[name] = rawMCPServer{
			Type:           typ,
			Command:        command,
			Args:           args,
			Env:            env,
			URL:            url,
			Website:        normalized[i].Website,
			Tips:           normalized[i].Tips,
			EnablePlatform: platforms,
		}
		placeholders := detectPlaceholders(url, args)
		normalized[i].MissingPlaceholders = placeholders
		if len(placeholders) > 0 {
			normalized[i].EnablePlatform = []string{}
			rawEntry := raw[name]
			rawEntry.EnablePlatform = []string{}
			raw[name] = rawEntry
		}
	}

	// Calculate deletedBuiltins: built-in servers that are missing from raw
	var deletedBuiltins []string
	for builtInName := range builtInServers {
		if _, exists := raw[builtInName]; !exists {
			deletedBuiltins = append(deletedBuiltins, builtInName)
		}
	}
	sort.Strings(deletedBuiltins)

	if err := ms.saveStore(raw, deletedBuiltins); err != nil {
		return err
	}
	if err := ms.syncClaudeServers(normalized); err != nil {
		return err
	}
	if err := ms.syncCodexServers(normalized); err != nil {
		return err
	}
	if err := ms.syncGeminiServers(normalized); err != nil {
		return err
	}
	return nil
}

func (ms *MCPService) configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, mcpStoreDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, mcpStoreFile), nil
}

func (ms *MCPService) loadConfig() (map[string]rawMCPServer, error) {
	path, err := ms.configPath()
	if err != nil {
		return nil, err
	}

	servers := map[string]rawMCPServer{}
	var deletedBuiltins []string

	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		// Try parsing new format first (with servers and deletedBuiltins)
		var storePayload mcpStorePayload
		if err := json.Unmarshal(data, &storePayload); err == nil && storePayload.Servers != nil {
			servers = storePayload.Servers
			deletedBuiltins = storePayload.DeletedBuiltins
		} else {
			// Fall back to legacy flat format for backward compatibility
			if err := json.Unmarshal(data, &servers); err != nil {
				return nil, err
			}
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	for name, entry := range servers {
		servers[name] = normalizeRawEntry(entry)
	}

	changed := false
	if imported, err := ms.importFromClaude(servers); err == nil {
		if ms.mergeImportedServers(servers, imported) {
			changed = true
		}
	} else {
		return nil, err
	}

	if ensureBuiltInServers(servers, deletedBuiltins) {
		changed = true
	}

	if changed {
		if err := ms.saveStore(servers, deletedBuiltins); err != nil {
			return servers, err
		}
	}

	return servers, nil
}

func (ms *MCPService) importFromClaude(existing map[string]rawMCPServer) (map[string]rawMCPServer, error) {
	path, err := claudeConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]rawMCPServer{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return map[string]rawMCPServer{}, nil
	}
	var payload struct {
		Servers map[string]claudeDesktopServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	result := make(map[string]rawMCPServer, len(payload.Servers))
	for name, entry := range payload.Servers {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			continue
		}
		if _, exists := existing[trimmedName]; exists {
			continue
		}
		typeHint := entry.Type
		if strings.TrimSpace(typeHint) == "" {
			if strings.TrimSpace(entry.URL) != "" {
				typeHint = "http"
			}
		}
		if strings.TrimSpace(typeHint) == "" {
			typeHint = "stdio"
		}
		typ := normalizeServerType(typeHint)
		if typ == "http" && entry.URL == "" {
			continue
		}
		if typ == "stdio" && entry.Command == "" {
			continue
		}
		result[trimmedName] = rawMCPServer{
			Type:           typ,
			Command:        strings.TrimSpace(entry.Command),
			Args:           cleanArgs(entry.Args),
			Env:            cleanEnv(entry.Env),
			URL:            strings.TrimSpace(entry.URL),
			EnablePlatform: []string{platClaudeCode},
		}
	}
	return result, nil
}

func (ms *MCPService) saveStore(servers map[string]rawMCPServer, deletedBuiltins []string) error {
	path, err := ms.configPath()
	if err != nil {
		return err
	}
	storePayload := mcpStorePayload{
		Servers:         servers,
		DeletedBuiltins: deletedBuiltins,
	}
	data, err := json.MarshalIndent(storePayload, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizeServerType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "http":
		return "http"
	default:
		return "stdio"
	}
}

func normalizePlatforms(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if platform, ok := normalizePlatform(raw); ok {
			if _, exists := seen[platform]; exists {
				continue
			}
			seen[platform] = struct{}{}
			result = append(result, platform)
		}
	}
	return result
}

func normalizePlatform(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "claude", "claude_code", "claude-code":
		return "claude-code", true
	case "codex":
		return "codex", true
	case "gemini", "gemini-cli", "gemini_cli":
		return "gemini", true
	default:
		return "", false
	}
}

func unionPlatforms(primary, secondary []string) []string {
	combined := append([]string{}, primary...)
	combined = append(combined, secondary...)
	return normalizePlatforms(combined)
}

func normalizeRawEntry(entry rawMCPServer) rawMCPServer {
	entry.Type = normalizeServerType(entry.Type)
	entry.Command = strings.TrimSpace(entry.Command)
	entry.URL = strings.TrimSpace(entry.URL)
	entry.Website = strings.TrimSpace(entry.Website)
	entry.Tips = strings.TrimSpace(entry.Tips)
	entry.Args = cleanArgs(entry.Args)
	entry.Env = cleanEnv(entry.Env)
	entry.EnablePlatform = normalizePlatforms(entry.EnablePlatform)
	return entry
}

func cloneArgs(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	dup := make([]string, len(values))
	copy(dup, values)
	return dup
}

func cloneEnv(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	dup := make(map[string]string, len(values))
	for k, v := range values {
		dup[k] = v
	}
	return dup
}

func cleanArgs(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func cleanEnv(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		result[trimmedKey] = strings.TrimSpace(value)
	}
	return result
}

func containsNormalized(pool map[string]struct{}, value string) bool {
	if len(pool) == 0 {
		return false
	}
	_, ok := pool[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

func loadClaudeEnabledServers() map[string]struct{} {
	result := map[string]struct{}{}
	home, err := os.UserHomeDir()
	if err != nil {
		return result
	}
	path := filepath.Join(home, claudeMcpFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	var payload claudeMcpFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return result
	}
	for name := range payload.Servers {
		result[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	return result
}

func loadCodexEnabledServers() map[string]struct{} {
	result := map[string]struct{}{}
	home, err := os.UserHomeDir()
	if err != nil {
		return result
	}
	path := filepath.Join(home, codexDirName, codexConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	var payload codexMcpFilePayload
	if err := toml.Unmarshal(data, &payload); err != nil {
		return result
	}
	for name := range payload.Servers {
		result[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	return result
}

func loadGeminiEnabledServers() map[string]struct{} {
	result := map[string]struct{}{}
	home, err := os.UserHomeDir()
	if err != nil {
		return result
	}
	path := filepath.Join(home, geminiDirName, geminiConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	var payload geminiMcpFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return result
	}
	for name := range payload.Servers {
		result[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	return result
}

func (ms *MCPService) mergeImportedServers(target, imported map[string]rawMCPServer) bool {
	changed := false
	for name, entry := range imported {
		entry = normalizeRawEntry(entry)
		if existing, ok := target[name]; ok {
			entry.EnablePlatform = unionPlatforms(existing.EnablePlatform, entry.EnablePlatform)
			if entry.Website == "" {
				entry.Website = existing.Website
			}
			if entry.Tips == "" {
				entry.Tips = existing.Tips
			}
		}
		if existing, ok := target[name]; !ok || !reflect.DeepEqual(existing, entry) {
			target[name] = entry
			changed = true
		}
	}
	return changed
}

func ensureBuiltInServers(target map[string]rawMCPServer, deletedBuiltins []string) bool {
	deletedSet := make(map[string]struct{}, len(deletedBuiltins))
	for _, name := range deletedBuiltins {
		deletedSet[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}

	changed := false
	for name, builtIn := range builtInServers {
		// Skip deleted built-in servers (tombstone check)
		if _, deleted := deletedSet[strings.ToLower(name)]; deleted {
			continue
		}

		builtIn = normalizeRawEntry(builtIn)
		if existing, ok := target[name]; ok {
			merged := existing
			merged.EnablePlatform = unionPlatforms(existing.EnablePlatform, builtIn.EnablePlatform)
			if merged.Command == "" {
				merged.Command = builtIn.Command
			}
			if len(merged.Args) == 0 {
				merged.Args = builtIn.Args
			}
			if len(merged.Env) == 0 {
				merged.Env = builtIn.Env
			}
			if merged.URL == "" {
				merged.URL = builtIn.URL
			}
			if merged.Website == "" {
				merged.Website = builtIn.Website
			}
			if merged.Tips == "" {
				merged.Tips = builtIn.Tips
			}
			merged = normalizeRawEntry(merged)
			if !reflect.DeepEqual(existing, merged) {
				target[name] = merged
				changed = true
			}
			continue
		}
		target[name] = builtIn
		changed = true
	}
	return changed
}

func (ms *MCPService) syncClaudeServers(servers []MCPServer) error {
	path, err := claudeConfigPath()
	if err != nil {
		return err
	}
	desired := make(map[string]claudeDesktopServer)
	for _, server := range servers {
		if !platformContains(server.EnablePlatform, platClaudeCode) {
			continue
		}
		desired[server.Name] = buildClaudeDesktopEntry(server)
	}
	payload := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &payload); err != nil {
			payload = make(map[string]any)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	payload["mcpServers"] = desired
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (ms *MCPService) syncCodexServers(servers []MCPServer) error {
	path, err := codexConfigPath()
	if err != nil {
		return err
	}
	desired := make(map[string]map[string]any)
	for _, server := range servers {
		if !platformContains(server.EnablePlatform, platCodex) {
			continue
		}
		desired[server.Name] = buildCodexEntry(server)
	}
	payload := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if err := toml.Unmarshal(data, &payload); err != nil {
			payload = make(map[string]any)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	payload["mcp_servers"] = desired
	data, err := toml.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (ms *MCPService) syncGeminiServers(servers []MCPServer) error {
	path, err := geminiConfigPath()
	if err != nil {
		return err
	}
	desired := make(map[string]map[string]any)
	for _, server := range servers {
		if !platformContains(server.EnablePlatform, platGemini) {
			continue
		}
		desired[server.Name] = buildGeminiEntry(server)
	}
	payload := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &payload); err != nil {
			payload = make(map[string]any)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	payload["mcpServers"] = desired
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func platformContains(platforms []string, target string) bool {
	for _, value := range platforms {
		if value == target {
			return true
		}
	}
	return false
}

func buildClaudeDesktopEntry(server MCPServer) claudeDesktopServer {
	entry := claudeDesktopServer{Type: server.Type}
	if server.Type == "http" {
		entry.URL = server.URL
	} else {
		entry.Command = server.Command
		if len(server.Args) > 0 {
			entry.Args = server.Args
		}
		if len(server.Env) > 0 {
			entry.Env = server.Env
		}
	}
	return entry
}

func buildCodexEntry(server MCPServer) map[string]any {
	entry := make(map[string]any)
	entry["type"] = server.Type
	if server.Type == "http" {
		entry["url"] = server.URL
	} else {
		entry["command"] = server.Command
		if len(server.Args) > 0 {
			entry["args"] = server.Args
		}
		if len(server.Env) > 0 {
			entry["env"] = server.Env
		}
	}
	return entry
}

// buildGeminiEntry creates Gemini CLI MCP server config.
// Gemini uses "httpUrl" (not "url") for HTTP type, and omits the "type" field.
func buildGeminiEntry(server MCPServer) map[string]any {
	entry := make(map[string]any)
	if server.Type == "http" {
		entry["httpUrl"] = server.URL
	} else {
		entry["command"] = server.Command
		if len(server.Args) > 0 {
			entry["args"] = server.Args
		}
		if len(server.Env) > 0 {
			entry["env"] = server.Env
		}
	}
	return entry
}

func claudeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, claudeMcpFile), nil
}

func codexConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, codexDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, codexConfigFile), nil
}

func geminiConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, geminiDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, geminiConfigFile), nil
}

func detectPlaceholders(url string, args []string) []string {
	set := make(map[string]struct{})
	collectPlaceholders(set, url)
	for _, arg := range args {
		collectPlaceholders(set, arg)
	}
	if len(set) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(set))
	for key := range set {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func collectPlaceholders(set map[string]struct{}, value string) {
	if value == "" {
		return
	}
	matches := placeholderPattern.FindAllStringSubmatch(value, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		set[match[1]] = struct{}{}
	}
}
