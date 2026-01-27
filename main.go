package main

import (
	"codeswitch/services"
	"embed"
	_ "embed"
	"fmt"
	"log"
	"math"
	"runtime"
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
	updateService := services.NewUpdateService(AppVersion)
	consoleService := services.NewConsoleService()
	customCliService := services.NewCustomCliService(providerRelay.Addr())
	networkService := services.NewNetworkService(providerRelay.Addr(), claudeSettings, codexSettings, geminiService)

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
			application.NewService(updateService),
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
	// 设置 UpdateService 的 App 引用，用于发送更新事件
	updateService.SetApp(app)

	app.OnShutdown(func() {
		log.Println("🛑 应用正在关闭，停止后台服务...")

		// 1. 停止黑名单定时器
		close(blacklistStopChan)

		// 2. 停止健康检查轮询
		healthCheckService.StopBackgroundPolling()
		log.Println("✅ 健康检查服务已停止")

		// 3. 停止代理服务器
		_ = providerRelay.Stop()

		// 4. 优雅关闭数据库写入队列（10秒超时，双队列架构）
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
		Width:     1400,
		Height:    1040,
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

	var trayWindow application.Window

	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(event *application.ApplicationEvent) {
		showMainWindow(true)
	})

	app.Event.OnApplicationEvent(events.Mac.ApplicationDidBecomeActive, func(event *application.ApplicationEvent) {
		if trayWindow != nil {
			// Tray exists on macOS; avoid auto-opening the main window on activation.
			return
		}
		if mainWindow.IsVisible() {
			mainWindow.Focus()
			return
		}
		showMainWindow(true)
	})

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
	adjustment := 0.0
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
			adjustment = settings.BudgetUsedAdjustment
		}
	}
	used += adjustment
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

