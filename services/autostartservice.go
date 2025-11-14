package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type AutoStartService struct{}

func NewAutoStartService() *AutoStartService {
	return &AutoStartService{}
}

// IsEnabled 检查是否已启用开机自启动
func (as *AutoStartService) IsEnabled() (bool, error) {
	switch runtime.GOOS {
	case "windows":
		return as.isEnabledWindows()
	case "darwin":
		return as.isEnabledDarwin()
	case "linux":
		return as.isEnabledLinux()
	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Enable 启用开机自启动
func (as *AutoStartService) Enable() error {
	switch runtime.GOOS {
	case "windows":
		return as.enableWindows()
	case "darwin":
		return as.enableDarwin()
	case "linux":
		return as.enableLinux()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Disable 禁用开机自启动
func (as *AutoStartService) Disable() error {
	switch runtime.GOOS {
	case "windows":
		return as.disableWindows()
	case "darwin":
		return as.disableDarwin()
	case "linux":
		return as.disableLinux()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Windows 实现
func (as *AutoStartService) isEnabledWindows() (bool, error) {
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	cmd := exec.Command("reg", "query", key, "/v", "CodeSwitch")
	err := cmd.Run()
	return err == nil, nil
}

func (as *AutoStartService) enableWindows() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	cmd := exec.Command("reg", "add", key, "/v", "CodeSwitch", "/t", "REG_SZ", "/d", exePath, "/f")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to add registry key: %w", err)
	}
	return nil
}

func (as *AutoStartService) disableWindows() error {
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	cmd := exec.Command("reg", "delete", key, "/v", "CodeSwitch", "/f")
	// 忽略不存在的错误
	_ = cmd.Run()
	return nil
}

// macOS 实现
func (as *AutoStartService) isEnabledDarwin() (bool, error) {
	plistPath := as.getDarwinPlistPath()
	_, err := os.Stat(plistPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (as *AutoStartService) enableDarwin() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	plistPath := as.getDarwinPlistPath()
	plistDir := filepath.Dir(plistPath)
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		return fmt.Errorf("failed to create launch agents directory: %w", err)
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.codeswitch.app</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
</dict>
</plist>`, exePath)

	if err := os.WriteFile(plistPath, []byte(plistContent), 0o644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	return nil
}

func (as *AutoStartService) disableDarwin() error {
	plistPath := as.getDarwinPlistPath()
	// 忽略不存在的错误
	_ = os.Remove(plistPath)
	return nil
}

func (as *AutoStartService) getDarwinPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.codeswitch.app.plist")
}

// Linux 实现 (使用 .desktop 文件)
func (as *AutoStartService) isEnabledLinux() (bool, error) {
	desktopPath := as.getLinuxDesktopPath()
	_, err := os.Stat(desktopPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (as *AutoStartService) enableLinux() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	desktopPath := as.getLinuxDesktopPath()
	desktopDir := filepath.Dir(desktopPath)
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		return fmt.Errorf("failed to create autostart directory: %w", err)
	}

	desktopContent := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=CodeSwitch
Exec=%s
Hidden=false
NoDisplay=false
X-GNOME-Autostart-enabled=true`, exePath)

	if err := os.WriteFile(desktopPath, []byte(desktopContent), 0o644); err != nil {
		return fmt.Errorf("failed to write desktop file: %w", err)
	}

	return nil
}

func (as *AutoStartService) disableLinux() error {
	desktopPath := as.getLinuxDesktopPath()
	// 忽略不存在的错误
	_ = os.Remove(desktopPath)
	return nil
}

func (as *AutoStartService) getLinuxDesktopPath() string {
	home, _ := os.UserHomeDir()
	// 优先使用 XDG_CONFIG_HOME，如果未设置则使用 ~/.config
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "autostart", "codeswitch.desktop")
}
