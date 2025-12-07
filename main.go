package main

import (
	"codeswitch/services"
	"embed"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

//go:embed assets/icon.png assets/icon-dark.png
var trayIcons embed.FS

type AppService struct {
	App *application.App
}

func (a *AppService) SetApp(app *application.App) {
	a.App = app
}

func (a *AppService) OpenSecondWindow() {
	if a.App == nil {
		fmt.Println("[ERROR] app not initialized")
		return
	}
	name := fmt.Sprintf("logs-%d", time.Now().UnixNano())
	win := a.App.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Logs",
		Name:      name,
		Width:     1024,
		Height:    800,
		MinWidth:  600,
		MinHeight: 300,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			TitleBar:                application.MacTitleBarHidden,
			Backdrop:                application.MacBackdropTransparent,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/#/logs",
	})
	win.Center()
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {
	appservice := &AppService{}

	// 【更新恢复】Windows 平台：检查并从失败的更新中恢复
	checkAndRecoverFromFailedUpdate()

	// 【残留清理】全平台：清理更新过程中的临时文件（Windows/Linux/macOS）
	cleanupOldFiles()

	// 【修复】第一步：初始化数据库（必须最先执行）
	// 解决问题：InitGlobalDBQueue 依赖 xdb.DB("default")，但 xdb.Inits() 在 NewProviderRelayService 中
	if err := services.InitDatabase(); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	log.Println("✅ 数据库已初始化")

	// 【修复】第二步：初始化写入队列（依赖数据库连接）
	if err := services.InitGlobalDBQueue(); err != nil {
		log.Fatalf("初始化数据库队列失败: %v", err)
	}
	log.Println("✅ 数据库写入队列已启动")

	// 【修复】第三步：创建服务（现在可以安全使用数据库了）
	suiService, errt := services.NewSuiStore()
	if errt != nil {
		log.Fatalf("SuiStore 初始化失败: %v", errt)
	}

	providerService := services.NewProviderService()
	settingsService := services.NewSettingsService()
	autoStartService := services.NewAutoStartService()
	appSettings := services.NewAppSettingsService(autoStartService)
	notificationService := services.NewNotificationService(appSettings) // 通知服务
	blacklistService := services.NewBlacklistService(settingsService, notificationService)
	geminiService := services.NewGeminiService("127.0.0.1:18100")
	providerRelay := services.NewProviderRelayService(providerService, geminiService, blacklistService, notificationService, ":18100")
	claudeSettings := services.NewClaudeSettingsService(providerRelay.Addr())
	codexSettings := services.NewCodexSettingsService(providerRelay.Addr())
	cliConfigService := services.NewCliConfigService(providerRelay.Addr())
	logService := services.NewLogService()
	updateService := services.NewUpdateService(AppVersion)
	mcpService := services.NewMCPService()
	skillService := services.NewSkillService()
	promptService := services.NewPromptService()
	envCheckService := services.NewEnvCheckService()
	importService := services.NewImportService(providerService, mcpService)
	deeplinkService := services.NewDeepLinkService(providerService)
	speedTestService := services.NewSpeedTestService()
	connectivityTestService := services.NewConnectivityTestService(providerService, blacklistService, settingsService)
	dockService := dock.New()
	versionService := NewVersionService()
	consoleService := services.NewConsoleService()

	// 应用待处理的更新
	go func() {
		time.Sleep(2 * time.Second)
		if err := updateService.ApplyUpdate(); err != nil {
			log.Printf("应用更新失败: %v", err)
		}
	}()

	// 启动定时检查（如果启用）
	if updateService.IsAutoCheckEnabled() {
		go func() {
			time.Sleep(10 * time.Second) // 延迟10秒，等待应用完成初始化
			updateService.CheckUpdateAsync() // 启动时检查一次
			updateService.StartDailyCheck()  // 启动每日8点定时检查
		}()
	}

	go func() {
		if err := providerRelay.Start(); err != nil {
			log.Printf("provider relay start error: %v", err)
		}
	}()

	// 启动黑名单自动恢复定时器（每分钟检查一次）
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			if err := blacklistService.AutoRecoverExpired(); err != nil {
				log.Printf("自动恢复黑名单失败: %v", err)
			}
		}
	}()

	// 根据 AppSettings 配置启动自动连通性检测
	go func() {
		time.Sleep(3 * time.Second) // 延迟3秒，等待应用初始化
		settings, err := appSettings.GetAppSettings()
		if err != nil {
			log.Printf("读取应用设置失败: %v", err)
			return
		}
		if settings.AutoConnectivityTest {
			if err := connectivityTestService.SetAutoTestEnabled(true); err != nil {
				log.Printf("启动自动连通性检测失败: %v", err)
			} else {
				log.Println("✅ 自动连通性检测已启动")
			}
		}
	}()

	//fmt.Println(clipboardService)
	// Create a new Wails application by providing the necessary options.
	// Variables 'Name' and 'Description' are for application metadata.
	// 'Assets' configures the asset server with the 'FS' variable pointing to the frontend files.
	// 'Bind' is a list of Go struct instances. The frontend has access to the methods of these instances.
	// 'Mac' options tailor the application when running an macOS.
	app := application.New(application.Options{
		Name:        "AI Code Studio",
		Description: "Claude Code and Codex provier manager",
		Services: []application.Service{
			application.NewService(appservice),
			application.NewService(suiService),
			application.NewService(providerService),
			application.NewService(settingsService),
			application.NewService(blacklistService),
			application.NewService(claudeSettings),
			application.NewService(codexSettings),
			application.NewService(cliConfigService),
			application.NewService(logService),
			application.NewService(appSettings),
			application.NewService(updateService),
			application.NewService(mcpService),
			application.NewService(skillService),
			application.NewService(promptService),
			application.NewService(envCheckService),
			application.NewService(importService),
			application.NewService(deeplinkService),
			application.NewService(speedTestService),
			application.NewService(connectivityTestService),
			application.NewService(dockService),
			application.NewService(versionService),
			application.NewService(geminiService),
			application.NewService(consoleService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	// 设置 NotificationService 的 App 引用，用于发送事件到前端
	notificationService.SetApp(app)

	app.OnShutdown(func() {
		_ = providerRelay.Stop()

		// 优雅关闭数据库写入队列（10秒超时，双队列架构）
		if err := services.ShutdownGlobalDBQueue(10 * time.Second); err != nil {
			log.Printf("⚠️ 队列关闭超时: %v", err)
		} else {
			// 单次队列统计
			stats1 := services.GetGlobalDBQueueStats()
			log.Printf("✅ 单次队列已关闭，统计：成功=%d 失败=%d 平均延迟=%.2fms",
				stats1.SuccessWrites, stats1.FailedWrites, stats1.AvgLatencyMs)

			// 批量队列统计
			stats2 := services.GetGlobalDBQueueLogsStats()
			log.Printf("✅ 批量队列已关闭，统计：成功=%d 失败=%d 平均延迟=%.2fms（批均分） 批次=%d",
				stats2.SuccessWrites, stats2.FailedWrites, stats2.AvgLatencyMs, stats2.BatchCommits)
		}
	})

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Code Switch R",
		Width:     1024,
		Height:    800,
		MinWidth:  600,
		MinHeight: 300,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})
	var mainWindowCentered bool
	focusMainWindow := func() {
		if runtime.GOOS == "windows" {
			mainWindow.SetAlwaysOnTop(true)
			mainWindow.Focus()
			go func() {
				time.Sleep(150 * time.Millisecond)
				mainWindow.SetAlwaysOnTop(false)
			}()
			return
		}
		mainWindow.Focus()
	}
	showMainWindow := func(withFocus bool) {
		if !mainWindowCentered {
			mainWindow.Center()
			mainWindowCentered = true
		}
		if mainWindow.IsMinimised() {
			mainWindow.UnMinimise()
		}
		mainWindow.Show()
		if withFocus {
			focusMainWindow()
		}
		handleDockVisibility(dockService, true)
	}

	showMainWindow(false)

	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		mainWindow.Hide()
		handleDockVisibility(dockService, false)
		e.Cancel()
	})

	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(event *application.ApplicationEvent) {
		showMainWindow(true)
	})

	app.Event.OnApplicationEvent(events.Mac.ApplicationDidBecomeActive, func(event *application.ApplicationEvent) {
		if mainWindow.IsVisible() {
			mainWindow.Focus()
			return
		}
		showMainWindow(true)
	})

	systray := app.SystemTray.New()
	// systray.SetLabel("AI Code Studio")
	systray.SetTooltip("AI Code Studio")
	if lightIcon := loadTrayIcon("assets/icon.png"); len(lightIcon) > 0 {
		systray.SetIcon(lightIcon)
	}
	if darkIcon := loadTrayIcon("assets/icon-dark.png"); len(darkIcon) > 0 {
		systray.SetDarkModeIcon(darkIcon)
	}

	trayMenu := application.NewMenu()
	trayMenu.Add("显示主窗口").OnClick(func(ctx *application.Context) {
		showMainWindow(true)
	})
	trayMenu.Add("退出").OnClick(func(ctx *application.Context) {
		app.Quit()
	})
	systray.SetMenu(trayMenu)

	systray.OnClick(func() {
		if !mainWindow.IsVisible() {
			showMainWindow(true)
			return
		}
		if !mainWindow.IsFocused() {
			focusMainWindow()
		}
	})

	appservice.SetApp(app)

	// Create a goroutine that emits an event containing the current time every second.
	// The frontend can listen to this event and update the UI accordingly.
	go func() {
		// for {
		// 	now := time.Now().Format(time.RFC1123)
		// 	app.EmitEvent("time", now)
		// 	time.Sleep(time.Second)
		// }
	}()

	// Run the application. This blocks until the application has been exited.
	err := app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}

