package main

import (
	"codeswitch/services"
	"embed"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
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
	App        *application.App
	TrayWindow application.Window
}

func (a *AppService) SetApp(app *application.App) {
	a.App = app
}

func (a *AppService) SetTrayWindowHeight(height int) {
	if runtime.GOOS != "darwin" || a.TrayWindow == nil {
		return
	}
	if height < trayWindowMinHeight {
		height = trayWindowMinHeight
	}
	if height > trayWindowMaxHeight {
		height = trayWindowMaxHeight
	}
	a.TrayWindow.SetSize(trayWindowWidth, height)
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

	// 【更新恢复】全平台：检查并从失败的更新中恢复
	checkAndRecoverFromFailedUpdate()

	// 【P1-5 加固】幂等清理：处理更新脚本崩溃导致的残留 pending 文件
	cleanupStalePendingUpdate()

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
	providerRelay := services.NewProviderRelayService(providerService, geminiService, blacklistService, notificationService, appSettings, ":18100")
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
	healthCheckService := services.NewHealthCheckService(providerService, blacklistService, settingsService)
	// 初始化健康检查数据库表
	if err := healthCheckService.Start(); err != nil {
		log.Fatalf("初始化健康检查服务失败: %v", err)
	}
	dockService := dock.New()
	versionService := NewVersionService()
	consoleService := services.NewConsoleService()
	customCliService := services.NewCustomCliService(providerRelay.Addr())
	networkService := services.NewNetworkService(providerRelay.Addr(), claudeSettings, codexSettings, geminiService)

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
	blacklistStopChan := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := blacklistService.AutoRecoverExpired(); err != nil {
					log.Printf("自动恢复黑名单失败: %v", err)
				}
			case <-blacklistStopChan:
				log.Println("✅ 黑名单定时器已停止")
				return
			}
		}
	}()

	// 根据应用设置决定是否启动可用性监控（复用旧的 auto_connectivity_test 字段）
	go func() {
		time.Sleep(3 * time.Second) // 延迟3秒，等待应用初始化
		settings, err := appSettings.GetAppSettings()

		// 默认启用自动监控（保持开箱即用）
		autoEnabled := true
		if err != nil {
			log.Printf("读取应用设置失败（使用默认值）: %v", err)
		} else {
			// 读取成功，使用配置值
			autoEnabled = settings.AutoConnectivityTest
		}

		// 旧的 AutoConnectivityTest 字段现在控制可用性监控
		if autoEnabled {
			healthCheckService.SetAutoAvailabilityPolling(true)
			log.Println("✅ 自动可用性监控已启动")
		} else {
			log.Println("ℹ️  自动可用性监控已禁用（可在设置中开启）")
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
			application.NewService(healthCheckService),
			application.NewService(dockService),
			application.NewService(versionService),
			application.NewService(geminiService),
			application.NewService(consoleService),
			application.NewService(customCliService),
			application.NewService(networkService),
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
		log.Println("🛑 应用正在关闭，停止后台服务...")

		// 1. 停止黑名单定时器
		close(blacklistStopChan)

		// 2. 停止健康检查轮询
		healthCheckService.StopBackgroundPolling()
		log.Println("✅ 健康检查服务已停止")

		// 3. 停止更新定时器
		updateService.StopDailyCheck()
		log.Println("✅ 更新检查服务已停止")

		// 4. 停止代理服务器
		_ = providerRelay.Stop()

		// 5. 优雅关闭数据库写入队列（10秒超时，双队列架构）
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

		log.Println("✅ 所有后台服务已停止")
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

	var trayWindow application.Window
	if runtime.GOOS == "darwin" {
		trayWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:       "Code Switch Tray",
			Name:        "tray",
			Width:       trayWindowWidth,
			Height:      trayWindowMinHeight,
			MinWidth:    trayWindowWidth,
			MaxWidth:    trayWindowWidth,
			MinHeight:   trayWindowMinHeight,
			MaxHeight:   trayWindowMaxHeight,
			AlwaysOnTop: true,
			DisableResize: true,
			Frameless:     true,
			Hidden:        true,
			BackgroundType: application.BackgroundTypeTransparent,
			BackgroundColour: application.NewRGBA(0, 0, 0, 0),
			Mac: application.MacWindow{
				Backdrop:     application.MacBackdropTransparent,
				TitleBar:     application.MacTitleBarHidden,
				DisableShadow: true,
				WindowLevel:  application.MacWindowLevelPopUpMenu,
			},
			URL: "/#/tray",
		})
		trayWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			trayWindow.Hide()
			e.Cancel()
		})
		appservice.TrayWindow = trayWindow
	}

	systray := app.SystemTray.New()
	// systray.SetLabel("AI Code Studio")
	systray.SetTooltip("AI Code Studio")
	if lightIcon := loadTrayIcon("assets/icon.png"); len(lightIcon) > 0 {
		systray.SetIcon(lightIcon)
	}
	if darkIcon := loadTrayIcon("assets/icon-dark.png"); len(darkIcon) > 0 {
		systray.SetDarkModeIcon(darkIcon)
	}

	if runtime.GOOS == "darwin" && trayWindow != nil {
		trayMenu := application.NewMenu()
		trayMenu.Add("显示主窗口").OnClick(func(ctx *application.Context) {
			showMainWindow(true)
		})
		trayMenu.Add("退出").OnClick(func(ctx *application.Context) {
			app.Quit()
		})
		systray.SetMenu(trayMenu)
		systray.AttachWindow(trayWindow).WindowOffset(8)
		systray.OnRightClick(func() {
			systray.OpenMenu()
		})
	} else {
		refreshTrayMenu := func() {
			used, total := getTrayUsage(logService, appSettings)
			trayMenu := buildUsageTrayMenu(used, total, func() {
				showMainWindow(true)
			}, func() {
				app.Quit()
			})
			systray.SetMenu(trayMenu)
		}
		refreshTrayMenu()
		systray.OnRightClick(func() {
			refreshTrayMenu()
			systray.OpenMenu()
		})
		systray.OnClick(func() {
			if !mainWindow.IsVisible() {
				showMainWindow(true)
				return
			}
			if !mainWindow.IsFocused() {
				focusMainWindow()
			}
		})
	}

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

