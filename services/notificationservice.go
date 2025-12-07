package services

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
)

// NotificationService 系统通知服务
// @author sm
type NotificationService struct {
	appSettings    *AppSettingsService
	mu             sync.RWMutex
	lastNotifyTime time.Time
	minInterval    time.Duration // 通知最小间隔，防止刷屏
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
	return &NotificationService{
		appSettings: appSettings,
		minInterval: 3 * time.Second, // 3秒内不重复通知
	}
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

	title := "Code Switch - 供应商切换"
	body := fmt.Sprintf("[%s] %s → %s\n原因：%s",
		info.Platform,
		info.FromProvider,
		info.ToProvider,
		info.Reason)

	// 使用 beeep 发送系统通知
	// 第三个参数是图标路径，空字符串使用默认图标
	if err := beeep.Notify(title, body, ""); err != nil {
		log.Printf("[Notification] 发送通知失败: %v", err)
	} else {
		log.Printf("[Notification] 已发送切换通知: %s → %s", info.FromProvider, info.ToProvider)
	}
}

// NotifyProviderBlacklisted 发送供应商被拉黑通知
func (ns *NotificationService) NotifyProviderBlacklisted(platform, providerName string, level int, durationMinutes int) {
	if !ns.isEnabled() {
		return
	}

	go func() {
		title := "Code Switch - 供应商已拉黑"
		body := fmt.Sprintf("[%s] %s 已被拉黑\n等级: L%d，时长: %d 分钟",
			platform, providerName, level, durationMinutes)

		if err := beeep.Notify(title, body, ""); err != nil {
			log.Printf("[Notification] 发送拉黑通知失败: %v", err)
		} else {
			log.Printf("[Notification] 已发送拉黑通知: %s (L%d, %d分钟)", providerName, level, durationMinutes)
		}
	}()
}
