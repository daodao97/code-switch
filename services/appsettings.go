package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const (
	appSettingsDir  = ".codex-swtich"
	appSettingsFile = "app.json"
)

type AppSettings struct {
	ShowHeatmap   bool `json:"show_heatmap"`
	ShowHomeTitle bool `json:"show_home_title"`
	AutoStart     bool `json:"auto_start"`
}

type AppSettingsService struct {
	path             string
	mu               sync.Mutex
	autoStartService *AutoStartService
}

func NewAppSettingsService(autoStartService *AutoStartService) *AppSettingsService {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	path := filepath.Join(home, appSettingsDir, appSettingsFile)
	return &AppSettingsService{
		path:             path,
		autoStartService: autoStartService,
	}
}

func (as *AppSettingsService) defaultSettings() AppSettings {
	// 检查当前开机自启动状态
	autoStartEnabled := false
	if as.autoStartService != nil {
		if enabled, err := as.autoStartService.IsEnabled(); err == nil {
			autoStartEnabled = enabled
		}
	}

	return AppSettings{
		ShowHeatmap:   true,
		ShowHomeTitle: true,
		AutoStart:     autoStartEnabled,
	}
}

// GetAppSettings returns the persisted app settings or defaults if the file does not exist.
func (as *AppSettingsService) GetAppSettings() (AppSettings, error) {
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.loadLocked()
}

// SaveAppSettings persists the provided settings to disk.
func (as *AppSettingsService) SaveAppSettings(settings AppSettings) (AppSettings, error) {
	as.mu.Lock()
	defer as.mu.Unlock()

	// 同步开机自启动状态
	if as.autoStartService != nil {
		if settings.AutoStart {
			if err := as.autoStartService.Enable(); err != nil {
				return settings, err
			}
		} else {
			if err := as.autoStartService.Disable(); err != nil {
				return settings, err
			}
		}
	}

	if err := as.saveLocked(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func (as *AppSettingsService) loadLocked() (AppSettings, error) {
	settings := as.defaultSettings()
	data, err := os.ReadFile(as.path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return settings, err
	}
	if len(data) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func (as *AppSettingsService) saveLocked(settings AppSettings) error {
	dir := filepath.Dir(as.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(as.path, data, 0o644)
}
