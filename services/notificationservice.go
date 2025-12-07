package services

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed assets/icon.png
var notifyIconFS embed.FS

// NotificationService 系统通知服务
// @author sm
type NotificationService struct {
	appSettings    *AppSettingsService
	app            *application.App // Wails 应用实例，用于发送事件
	mu             sync.RWMutex
	lastNotifyTime time.Time
	minInterval    time.Duration // 通知最小间隔，防止刷屏
	iconPath       string        // 缓存的图标路径
}

// SwitchNotification 切换通知的详细信息
type SwitchNotification struct {
	FromProvider string // 原供应商
	ToProvider   string // 新供应商
	Reason       string // 切换原因
	Platform     string // 平台：claude/codex/gemini
}

// NewNotificationService 创建通知服务
func NewNotificationService(appSettings *AppSettingsService) *NotificationService {
	ns := &NotificationService{
		appSettings: appSettings,
		minInterval: 3 * time.Second, // 3秒内不重复通知
	}
	// 初始化图标路径
	ns.iconPath = ns.ensureIconFile()
	return ns
}

// SetApp 设置 Wails 应用实例（用于发送事件到前端）
// @author sm
func (ns *NotificationService) SetApp(app *application.App) {
	ns.app = app
}

// ensureIconFile 确保图标文件存在于临时目录，并返回路径
// @author sm
func (ns *NotificationService) ensureIconFile() string {
	// 获取用户配置目录
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("[Notification] 获取用户目录失败: %v", err)
		return ""
	}

	iconDir := filepath.Join(homeDir, ".code-switch", "icons")
	if err := os.MkdirAll(iconDir, 0755); err != nil {
		log.Printf("[Notification] 创建图标目录失败: %v", err)
		return ""
	}

	iconPath := filepath.Join(iconDir, "app-icon.png")

	// 检查文件是否已存在
	if _, err := os.Stat(iconPath); err == nil {
		return iconPath
	}

	// 从嵌入文件系统读取图标
	iconData, err := notifyIconFS.ReadFile("assets/icon.png")
	if err != nil {
		log.Printf("[Notification] 读取嵌入图标失败: %v", err)
		return ""
	}

	// 写入到临时文件
	if err := os.WriteFile(iconPath, iconData, 0644); err != nil {
		log.Printf("[Notification] 写入图标文件失败: %v", err)
		return ""
	}

	log.Printf("[Notification] 图标文件已创建: %s", iconPath)
	return iconPath
}

// isEnabled 检查通知是否开启
func (ns *NotificationService) isEnabled() bool {
	if ns.appSettings == nil {
		return true // 默认开启
	}
	settings, err := ns.appSettings.GetAppSettings()
	if err != nil {
		return true // 获取失败时默认开启
	}
	return settings.EnableSwitchNotify
}

// NotifyProviderSwitch 发送供应商切换通知（异步，不阻塞主流程）
func (ns *NotificationService) NotifyProviderSwitch(info SwitchNotification) {
	if !ns.isEnabled() {
		return
	}

	ns.mu.Lock()
	lastTime := ns.lastNotifyTime
	ns.mu.Unlock()

	// 防刷屏：检查是否在最小间隔内
	if time.Since(lastTime) < ns.minInterval {
		log.Printf("[Notification] 通知被节流，距上次通知仅 %v", time.Since(lastTime))
		return
	}

	// 异步发送通知
	go ns.sendSwitchNotification(info)
}

// sendSwitchNotification 实际发送切换通知的内部方法
func (ns *NotificationService) sendSwitchNotification(info SwitchNotification) {
	ns.mu.Lock()
	ns.lastNotifyTime = time.Now()
	ns.mu.Unlock()

	// 简化通知内容：仅显示已切换到哪个供应商
	title := "Code Switch"
	body := fmt.Sprintf("已切换到 %s", info.ToProvider)

	// 发送 Wails 事件到前端（用于点击通知后定位）
	ns.emitSwitchEvent(info)

	// 使用 beeep 发送系统通知，带应用图标
	if err := beeep.Notify(title, body, ns.iconPath); err != nil {
		log.Printf("[Notification] 发送通知失败: %v", err)
	} else {
		log.Printf("[Notification] 已发送切换通知: %s → %s", info.FromProvider, info.ToProvider)
	}
}

// emitSwitchEvent 发送切换事件到前端
// @author sm
func (ns *NotificationService) emitSwitchEvent(info SwitchNotification) {
	if ns.app == nil {
		return
	}
	ns.app.Event.Emit("provider:switched", map[string]interface{}{
		"platform":     info.Platform,
		"fromProvider": info.FromProvider,
		"toProvider":   info.ToProvider,
		"reason":       info.Reason,
		"timestamp":    time.Now().UnixMilli(),
	})
}

// NotifyProviderBlacklisted 发送供应商被拉黑通知
func (ns *NotificationService) NotifyProviderBlacklisted(platform, providerName string, level int, durationMinutes int) {
	if !ns.isEnabled() {
		return
	}

	go func() {
		// 简化通知内容
		title := "Code Switch"
		body := fmt.Sprintf("%s 已拉黑 %d 分钟", providerName, durationMinutes)

		// 发送 Wails 事件到前端
		ns.emitBlacklistEvent(platform, providerName, level, durationMinutes)

		// 使用 beeep 发送系统通知，带应用图标
		if err := beeep.Notify(title, body, ns.iconPath); err != nil {
			log.Printf("[Notification] 发送拉黑通知失败: %v", err)
		} else {
			log.Printf("[Notification] 已发送拉黑通知: %s (L%d, %d分钟)", providerName, level, durationMinutes)
		}
	}()
}

// emitBlacklistEvent 发送拉黑事件到前端
// @author sm
func (ns *NotificationService) emitBlacklistEvent(platform, providerName string, level, durationMinutes int) {
	if ns.app == nil {
		return
	}
	ns.app.Event.Emit("provider:blacklisted", map[string]interface{}{
		"platform":        platform,
		"providerName":    providerName,
		"level":           level,
		"durationMinutes": durationMinutes,
		"timestamp":       time.Now().UnixMilli(),
	})
}