const (
	trayWindowWidth     = 360
	trayWindowMinHeight = 120
	trayWindowMaxHeight = 420
	trayProgressBarWidth = 28
)

func getTrayUsage(logService *services.LogService, appSettings *services.AppSettingsService) (float64, float64) {
	used := 0.0
	total := 0.0
	if logService != nil {
		stats, err := logService.StatsSince("")
		if err == nil {
			used = stats.CostTotal
		}
	}
	if appSettings != nil {
		settings, err := appSettings.GetAppSettings()
		if err == nil {
			total = settings.BudgetTotal
		}
	}
	if used < 0 {
		used = 0
	}
	if total < 0 {
		total = 0
	}
	return used, total
}

func buildUsageTrayMenu(used float64, total float64, onShow func(), onQuit func()) *application.Menu {
	menu := application.NewMenu()
	menu.Add(trayUsageLabel(used, total)).SetEnabled(false)
	menu.Add(trayProgressLabel(used, total)).SetEnabled(false)
	menu.AddSeparator()
	menu.Add("显示主窗口").OnClick(func(ctx *application.Context) {
		onShow()
	})
	menu.Add("退出").OnClick(func(ctx *application.Context) {
		onQuit()
	})
	return menu
}

func trayUsageLabel(used float64, total float64) string {
	usedLabel := formatCurrency(used)
	if total <= 0 {
		return fmt.Sprintf("今日已用 %s / 未设置", usedLabel)
	}
	return fmt.Sprintf("今日已用 %s / %s", usedLabel, formatCurrency(total))
}

func trayProgressLabel(used float64, total float64) string {
	bar := strings.Repeat("-", trayProgressBarWidth)
	if total <= 0 {
		return fmt.Sprintf("进度 [%s] --%%", bar)
	}
	ratio := used / total
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(math.Round(ratio * float64(trayProgressBarWidth)))
	if filled < 0 {
		filled = 0
	}
	if filled > trayProgressBarWidth {
		filled = trayProgressBarWidth
	}
	bar = strings.Repeat("#", filled) + strings.Repeat("-", trayProgressBarWidth-filled)
	percent := int(math.Round(ratio * 100))
	return fmt.Sprintf("进度 [%s] %d%%", bar, percent)
}

func formatCurrency(value float64) string {
	return fmt.Sprintf("$%.2f", value)
}

// ============================================================
// 更新系统：启动恢复（全平台）和清理功能
// ============================================================

// checkAndRecoverFromFailedUpdate 检查并从失败的更新中恢复
// 在主程序启动时调用，处理更新脚本崩溃或更新失败的情况
// P1-7: 扩展支持 macOS 和 Linux
func checkAndRecoverFromFailedUpdate() {
	switch runtime.GOOS {
	case "windows":
		recoverWindowsUpdate()
	case "darwin":
		recoverDarwinUpdate()
	case "linux":
		recoverLinuxUpdate()
	}
}

