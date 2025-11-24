package main

import (
	"codeswitch/services"
	"embed"
	_ "embed"
	"fmt"
	"log"
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

	suiService, errt := services.NewSuiStore()
	if errt != nil {
		// 处理错误，比如日志或退出
	}
	providerService := services.NewProviderService()
	settingsService := services.NewSettingsService()
	blacklistService := services.NewBlacklistService(settingsService)
	providerRelay := services.NewProviderRelayService(providerService, blacklistService, ":18100")
	geminiService := services.NewGeminiService()
	claudeSettings := services.NewClaudeSettingsService(providerRelay.Addr())
	codexSettings := services.NewCodexSettingsService(providerRelay.Addr())
	logService := services.NewLogService()
	autoStartService := services.NewAutoStartService()
	updateService := services.NewUpdateService(AppVersion)
	appSettings := services.NewAppSettingsService(autoStartService)
	mcpService := services.NewMCPService()
	skillService := services.NewSkillService()
	promptService := services.NewPromptService()
	envCheckService := services.NewEnvCheckService()
	importService := services.NewImportService(providerService, mcpService)
	deeplinkService := services.NewDeepLinkService(providerService)
	speedTestService := services.NewSpeedTestService()
	dockService := dock.New()
	versionService := NewVersionService()

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
			time.Sleep(10 * time.Second) // 延迟10秒避免影响启动
			updateService.CheckUpdateAsync()
			updateService.StartDailyCheck()
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
			application.NewService(dockService),
			application.NewService(versionService),
			application.NewService(geminiService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	app.OnShutdown(func() {
		_ = providerRelay.Stop()
	})

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Code Switch",
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
