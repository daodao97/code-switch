package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	appSettingsDir      = ".code-switch" // 【修复】修正拼写错误（原为 .codex-swtich）
	appSettingsFile     = "app.json"
	oldSettingsDir      = ".codex-swtich"           // 旧的错误拼写
	migrationMarkerFile = ".migrated-from-codex-swtich" // 迁移标记文件
)

type AppSettings struct {
	ShowHeatmap          bool `json:"show_heatmap"`
	ShowHomeTitle        bool `json:"show_home_title"`
	AutoStart            bool `json:"auto_start"`
	AutoUpdate           bool `json:"auto_update"`
	AutoConnectivityTest bool `json:"auto_connectivity_test"`
	EnableSwitchNotify   bool `json:"enable_switch_notify"` // 供应商切换通知开关
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

	newDir := filepath.Join(home, appSettingsDir)
	newPath := filepath.Join(newDir, appSettingsFile)
	oldDir := filepath.Join(home, oldSettingsDir)
	oldPath := filepath.Join(oldDir, appSettingsFile)
	markerPath := filepath.Join(newDir, migrationMarkerFile)

	// 检查是否已经迁移过
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		// 尚未迁移，检查旧目录
		if _, err := os.Stat(oldPath); err == nil {
			// 旧文件存在，执行迁移
			if err := migrateSettings(oldPath, newPath, oldDir, markerPath); err != nil {
				fmt.Printf("[AppSettings] ⚠️  迁移配置失败: %v\n", err)
			}
		}
	}

	return &AppSettingsService{
		path:             newPath,
		autoStartService: autoStartService,
	}
}

// migrateSettings 完整的配置迁移
// 迁移顺序：写新文件 → 校验 → 标记 → 删旧
func migrateSettings(oldPath, newPath, oldDir, markerPath string) error {
	// 1. 确保新目录存在
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return fmt.Errorf("创建新目录失败: %w", err)
	}

	// 2. 检查新文件是否已存在
	if _, err := os.Stat(newPath); err == nil {
		// 新文件已存在，不覆盖，但仍创建迁移标记
		fmt.Printf("[AppSettings] 新配置文件已存在，跳过迁移\n")
	} else {
		// 3. 读取旧配置
		data, err := os.ReadFile(oldPath)
		if err != nil {
			return fmt.Errorf("读取旧配置失败: %w", err)
		}

		// 4. 写入新配置
		if err := os.WriteFile(newPath, data, 0644); err != nil {
			return fmt.Errorf("写入新配置失败: %w", err)
		}

		// 5. 校验新文件
		verifyData, err := os.ReadFile(newPath)
		if err != nil {
			// 写入成功但读取失败，回滚
			os.Remove(newPath)
			return fmt.Errorf("校验新配置失败（已回滚）: %w", err)
		}

		// 校验内容一致性
		if !bytes.Equal(data, verifyData) {
			os.Remove(newPath)
			return fmt.Errorf("配置内容校验失败（已回滚）: 写入内容与读取内容不一致")
		}

		// 如果是 JSON 文件，额外校验 JSON 格式有效性
		var jsonTest interface{}
		if err := json.Unmarshal(verifyData, &jsonTest); err != nil {
			os.Remove(newPath)
			return fmt.Errorf("JSON 格式校验失败（已回滚）: %w", err)
		}

		fmt.Printf("[AppSettings] ✅ 已迁移并校验配置: %s → %s\n", oldPath, newPath)
	}

	// 6. 创建迁移标记文件
	markerContent := fmt.Sprintf("迁移时间: %s\n旧路径: %s\n", time.Now().Format(time.RFC3339), oldDir)
	if err := os.WriteFile(markerPath, []byte(markerContent), 0644); err != nil {
		return fmt.Errorf("创建迁移标记失败: %w", err)
	}

	// 7. 只有在新文件校验通过后才删除旧目录
	if err := os.RemoveAll(oldDir); err != nil {
		// 删除失败不是致命错误，只记录警告
		fmt.Printf("[AppSettings] ⚠️  删除旧目录失败: %v（可手动删除 %s）\n", err, oldDir)
	} else {
		fmt.Printf("[AppSettings] ✅ 已删除旧目录: %s\n", oldDir)
	}

	return nil
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
		ShowHeatmap:          true,
		ShowHomeTitle:        true,
		AutoStart:            autoStartEnabled,
		AutoUpdate:           true,  // 默认开启自动更新
		AutoConnectivityTest: false, // 默认关闭自动连通性检测
		EnableSwitchNotify:   true,  // 默认开启切换通知
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