// recoverWindowsUpdate Windows 平台更新恢复
func recoverWindowsUpdate() {
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

	log.Printf("[Recovery-Win] 检测到备份文件: %s (size=%d)", backupPath, backupInfo.Size())

	// 检查当前 exe 是否可用（大小 > 1MB）
	currentInfo, err := os.Stat(currentExe)
	if err != nil {
		// 当前 exe 不存在或无法访问，需要回滚
		log.Printf("[Recovery-Win] 当前版本不可访问: %v，从备份恢复", err)
		if err := os.Rename(backupPath, currentExe); err != nil {
			log.Printf("[Recovery-Win] 回滚失败: %v", err)
			log.Println("[Recovery-Win] 请手动将备份文件恢复为原文件名")
		} else {
			log.Println("[Recovery-Win] 回滚成功，已恢复到旧版本")
		}
		return
	}

	if currentInfo.Size() > 1024*1024 {
		// 当前版本正常（>1MB），说明更新成功，清理备份
		log.Println("[Recovery-Win] 更新成功，清理旧版本备份")
		if err := os.Remove(backupPath); err != nil {
			log.Printf("[Recovery-Win] 删除备份失败: %v", err)
		}
	} else {
		// 当前版本损坏（<1MB），需要回滚
		log.Printf("[Recovery-Win] 当前版本异常（size=%d < 1MB），从备份恢复", currentInfo.Size())
		if err := os.Remove(currentExe); err != nil {
			log.Printf("[Recovery-Win] 删除损坏文件失败: %v", err)
		}
		if err := os.Rename(backupPath, currentExe); err != nil {
			log.Printf("[Recovery-Win] 回滚失败: %v", err)
			log.Println("[Recovery-Win] 请手动将备份文件恢复为原文件名")
		} else {
			log.Println("[Recovery-Win] 回滚成功，已恢复到旧版本")
		}
	}
}

// recoverDarwinUpdate macOS 平台更新恢复
func recoverDarwinUpdate() {
	currentExe, err := os.Executable()
	if err != nil {
		return
	}
	currentExe, _ = filepath.EvalSymlinks(currentExe)

	// 定位 .app 包路径
	appPath := currentExe
	for i := 0; i < 6; i++ {
		if strings.HasSuffix(strings.ToLower(appPath), ".app") {
			break
		}
		parent := filepath.Dir(appPath)
		if parent == appPath {
			break
		}
		appPath = parent
	}
	if !strings.HasSuffix(strings.ToLower(appPath), ".app") {
		return // 无法定位 .app 包
	}

	backupPath := appPath + ".old"

	// 检查备份是否存在
	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		return // 无备份，正常情况
	}

	log.Printf("[Recovery-Mac] 检测到备份应用包: %s", backupPath)

	// 检查当前 .app 是否可用（目录存在且包含 Info.plist）
	infoPlist := filepath.Join(appPath, "Contents", "Info.plist")
	if _, err := os.Stat(infoPlist); err != nil {
		// 当前 .app 损坏，需要回滚
		log.Printf("[Recovery-Mac] 当前版本损坏（Info.plist 不存在），从备份恢复")
		if err := os.RemoveAll(appPath); err != nil {
			log.Printf("[Recovery-Mac] 删除损坏目录失败: %v", err)
		}
		if err := os.Rename(backupPath, appPath); err != nil {
			log.Printf("[Recovery-Mac] 回滚失败: %v", err)
			log.Println("[Recovery-Mac] 请手动将备份应用恢复为原名称")
		} else {
			log.Println("[Recovery-Mac] 回滚成功，已恢复到旧版本")
		}
		return
	}

	// 当前版本正常，清理备份
	log.Println("[Recovery-Mac] 更新成功，清理旧版本备份")
	if err := os.RemoveAll(backupPath); err != nil {
		log.Printf("[Recovery-Mac] 删除备份失败: %v", err)
	}
	_ = backupInfo // 使用变量避免编译警告
}