func loadTrayIcon(path string) []byte {
	data, err := trayIcons.ReadFile(path)
	if err != nil {
		log.Printf("failed to load tray icon %s: %v", path, err)
		return nil
	}
	return data
}

func handleDockVisibility(service *dock.DockService, show bool) {
	if runtime.GOOS != "darwin" || service == nil {
		return
	}
	if show {
		service.ShowAppIcon()
	} else {
		service.HideAppIcon()
	}
}

// ============================================================
// 更新系统：启动恢复（Windows）和全平台清理功能
// ============================================================

// checkAndRecoverFromFailedUpdate 检查并从失败的更新中恢复
// 在主程序启动时调用，处理 updater.exe 崩溃或更新失败的情况
func checkAndRecoverFromFailedUpdate() {
	if runtime.GOOS != "windows" {
		return
	}

	currentExe, err := os.Executable()
	if err != nil {
		return
	}
	currentExe, _ = filepath.EvalSymlinks(currentExe)
	backupPath := currentExe + ".old"

	// 检查备份文件是否存在
	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		return // 无备份，正常情况
	}

	log.Printf("[Recovery] 检测到备份文件: %s (size=%d)", backupPath, backupInfo.Size())

	// 检查当前 exe 是否可用（大小 > 1MB）
	currentInfo, err := os.Stat(currentExe)
	currentOK := err == nil && currentInfo.Size() > 1024*1024 // 至少 1MB

	if currentOK {
		// 当前版本正常，说明更新成功，清理备份
		log.Println("[Recovery] 更新成功，清理旧版本备份")
		if err := os.Remove(backupPath); err != nil {
			log.Printf("[Recovery] 删除备份失败: %v", err)
		}
	} else {
		// 当前版本损坏，需要回滚
		log.Printf("[Recovery] 当前版本异常（size=%d），从备份恢复", currentInfo.Size())
		if err := os.Remove(currentExe); err != nil {
			log.Printf("[Recovery] 删除损坏文件失败: %v", err)
		}
		if err := os.Rename(backupPath, currentExe); err != nil {
			log.Printf("[Recovery] 回滚失败: %v", err)
			log.Println("[Recovery] 请手动将备份文件恢复为原文件名")
		} else {
			log.Println("[Recovery] 回滚成功，已恢复到旧版本")
		}
	}
}

