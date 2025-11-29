package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-version"
)

// UpdateInfo 更新信息
type UpdateInfo struct {
	Available    bool   `json:"available"`
	Version      string `json:"version"`
	DownloadURL  string `json:"download_url"`
	ReleaseNotes string `json:"release_notes"`
	FileSize     int64  `json:"file_size"`
	SHA256       string `json:"sha256"`
}

// UpdateState 更新状态
type UpdateState struct {
	LastCheckTime       time.Time `json:"last_check_time"`
	LastCheckSuccess    bool      `json:"last_check_success"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LatestKnownVersion  string    `json:"latest_known_version"`
	DownloadProgress    float64   `json:"download_progress"`
	UpdateReady         bool      `json:"update_ready"`
	AutoCheckEnabled    bool      `json:"auto_check_enabled"` // 新增：持久化自动检查开关
}

// UpdateService 更新服务
type UpdateService struct {
	currentVersion   string
	latestVersion    string
	downloadURL      string
	updateFilePath   string
	autoCheckEnabled bool
	downloadProgress float64
	dailyCheckTimer  *time.Timer
	lastCheckTime    time.Time
	checkFailures    int
	updateReady      bool
	isPortable       bool // 是否为便携版
	mu               sync.Mutex
	stateFile        string
	updateDir        string
}

// GitHubRelease GitHub Release 结构
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// NewUpdateService 创建更新服务
func NewUpdateService(currentVersion string) *UpdateService {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	updateDir := filepath.Join(home, ".code-switch", "updates")
	stateFile := filepath.Join(home, ".code-switch", "update-state.json")

	us := &UpdateService{
		currentVersion:   currentVersion,
		autoCheckEnabled: true, // 默认开启自动检查
		isPortable:       detectPortableMode(),
		updateDir:        updateDir,
		stateFile:        stateFile,
	}

	// 创建更新目录
	_ = os.MkdirAll(updateDir, 0o755)

	// 加载状态（如果文件不存在，会保持默认值 true）
	_ = us.LoadState()

	log.Printf("[UpdateService] 运行模式: %s", func() string {
		if us.isPortable {
			return "便携版"
		}
		return "安装版"
	}())

	return us
}

// detectPortableMode 检测是否为便携版
func detectPortableMode() bool {
	if runtime.GOOS != "windows" {
		return false // 非 Windows 默认不是便携版
	}

	exe, err := os.Executable()
	if err != nil {
		return false
	}

	exeDir := filepath.Dir(exe)

	// 检测是否在 Program Files 或 AppData 目录（安装版特征）
	lowerDir := strings.ToLower(exeDir)
	if strings.Contains(lowerDir, "program files") ||
		strings.Contains(lowerDir, "appdata") {
		return false
	}

	// 否则认为是便携版
	return true
}

// CheckUpdate 检查更新（带网络容错）
func (us *UpdateService) CheckUpdate() (*UpdateInfo, error) {
	log.Printf("[UpdateService] 开始检查更新，当前版本: %s", us.currentVersion)

	client := &http.Client{
		Timeout: 15 * time.Second, // 增加超时时间从10秒到15秒
	}

	releaseURL := "https://api.github.com/repos/Rogers-F/code-switch-R/releases/latest"

	req, err := http.NewRequest("GET", releaseURL, nil)
	if err != nil {
		log.Printf("[UpdateService] ❌ 创建请求失败: %v", err)
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CodeSwitch/"+us.currentVersion)

	log.Printf("[UpdateService] 请求 GitHub API: %s", releaseURL)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[UpdateService] ❌ GitHub API 不可达: %v", err)
		return nil, fmt.Errorf("GitHub API 不可达: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("[UpdateService] ❌ GitHub API 返回错误状态码: %d", resp.StatusCode)
		return nil, fmt.Errorf("GitHub API 返回错误状态码: %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		log.Printf("[UpdateService] ❌ 解析响应失败: %v", err)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	log.Printf("[UpdateService] 最新版本: %s", release.TagName)

	// 比较版本号
	needUpdate, err := us.compareVersions(us.currentVersion, release.TagName)
	if err != nil {
		log.Printf("[UpdateService] ❌ 版本比较失败: %v (current=%s, latest=%s)", err, us.currentVersion, release.TagName)
		return nil, fmt.Errorf("版本比较失败: %w", err)
	}

	if needUpdate {
		log.Printf("[UpdateService] ✅ 发现新版本: %s → %s", us.currentVersion, release.TagName)
	} else {
		log.Printf("[UpdateService] ✅ 已是最新版本: %s", us.currentVersion)
	}

	// 查找当前平台的下载链接
	downloadURL := us.findPlatformAsset(release.Assets)
	if downloadURL == "" {
		log.Printf("[UpdateService] ❌ 未找到适用于 %s 的安装包", runtime.GOOS)
		return nil, fmt.Errorf("未找到适用于 %s 的安装包", runtime.GOOS)
	}

	log.Printf("[UpdateService] 下载链接: %s", downloadURL)

	us.mu.Lock()
	us.latestVersion = release.TagName
	us.downloadURL = downloadURL
	us.mu.Unlock()

	return &UpdateInfo{
		Available:    needUpdate,
		Version:      release.TagName,
		DownloadURL:  downloadURL,
		ReleaseNotes: release.Body,
	}, nil
}

// compareVersions 比较版本号
func (us *UpdateService) compareVersions(current, latest string) (bool, error) {
	currentVer, err := version.NewVersion(current)
	if err != nil {
		return false, fmt.Errorf("解析当前版本失败: %w", err)
	}

	latestVer, err := version.NewVersion(latest)
	if err != nil {
		return false, fmt.Errorf("解析最新版本失败: %w", err)
	}

	return latestVer.GreaterThan(currentVer), nil
}

// findPlatformAsset 查找当前平台的下载链接
func (us *UpdateService) findPlatformAsset(assets []struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}) string {
	var targetName string
	switch runtime.GOOS {
	case "windows":
		if us.isPortable {
			// 便携版：查找 CodeSwitch.exe（不带 -installer）
			targetName = "CodeSwitch.exe"
		} else {
			// 安装版：查找 CodeSwitch-amd64-installer.exe
			targetName = "CodeSwitch-amd64-installer.exe"
		}
	case "darwin":
		if runtime.GOARCH == "arm64" {
			targetName = "codeswitch-macos-arm64.zip"
		} else {
			targetName = "codeswitch-macos-amd64.zip"
		}
	case "linux":
		targetName = "CodeSwitch.AppImage"
	default:
		return ""
	}

	// 精确匹配文件名
	for _, asset := range assets {
		if asset.Name == targetName {
			log.Printf("[UpdateService] 找到更新文件: %s (模式: %s)", targetName, func() string {
				if us.isPortable {
					return "便携版"
				}
				return "安装版"
			}())
			return asset.BrowserDownloadURL
		}
	}

	log.Printf("[UpdateService] 未找到适配文件 %s", targetName)
	return ""
}

// DownloadUpdate 下载更新文件
func (us *UpdateService) DownloadUpdate(progressCallback func(float64)) error {
	us.mu.Lock()
	url := us.downloadURL
	us.mu.Unlock()

	if url == "" {
		return fmt.Errorf("下载链接为空，请先检查更新")
	}

	// 创建 HTTP 请求
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("下载失败，HTTP 状态码: %d", resp.StatusCode)
	}

	// 生成文件名
	filename := filepath.Base(url)
	filePath := filepath.Join(us.updateDir, filename)

	// 创建文件
	out, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer out.Close()

	// 下载并显示进度
	totalSize := resp.ContentLength
	downloaded := int64(0)

	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := out.Write(buf[:n])
			if writeErr != nil {
				return fmt.Errorf("写入文件失败: %w", writeErr)
			}

			downloaded += int64(n)

			if totalSize > 0 && progressCallback != nil {
				progress := float64(downloaded) / float64(totalSize) * 100
				us.mu.Lock()
				us.downloadProgress = progress
				us.mu.Unlock()
				progressCallback(progress)
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取数据失败: %w", err)
		}
	}

	us.mu.Lock()
	us.updateFilePath = filePath
	us.downloadProgress = 100
	us.mu.Unlock()

	return nil
}

// PrepareUpdate 准备更新
func (us *UpdateService) PrepareUpdate() error {
	us.mu.Lock()
	defer us.mu.Unlock()

	if us.updateFilePath == "" {
		return fmt.Errorf("更新文件路径为空")
	}

	// 写入待更新标记
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	metadata := map[string]interface{}{
		"version":       us.latestVersion,
		"download_path": us.updateFilePath,
		"download_time": time.Now().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}

	if err := os.WriteFile(pendingFile, data, 0o644); err != nil {
		return fmt.Errorf("写入标记文件失败: %w", err)
	}

	us.updateReady = true
	us.SaveState()

	return nil
}

// ApplyUpdate 应用更新（启动时调用）
func (us *UpdateService) ApplyUpdate() error {
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")

	// 检查是否有待更新
	if _, err := os.Stat(pendingFile); os.IsNotExist(err) {
		return nil // 没有待更新
	}

	// 读取元数据
	data, err := os.ReadFile(pendingFile)
	if err != nil {
		return fmt.Errorf("读取标记文件失败: %w", err)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return fmt.Errorf("解析元数据失败: %w", err)
	}

	downloadPath, ok := metadata["download_path"].(string)
	if !ok || downloadPath == "" {
		return fmt.Errorf("元数据中缺少下载路径")
	}

	// 根据平台执行安装
	var installErr error
	switch runtime.GOOS {
	case "windows":
		installErr = us.applyUpdateWindows(downloadPath)
	case "darwin":
		installErr = us.applyUpdateDarwin(downloadPath)
	case "linux":
		installErr = us.applyUpdateLinux(downloadPath)
	default:
		installErr = fmt.Errorf("不支持的平台: %s", runtime.GOOS)
	}

	if installErr != nil {
		return installErr
	}

	// 清理标记文件
	_ = os.Remove(pendingFile)

	return nil
}

// applyUpdateWindows Windows 平台更新
func (us *UpdateService) applyUpdateWindows(updatePath string) error {
	if us.isPortable {
		// 便携版：替换当前可执行文件
		return us.applyPortableUpdate(updatePath)
	}

	// 安装版：启动安装器（静默模式，请求管理员权限）
	// 使用 PowerShell Start-Process -Verb RunAs 来请求 UAC 提升权限
	psScript := fmt.Sprintf(`Start-Process -FilePath '%s' -ArgumentList '/SILENT' -Verb RunAs`, updatePath)
	cmd := exec.Command("powershell", "-Command", psScript)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动安装器失败: %w", err)
	}

	// **关键修复**：删除 pending 标记文件，防止安装失败时重启循环
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	_ = os.Remove(pendingFile)

	// 退出当前应用
	os.Exit(0)
	return nil
}

// applyPortableUpdate 便携版更新逻辑
func (us *UpdateService) applyPortableUpdate(newExePath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}

	// 解析符号链接（如果有）
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("解析符号链接失败: %w", err)
	}

	log.Printf("[UpdateService] 便携版更新: %s -> %s", newExePath, currentExe)

	// 备份旧文件
	backupPath := currentExe + ".old"
	if err := os.Rename(currentExe, backupPath); err != nil {
		return fmt.Errorf("备份旧文件失败: %w", err)
	}

	// 复制新文件
	if err := copyUpdateFile(newExePath, currentExe); err != nil {
		// 恢复备份
		_ = os.Rename(backupPath, currentExe)
		return fmt.Errorf("复制新文件失败: %w", err)
	}

	log.Println("[UpdateService] 便携版更新成功，准备重启...")

	// 删除备份（延迟删除，避免文件占用）
	go func() {
		time.Sleep(2 * time.Second)
		_ = os.Remove(backupPath)
	}()

	// **关键修复**：删除 pending 标记文件，防止重启后再次触发更新
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	_ = os.Remove(pendingFile)

	// 重启应用
	cmd := exec.Command(currentExe)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("重启应用失败: %w", err)
	}

	// 退出当前进程
	os.Exit(0)
	return nil
}

// applyUpdateDarwin macOS 平台更新
func (us *UpdateService) applyUpdateDarwin(zipPath string) error {
	// TODO: 实现 macOS 更新逻辑
	// 1. 解压 zip 文件
	// 2. 替换 /Applications/CodeSwitch.app
	// 3. 重启应用
	log.Println("[UpdateService] macOS 更新功能待实现")
	return nil
}

// applyUpdateLinux Linux 平台更新
func (us *UpdateService) applyUpdateLinux(appImagePath string) error {
	// 替换当前可执行文件
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}

	// 备份旧文件
	backupPath := currentExe + ".bak"
	_ = os.Rename(currentExe, backupPath)

	// 复制新文件
	if err := copyUpdateFile(appImagePath, currentExe); err != nil {
		// 恢复备份
		_ = os.Rename(backupPath, currentExe)
		return fmt.Errorf("复制新文件失败: %w", err)
	}

	// 设置执行权限
	if err := os.Chmod(currentExe, 0o755); err != nil {
		return fmt.Errorf("设置执行权限失败: %w", err)
	}

	// 删除备份
	_ = os.Remove(backupPath)

	return nil
}

// RestartApp 重启应用
func (us *UpdateService) RestartApp() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command(executable)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("启动新进程失败: %w", err)
		}
		os.Exit(0)

	case "darwin":
		cmd := exec.Command("open", "-n", executable)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("启动新进程失败: %w", err)
		}
		os.Exit(0)

	case "linux":
		cmd := exec.Command(executable)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("启动新进程失败: %w", err)
		}
		os.Exit(0)
	}

	return nil
}

// StartDailyCheck 启动每日8点定时检查
func (us *UpdateService) StartDailyCheck() {
	us.stopDailyCheck()

	duration := us.calculateNextCheckDuration()
	us.dailyCheckTimer = time.AfterFunc(duration, func() {
		us.performDailyCheck()
		us.StartDailyCheck() // 重新调度下次检查
	})

	log.Printf("[UpdateService] 定时检查已启动，下次检查时间: %s", time.Now().Add(duration).Format("2006-01-02 15:04:05"))
}

// stopDailyCheck 停止定时检查
func (us *UpdateService) stopDailyCheck() {
	if us.dailyCheckTimer != nil {
		us.dailyCheckTimer.Stop()
		us.dailyCheckTimer = nil
	}
}

// calculateNextCheckDuration 计算距离下一个8点的时长
func (us *UpdateService) calculateNextCheckDuration() time.Duration {
	now := time.Now()

	// 今天早上8点
	next := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, now.Location())

	// 如果已经过了今天8点，调整到明天8点
	if now.After(next) {
		next = next.Add(24 * time.Hour)
	}

	return next.Sub(now)
}

// performDailyCheck 执行每日检查（带重试）
func (us *UpdateService) performDailyCheck() {
	log.Println("[UpdateService] 开始每日定时检查更新...")

	var updateInfo *UpdateInfo
	var err error

	// 重试机制：最多3次，间隔5分钟
	for i := 0; i < 3; i++ {
		updateInfo, err = us.CheckUpdate()

		if err == nil {
			// 检查成功
			us.mu.Lock()
			us.lastCheckTime = time.Now()
			us.checkFailures = 0
			us.mu.Unlock()
			us.SaveState()

			if updateInfo.Available {
				log.Printf("[UpdateService] 发现新版本 %s，开始下载...", updateInfo.Version)
				go us.autoDownload()
			} else {
				log.Println("[UpdateService] 已是最新版本")
			}
			return
		}

		// 网络错误，记录日志
		log.Printf("[UpdateService] 检查更新失败（第%d次）: %v", i+1, err)

		us.mu.Lock()
		us.checkFailures++
		us.mu.Unlock()

		if i < 2 { // 不是最后一次，等待后重试
			time.Sleep(5 * time.Minute)
		}
	}

	// 3次都失败，静默放弃
	us.SaveState()
	log.Println("[UpdateService] 检查更新失败，将在明天8点重试")
}

// autoDownload 自动下载更新（静默失败）
func (us *UpdateService) autoDownload() {
	err := us.DownloadUpdate(func(progress float64) {
		log.Printf("[UpdateService] 下载进度: %.2f%%", progress)
	})

	if err != nil {
		log.Printf("[UpdateService] 自动下载失败: %v", err)
		return
	}

	// 下载成功，准备更新
	if err := us.PrepareUpdate(); err != nil {
		log.Printf("[UpdateService] 准备更新失败: %v", err)
		return
	}

	log.Println("[UpdateService] 更新已下载完成，等待用户重启应用")
}

// CheckUpdateAsync 异步检查更新
func (us *UpdateService) CheckUpdateAsync() {
	go func() {
		updateInfo, err := us.CheckUpdate()
		if err != nil {
			log.Printf("[UpdateService] 检查更新失败: %v", err)
			us.mu.Lock()
			us.checkFailures++
			us.mu.Unlock()
			us.SaveState()
			return
		}

		us.mu.Lock()
		us.lastCheckTime = time.Now()
		us.checkFailures = 0
		us.mu.Unlock()
		us.SaveState()

		if updateInfo.Available {
			log.Printf("[UpdateService] 发现新版本 %s", updateInfo.Version)
			go us.autoDownload()
		}
	}()
}

// GetUpdateState 获取更新状态
func (us *UpdateService) GetUpdateState() *UpdateState {
	us.mu.Lock()
	defer us.mu.Unlock()

	return &UpdateState{
		LastCheckTime:       us.lastCheckTime,
		LastCheckSuccess:    us.checkFailures == 0,
		ConsecutiveFailures: us.checkFailures,
		LatestKnownVersion:  us.latestVersion,
		DownloadProgress:    us.downloadProgress,
		UpdateReady:         us.updateReady,
		AutoCheckEnabled:    us.autoCheckEnabled, // 返回自动检查状态
	}
}

// IsAutoCheckEnabled 是否启用自动检查
func (us *UpdateService) IsAutoCheckEnabled() bool {
	us.mu.Lock()
	defer us.mu.Unlock()
	return us.autoCheckEnabled
}

// SetAutoCheckEnabled 设置是否启用自动检查
func (us *UpdateService) SetAutoCheckEnabled(enabled bool) {
	us.mu.Lock()
	us.autoCheckEnabled = enabled
	us.mu.Unlock()

	if enabled {
		us.StartDailyCheck()
	} else {
		us.stopDailyCheck()
	}

	us.SaveState()
}

// SaveState 保存状态
func (us *UpdateService) SaveState() error {
	us.mu.Lock()
	defer us.mu.Unlock()

	state := UpdateState{
		LastCheckTime:       us.lastCheckTime,
		LastCheckSuccess:    us.checkFailures == 0,
		ConsecutiveFailures: us.checkFailures,
		LatestKnownVersion:  us.latestVersion,
		DownloadProgress:    us.downloadProgress,
		UpdateReady:         us.updateReady,
		AutoCheckEnabled:    us.autoCheckEnabled, // 持久化自动检查开关
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}

	dir := filepath.Dir(us.stateFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	return os.WriteFile(us.stateFile, data, 0o644)
}

// LoadState 加载状态
func (us *UpdateService) LoadState() error {
	data, err := os.ReadFile(us.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，保存默认配置
			_ = us.SaveState()
			return nil
		}
		return fmt.Errorf("读取状态文件失败: %w", err)
	}

	var state UpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("解析状态失败: %w", err)
	}

	us.mu.Lock()
	us.lastCheckTime = state.LastCheckTime
	us.checkFailures = state.ConsecutiveFailures
	us.latestVersion = state.LatestKnownVersion
	us.downloadProgress = state.DownloadProgress
	us.updateReady = state.UpdateReady

	// 检查文件中是否包含 auto_check_enabled 字段
	// 如果包含，使用文件中的值；否则保持默认值 true（兼容老版本）
	dataStr := string(data)
	if strings.Contains(dataStr, "\"auto_check_enabled\"") {
		// 文件中包含 auto_check_enabled 字段，使用文件中的值
		us.autoCheckEnabled = state.AutoCheckEnabled
	}
	// 否则保持初始化时设置的默认值 true
	us.mu.Unlock()

	return nil
}

// copyUpdateFile 复制更新文件
func copyUpdateFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

// calculateSHA256 计算文件 SHA256
func calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