// recoverLinuxUpdate Linux 平台更新恢复
func recoverLinuxUpdate() {
	currentExe, err := os.Executable()
	if err != nil {
		return
	}

	// AppImage 运行时 os.Executable() 返回 /tmp/.mount_* 内部路径
	// 使用 APPIMAGE 环境变量获取真实路径
	targetExe := currentExe
	appimageEnv := strings.TrimSpace(os.Getenv("APPIMAGE"))
	isAppImageMount := strings.Contains(currentExe, "/.mount_")

	if isAppImageMount && appimageEnv != "" && filepath.IsAbs(appimageEnv) {
		if !strings.Contains(appimageEnv, "/.mount_") {
			if resolved, err := filepath.EvalSymlinks(appimageEnv); err == nil {
				if !strings.Contains(resolved, "/.mount_") {
					targetExe = resolved
				}
			}
		}
	} else {
		targetExe, _ = filepath.EvalSymlinks(currentExe)
	}

	backupPath := targetExe + ".old"

	// 检查备份文件是否存在
	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		return // 无备份，正常情况
	}

	log.Printf("[Recovery-Linux] 检测到备份文件: %s (size=%d)", backupPath, backupInfo.Size())

	// 检查当前文件是否可用（大小 > 1MB 且为 ELF 格式）
	currentInfo, err := os.Stat(targetExe)
	if err != nil {
		// 当前文件不存在，需要回滚
		log.Printf("[Recovery-Linux] 当前版本不可访问: %v，从备份恢复", err)
		if err := os.Rename(backupPath, targetExe); err != nil {
			log.Printf("[Recovery-Linux] 回滚失败: %v", err)
			log.Println("[Recovery-Linux] 请手动将备份文件恢复为原文件名")
		} else {
			log.Println("[Recovery-Linux] 回滚成功，已恢复到旧版本")
		}
		return
	}

	// 检查文件大小和 ELF magic
	isValid := currentInfo.Size() > 1024*1024
	if isValid {
		f, err := os.Open(targetExe)
		if err == nil {
			magic := make([]byte, 4)
			n, _ := f.Read(magic)
			f.Close()
			isValid = n == 4 && magic[0] == 0x7F && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F'
		}
	}

	if isValid {
		// 当前版本正常，清理备份
		log.Println("[Recovery-Linux] 更新成功，清理旧版本备份")
		if err := os.Remove(backupPath); err != nil {
			log.Printf("[Recovery-Linux] 删除备份失败: %v", err)
		}
	} else {
		// 当前版本损坏，需要回滚
		log.Printf("[Recovery-Linux] 当前版本异常（size=%d 或非 ELF），从备份恢复", currentInfo.Size())
		if err := os.Remove(targetExe); err != nil {
			log.Printf("[Recovery-Linux] 删除损坏文件失败: %v", err)
		}
		if err := os.Rename(backupPath, targetExe); err != nil {
			log.Printf("[Recovery-Linux] 回滚失败: %v", err)
			log.Println("[Recovery-Linux] 请手动将备份文件恢复为原文件名")
		} else {
			log.Println("[Recovery-Linux] 回滚成功，已恢复到旧版本")
		}
	}
}

// cleanupStalePendingUpdate 清理残留的 pending 文件
// P1-5 加固：处理更新脚本崩溃但更新实际成功的情况
// 场景：脚本成功替换文件并重启应用，但在清理 pending 前崩溃
func cleanupStalePendingUpdate() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	pendingFile := filepath.Join(home, ".code-switch", ".pending-update")

	// 检查 pending 文件是否存在
	data, err := os.ReadFile(pendingFile)
	if err != nil {
		return // 无 pending 文件，正常情况
	}

	// 解析 pending 文件获取版本
	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		// 无法解析，删除损坏的 pending 文件
		log.Printf("[Cleanup-Pending] 无法解析 pending 文件，删除: %s", pendingFile)
		os.Remove(pendingFile)
		return
	}

	pendingVersion, ok := metadata["version"].(string)
	if !ok || pendingVersion == "" {
		// 无版本信息，删除
		log.Printf("[Cleanup-Pending] pending 文件缺少版本信息，删除: %s", pendingFile)
		os.Remove(pendingFile)
		return
	}

	// 比较版本：如果当前版本 >= pending 版本，说明更新已成功
	// 使用简单字符串比较（版本号格式为 vX.Y.Z）
	// 如果当前版本等于或高于 pending 版本，说明更新成功但脚本没有清理
	currentVersion := AppVersion
	if currentVersion == pendingVersion || versionGreaterOrEqual(currentVersion, pendingVersion) {
		log.Printf("[Cleanup-Pending] 检测到残留 pending（当前=%s，pending=%s），更新已成功，清理残留", currentVersion, pendingVersion)
		if err := os.Remove(pendingFile); err != nil {
			log.Printf("[Cleanup-Pending] 删除 pending 文件失败: %v", err)
		} else {
			log.Println("[Cleanup-Pending] 已清理残留 pending 文件")
		}
		return
	}

	// 当前版本 < pending 版本，说明更新尚未完成（可能是重启后待安装）
	// 不删除 pending，让 ApplyUpdate() 处理
	log.Printf("[Cleanup-Pending] 检测到待安装更新（当前=%s，pending=%s），保留 pending", currentVersion, pendingVersion)
}

// versionGreaterOrEqual 比较版本号（简化实现，假设格式为 vX.Y.Z）
func versionGreaterOrEqual(current, target string) bool {
	// 移除 v 前缀
	current = strings.TrimPrefix(current, "v")
	target = strings.TrimPrefix(target, "v")

	// 分割版本号
	currentParts := strings.Split(current, ".")
	targetParts := strings.Split(target, ".")

	// 比较各部分
	for i := 0; i < len(currentParts) && i < len(targetParts); i++ {
		c, _ := strconv.Atoi(currentParts[i])
		t, _ := strconv.Atoi(targetParts[i])
		if c > t {
			return true
		}
		if c < t {
			return false
		}
	}

	// 如果前面都相等，比较长度
	return len(currentParts) >= len(targetParts)
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
