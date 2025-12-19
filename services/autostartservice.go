package services

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type AutoStartService struct {
	homeDir string // 缓存的用户家目录（已校验）
	homeErr error  // 家目录获取错误
}

func NewAutoStartService() *AutoStartService {
	home, err := getUserHomeDir()
	return &AutoStartService{
		homeDir: home,
		homeErr: err,
	}
}

// requireHome 校验家目录是否可用（仅用于 macOS/Linux）
func (as *AutoStartService) requireHome() error {
	if as.homeErr != nil {
		return fmt.Errorf("无法获取用户家目录: %w", as.homeErr)
	}
	if as.homeDir == "" || as.homeDir == "." || !filepath.IsAbs(as.homeDir) {
		return fmt.Errorf("无法获取用户家目录: homeDir 未初始化或无效")
	}
	return nil
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
const (
	windowsRunKey             = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	windowsStartupApprovedKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	windowsAutoStartValue     = "CodeSwitch"
)

func windowsRegExe() string {
	if windir := os.Getenv("WINDIR"); windir != "" {
		candidate := filepath.Join(windir, "System32", "reg.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "reg.exe"
}

func (as *AutoStartService) isEnabledWindows() (bool, error) {
	regExe := windowsRegExe()

	// 1. 检查 Run 键是否存在
	cmd := hideWindowCmd(regExe, "query", windowsRunKey, "/v", windowsAutoStartValue)
	out, err := cmd.CombinedOutput()
	if err != nil {
		lowerOut := strings.ToLower(string(out))
		if strings.Contains(lowerOut, "unable to find") ||
			strings.Contains(lowerOut, "无法找到") ||
			strings.Contains(lowerOut, "找不到") {
			return false, nil
		}
		return false, fmt.Errorf("查询 Windows 自启动注册表失败: %w, 输出: %s",
			err, strings.TrimSpace(string(out)))
	}

	// 2. 验证路径是否匹配当前可执行文件
	if exePath, exeErr := os.Executable(); exeErr == nil {
		if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
			exePath = resolved
		}
		exePath = strings.TrimPrefix(exePath, `\\?\`)
		if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(exePath)) {
			return false, nil
		}
	}

	// 3. 检查 StartupApproved 是否禁用了该项
	// Windows 10/11 在任务管理器禁用启动项时会在此处写入禁用标记
	approvedCmd := hideWindowCmd(regExe, "query", windowsStartupApprovedKey, "/v", windowsAutoStartValue)
	approvedOut, err := approvedCmd.CombinedOutput()
	if err == nil {
		// 解析 REG_BINARY 输出，格式类似: "CodeSwitch    REG_BINARY    030000..."
		// 第一个字节: 02/06=启用, 03=禁用
		outStr := string(approvedOut)
		if idx := strings.Index(strings.ToUpper(outStr), "REG_BINARY"); idx != -1 {
			hexPart := strings.TrimSpace(outStr[idx+len("REG_BINARY"):])
			// 取第一个空格前的十六进制字符串
			if spaceIdx := strings.IndexAny(hexPart, " \t\r\n"); spaceIdx != -1 {
				hexPart = hexPart[:spaceIdx]
			}
			// 检查第一个字节是否为 03（禁用）
			if len(hexPart) >= 2 && strings.ToLower(hexPart[:2]) == "03" {
				return false, nil // 被系统禁用
			}
		}
	}
	// StartupApproved 不存在或解析失败时，视为启用（向后兼容）

	return true, nil
}

func (as *AutoStartService) enableWindows() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	exePath = strings.TrimPrefix(exePath, `\\?\`)

	quotedPath := fmt.Sprintf(`"%s"`, exePath)
	regExe := windowsRegExe()
	cmd := hideWindowCmd(regExe, "add", windowsRunKey, "/v", windowsAutoStartValue,
		"/t", "REG_SZ", "/d", quotedPath, "/f")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add registry key: %w, output: %s",
			err, strings.TrimSpace(string(out)))
	}

	// Windows 10/11: clear StartupApproved disabled state
	_ = hideWindowCmd(regExe, "delete", windowsStartupApprovedKey, "/v", windowsAutoStartValue, "/f").Run()
	return nil
}

func (as *AutoStartService) disableWindows() error {
	regExe := windowsRegExe()
	cmd := hideWindowCmd(regExe, "delete", windowsRunKey, "/v", windowsAutoStartValue, "/f")
	_ = cmd.Run()
	return nil
}

// macOS 实现
func (as *AutoStartService) isEnabledDarwin() (bool, error) {
	if err := as.requireHome(); err != nil {
		return false, err
	}

	plistPath := as.getDarwinPlistPath()
	_, err := os.Stat(plistPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (as *AutoStartService) enableDarwin() error {
	if err := as.requireHome(); err != nil {
		return err
	}

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
	if err := as.requireHome(); err != nil {
		return err
	}

	plistPath := as.getDarwinPlistPath()
	// 忽略不存在的错误
	_ = os.Remove(plistPath)
	return nil
}

func (as *AutoStartService) getDarwinPlistPath() string {
	return filepath.Join(as.homeDir, "Library", "LaunchAgents", "com.codeswitch.app.plist")
}

// Linux 实现 (使用 .desktop 文件)
func (as *AutoStartService) isEnabledLinux() (bool, error) {
	desktopPath, err := as.getLinuxDesktopPath()
	if err != nil {
		return false, err
	}

	_, err = os.Stat(desktopPath)
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

	desktopPath, err := as.getLinuxDesktopPath()
	if err != nil {
		return err
	}

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
	desktopPath, err := as.getLinuxDesktopPath()
	if err != nil {
		return err
	}

	// 忽略不存在的错误
	_ = os.Remove(desktopPath)
	return nil
}

func (as *AutoStartService) getLinuxDesktopPath() (string, error) {
	// 优先使用 XDG_CONFIG_HOME，如果已设置且为绝对路径则直接使用
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome != "" && !filepath.IsAbs(configHome) {
		configHome = "" // 防止相对路径导致写入工作目录
	}

	// XDG 未设置或无效，回退到 ~/.config
	if configHome == "" {
		if err := as.requireHome(); err != nil {
			return "", err
		}
		configHome = filepath.Join(as.homeDir, ".config")
	}

	return filepath.Join(configHome, "autostart", "codeswitch.desktop"), nil
}
