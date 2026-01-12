package services

import (
	"encoding/json"
	"errors"
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
	codexProviderKey      = "code-switch"
	codexEnvKey           = "OPENAI_API_KEY"
	codexWireAPI          = "responses"
	codexTokenValue       = "code-switch"
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
	provider, ok := config.ModelProviders[codexProviderKey]
	if !ok {
		return status, nil
	}
	baseURL := css.baseURL()
	if strings.EqualFold(config.ModelProvider, codexProviderKey) && strings.EqualFold(provider.BaseURL, baseURL) {
		status.Enabled = true
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
	raw["preferred_auth_method"] = codexPreferredAuth
	raw["model"] = codexDefaultModel
	raw["model_provider"] = codexProviderKey

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
	if err := os.WriteFile(settingsPath, cleaned, 0o600); err != nil {
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
