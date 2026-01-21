package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// GetBlacklistLevelConfigPath 获取等级拉黑配置文件路径
func GetBlacklistLevelConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户目录失败: %w", err)
	}

	configDir := filepath.Join(home, ".code-switch")
	// 确保目录存在
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("创建配置目录失败: %w", err)
	}

	return filepath.Join(configDir, "blacklist-config.json"), nil
}

// GetBlacklistLevelConfig 获取等级拉黑配置
// 【修复】开关状态从数据库读取，其他配置从 JSON 文件读取
func (ss *SettingsService) GetBlacklistLevelConfig() (*BlacklistLevelConfig, error) {
	configPath, err := GetBlacklistLevelConfigPath()
	if err != nil {
		return nil, err
	}

	var config *BlacklistLevelConfig

	// 【修复】始终以默认配置为基础，再用 JSON 覆盖存在的字段
	// 这样旧版 JSON 中没有的新字段（如 RetryWaitSeconds）会保留默认值，而不是零值
	config = DefaultBlacklistLevelConfig()

	// 如果配置文件存在，用其内容覆盖默认值
	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}

		// JSON Unmarshal 只会覆盖 JSON 中存在的字段，未出现的字段保持默认值
		if err := json.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("解析配置文件失败: %w", err)
		}
	}
	// 如果文件不存在，直接使用默认配置（已在上面初始化）

	// 【关键修复】从数据库读取开关状态，覆盖 JSON 文件中的值
	// 因为 UI 开关是通过 SetLevelBlacklistEnabled() 写入数据库的
	dbEnabled, err := ss.GetLevelBlacklistEnabled()
	if err == nil {
		config.EnableLevelBlacklist = dbEnabled
	}
	// 如果数据库读取失败，保留 JSON 文件中的值（向后兼容）

	// 【关键修复】从数据库读取阈值，覆盖 JSON 文件中的值
	// 因为 UI 设置的阈值是通过 UpdateBlacklistSettings() 写入数据库的
	dbThreshold, _, err := ss.GetBlacklistSettings()
	if err == nil && dbThreshold > 0 {
		config.FailureThreshold = dbThreshold
	}
	// 如果数据库读取失败，保留 JSON 文件中的值（向后兼容）

	return config, nil
}

// SaveBlacklistLevelConfig 保存等级拉黑配置
func (ss *SettingsService) SaveBlacklistLevelConfig(config *BlacklistLevelConfig) error {
	configPath, err := GetBlacklistLevelConfigPath()
	if err != nil {
		return err
	}

	// 序列化配置
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	// 原子写入：先写临时文件，再重命名
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("写入临时配置文件失败: %w", err)
	}

	if err := os.Rename(tmpPath, configPath); err != nil {
		return fmt.Errorf("重命名配置文件失败: %w", err)
	}

	return nil
}

// UpdateBlacklistLevelConfig 更新等级拉黑配置
func (ss *SettingsService) UpdateBlacklistLevelConfig(config *BlacklistLevelConfig) error {
	// 验证配置
	if err := validateBlacklistLevelConfig(config); err != nil {
		return err
	}

	return ss.SaveBlacklistLevelConfig(config)
}

// validateBlacklistLevelConfig 验证等级拉黑配置
func validateBlacklistLevelConfig(config *BlacklistLevelConfig) error {
	if config.FailureThreshold < 1 || config.FailureThreshold > 10 {
		return fmt.Errorf("失败阈值必须在 1-10 之间")
	}

	if config.DedupeWindowSeconds < 1 || config.DedupeWindowSeconds > 300 {
		return fmt.Errorf("去重窗口必须在 1-300 秒之间")
	}

	if config.NormalDegradeIntervalHours < 0.1 || config.NormalDegradeIntervalHours > 24 {
		return fmt.Errorf("正常降级间隔必须在 0.1-24 小时之间")
	}

	if config.ForgivenessHours < 0.5 || config.ForgivenessHours > 72 {
		return fmt.Errorf("宽恕触发时间必须在 0.5-72 小时之间")
	}

	if config.JumpPenaltyWindowHours < 0.1 || config.JumpPenaltyWindowHours > 24 {
		return fmt.Errorf("跳级惩罚窗口必须在 0.1-24 小时之间")
	}

	// 验证等级时长（必须递增）
	if config.L1DurationMinutes < 1 || config.L1DurationMinutes > 10080 {
		return fmt.Errorf("L1 拉黑时长必须在 1-10080 分钟之间")
	}
	if config.L2DurationMinutes <= config.L1DurationMinutes {
		return fmt.Errorf("L2 拉黑时长必须大于 L1")
	}
	if config.L3DurationMinutes <= config.L2DurationMinutes {
		return fmt.Errorf("L3 拉黑时长必须大于 L2")
	}
	if config.L4DurationMinutes <= config.L3DurationMinutes {
		return fmt.Errorf("L4 拉黑时长必须大于 L3")
	}
	if config.L5DurationMinutes <= config.L4DurationMinutes {
		return fmt.Errorf("L5 拉黑时长必须大于 L4")
	}

	if config.FallbackMode != "fixed" && config.FallbackMode != "none" {
		return fmt.Errorf("fallbackMode 只支持 'fixed' 或 'none'")
	}

	if config.FallbackDurationMinutes < 1 || config.FallbackDurationMinutes > 10080 {
		return fmt.Errorf("fallback 拉黑时长必须在 1-10080 分钟之间")
	}

	return nil
}