// cleanupOldFiles 清理更新过程中的残留文件
// 在主程序启动时调用 - 支持所有平台
func cleanupOldFiles() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	updateDir := filepath.Join(home, ".code-switch", "updates")
	if _, err := os.Stat(updateDir); os.IsNotExist(err) {
		return // 更新目录不存在
	}

	log.Printf("[Cleanup] 开始清理更新目录: %s", updateDir)

	// 1. 清理超过 7 天的 .old 备份文件（所有平台通用）
	cleanupByAge(updateDir, ".old", 7*24*time.Hour)

	// 2. 按平台清理旧版本下载文件
	switch runtime.GOOS {
	case "windows":
		cleanupByCount(updateDir, "CodeSwitch*.exe", 1)
		cleanupByCount(updateDir, "updater*.exe", 1)
	case "linux":
		cleanupByCount(updateDir, "CodeSwitch*.AppImage", 1)
	case "darwin":
		cleanupByCount(updateDir, "codeswitch-macos-*.zip", 1)
	}

	// 3. 清理旧日志（保留最近 5 个，或总大小 < 5MB）- 所有平台通用
	cleanupLogs(updateDir, 5, 5*1024*1024)

	log.Println("[Cleanup] 清理完成")
}

// cleanupByAge 按时间清理文件
func cleanupByAge(dir, suffix string, maxAge time.Duration) {
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, suffix) && time.Since(info.ModTime()) > maxAge {
			log.Printf("[Cleanup] 删除过期文件: %s (age=%v)", path, time.Since(info.ModTime()).Round(time.Hour))
			os.Remove(path)
		}
		return nil
	})
}

// cleanupByCount 按数量清理（保留最新 N 个）
func cleanupByCount(dir, pattern string, keepCount int) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil || len(matches) <= keepCount {
		return
	}

	// 按修改时间排序（新→旧）
	sort.Slice(matches, func(i, j int) bool {
		infoI, _ := os.Stat(matches[i])
		infoJ, _ := os.Stat(matches[j])
		if infoI == nil || infoJ == nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// 删除多余的旧文件
	for _, path := range matches[keepCount:] {
		log.Printf("[Cleanup] 删除旧版本: %s", path)
		os.Remove(path)
	}
}

// cleanupLogs 日志清理（数量 + 大小双重限制）
func cleanupLogs(dir string, maxCount int, maxTotalSize int64) {
	pattern := filepath.Join(dir, "update*.log")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return
	}

	// 按修改时间排序（新→旧）
	sort.Slice(matches, func(i, j int) bool {
		infoI, _ := os.Stat(matches[i])
		infoJ, _ := os.Stat(matches[j])
		if infoI == nil || infoJ == nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	var totalSize int64
	for i, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		// 超过数量限制或大小限制，删除
		if i >= maxCount || totalSize+info.Size() > maxTotalSize {
			log.Printf("[Cleanup] 删除旧日志: %s (size=%d)", path, info.Size())
			os.Remove(path)
		} else {
			totalSize += info.Size()
		}
	}
}
