package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-version"
)

// ErrUACDenied 表示用户取消或拒绝 UAC 提权
var ErrUACDenied = errors.New("ERR_UAC_DENIED")

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
	lockFile         string // 更新锁文件路径

	// 保存最新检查到的更新信息（含 SHA256）
	latestUpdateInfo *UpdateInfo
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

	// P1-2 修复：加载状态时记录错误（不阻止启动，但提供可观测性）
	if err := us.LoadState(); err != nil {
		log.Printf("[UpdateService] ⚠️ 加载状态失败（将使用默认值）: %v", err)
	}

	log.Printf("[UpdateService] 运行模式: %s", func() string {
		if us.isPortable {
			return "便携版"
		}
		return "安装版"
	}())

	return us
}

// detectPortableMode 检测是否为便携版
// 采用写权限检测方式：如果能在 exe 所在目录创建文件，则为便携版
func detectPortableMode() bool {
	if runtime.GOOS != "windows" {
		return false // 非 Windows 默认不是便携版
	}

	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	exePath, _ = filepath.EvalSymlinks(exePath)
	exeDir := filepath.Dir(exePath)

	// 直接检测写权限（比路径匹配更准确）
	// 如果能在 exe 所在目录创建文件，则为便携版
	testFile := filepath.Join(exeDir, fmt.Sprintf(".write-test-%d", os.Getpid()))
	f, err := os.Create(testFile)
	if err != nil {
		// 无写权限，视为安装版（需要 UAC）
		log.Printf("[Update] 检测为安装版: 无法写入 %s", exeDir)
		return false
	}
	f.Close()
	os.Remove(testFile)

	log.Printf("[Update] 检测为便携版: 可写入 %s", exeDir)
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

	// 查找对应的 SHA256 校验文件
	sha256Hash := us.findSHA256ForAsset(release.Assets, downloadURL)
	if sha256Hash != "" {
		log.Printf("[UpdateService] SHA256: %s", sha256Hash)
	}

	updateInfo := &UpdateInfo{
		Available:    needUpdate,
		Version:      release.TagName,
		DownloadURL:  downloadURL,
		ReleaseNotes: release.Body,
		SHA256:       sha256Hash,
	}

	us.mu.Lock()
	us.latestVersion = release.TagName
	us.downloadURL = downloadURL
	us.latestUpdateInfo = updateInfo // 保存更新信息
	us.mu.Unlock()

	return updateInfo, nil
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
		// 统一下载核心 exe（无论便携版还是安装版）
		// 安装版通过 updater.exe 提权替换
		targetName = "CodeSwitch.exe"
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

// findSHA256ForAsset 查找资产对应的 SHA256 哈希
// SHA256 文件格式：<hash>  <filename> 或 <hash> <filename>
func (us *UpdateService) findSHA256ForAsset(assets []struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}, assetURL string) string {
	// 从 URL 提取文件名
	assetName := filepath.Base(assetURL)
	sha256FileName := assetName + ".sha256"

	// 查找 SHA256 文件
	var sha256URL string
	for _, asset := range assets {
		if asset.Name == sha256FileName {
			sha256URL = asset.BrowserDownloadURL
			break
		}
	}

	if sha256URL == "" {
		log.Printf("[UpdateService] 未找到 SHA256 文件: %s", sha256FileName)
		return ""
	}

	// 下载并解析 SHA256 文件
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(sha256URL)
	if err != nil {
		log.Printf("[UpdateService] 下载 SHA256 文件失败: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("[UpdateService] SHA256 文件返回错误状态码: %d", resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[UpdateService] 读取 SHA256 文件失败: %v", err)
		return ""
	}

	// 解析格式：<hash>  <filename> 或 <hash> <filename>
	content := strings.TrimSpace(string(body))
	parts := strings.Fields(content)
	if len(parts) >= 1 {
		log.Printf("[UpdateService] 获取到 SHA256: %s", parts[0])
		return parts[0] // 返回哈希值
	}

	return ""
}

// DownloadUpdate 下载更新文件（支持更新锁、重试、断点续传、SHA256校验）
func (us *UpdateService) DownloadUpdate(progressCallback func(float64)) error {
	log.Printf("[UpdateService] 开始下载更新...")

	// 获取更新锁，防止并发下载
	if err := us.acquireUpdateLock(); err != nil {
		log.Printf("[UpdateService] ❌ 获取更新锁失败: %v", err)
		return err
	}
	defer us.releaseUpdateLock()

	us.mu.Lock()
	url := us.downloadURL
	// P0-3 修复：快照 version 和 SHA256，避免竞态条件
	snapshotVersion := us.latestVersion
	snapshotSHA := ""
	if us.latestUpdateInfo != nil {
		snapshotSHA = us.latestUpdateInfo.SHA256
	}
	// 重置下载状态
	us.updateReady = false
	us.downloadProgress = 0
	us.mu.Unlock()
	us.SaveState()

	if url == "" {
		log.Printf("[UpdateService] ❌ 下载链接为空")
		return fmt.Errorf("下载链接为空，请先检查更新")
	}

	log.Printf("[UpdateService] 下载 URL: %s", url)

	filePath := filepath.Join(us.updateDir, filepath.Base(url))

	// 检查本地是否已有完整文件（断点续传场景：之前下载完成但未安装）
	if snapshotSHA != "" {
		if hash, err := calculateSHA256(filePath); err == nil && strings.EqualFold(hash, snapshotSHA) {
			log.Printf("[UpdateService] 本地已有完整文件，跳过下载")
			us.mu.Lock()
			us.updateFilePath = filePath
			us.downloadProgress = 100
			us.mu.Unlock()
			return us.prepareUpdateInternal(snapshotVersion, snapshotSHA, filePath)
		}
	}

	log.Printf("[UpdateService] 开始下载到: %s", filePath)

	// 三次重试下载
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		log.Printf("[UpdateService] 下载尝试 %d/3...", attempt)
		if err := us.downloadWithResume(url, filePath, progressCallback); err != nil {
			lastErr = err
			log.Printf("[UpdateService] 下载失败（第%d次）: %v", attempt, err)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		_ = os.Remove(filePath) // 清理残留文件
		return fmt.Errorf("下载失败: %w", lastErr)
	}

	// SHA256 校验
	if snapshotSHA != "" {
		if err := us.verifyDownload(filePath, snapshotSHA); err != nil {
			_ = os.Remove(filePath)
			return err
		}
	}

	us.mu.Lock()
	us.updateFilePath = filePath
	us.downloadProgress = 100
	us.mu.Unlock()

	// 下载成功后立即准备更新，使用快照值写入 pending 标记
	if err := us.prepareUpdateInternal(snapshotVersion, snapshotSHA, filePath); err != nil {
		return fmt.Errorf("准备更新失败: %w", err)
	}

	return nil
}

// downloadWithResume 支持断点续传的下载
func (us *UpdateService) downloadWithResume(url, dest string, progressCallback func(float64)) error {
	client := &http.Client{Timeout: 5 * time.Minute}

	var start int64
	var total int64
	if info, err := os.Stat(dest); err == nil {
		start = info.Size()
	}

	// HEAD 请求检查是否支持 Range
	if start > 0 {
		head, err := client.Head(url)
		if head != nil {
			_ = head.Body.Close()
		}

		if err == nil && head != nil && head.StatusCode == http.StatusOK {
			if strings.EqualFold(head.Header.Get("Accept-Ranges"), "bytes") {
				total = head.ContentLength
				log.Printf("[UpdateService] 断点续传: 从 %d 字节继续下载", start)
			} else {
				start = 0
				_ = os.Remove(dest)
			}
		} else {
			start = 0
			_ = os.Remove(dest)
		}
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("下载失败，HTTP 状态码: %d", resp.StatusCode)
	}

	if total == 0 {
		total = resp.ContentLength
		if total > 0 && start > 0 {
			total += start
		}
	}

	var out *os.File
	if start > 0 {
		out, err = os.OpenFile(dest, os.O_WRONLY|os.O_APPEND, 0o644)
	} else {
		out, err = os.Create(dest)
	}
	if err != nil {
		return err
	}
	defer out.Close()

	downloaded := start
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("写入文件失败: %w", writeErr)
			}
			downloaded += int64(n)

			if total > 0 {
				progress := float64(downloaded) / float64(total) * 100
				us.mu.Lock()
				us.downloadProgress = progress
				us.mu.Unlock()
				// 回调是可选的，但进度始终更新
				if progressCallback != nil {
					progressCallback(progress)
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("读取数据失败: %w", readErr)
		}
	}
	return nil
}

// prepareUpdateInternal 内部方法：使用明确的 version/sha256/filePath 写入 pending 标记
// P0-3 修复：避免从共享字段读取，消除竞态条件
func (us *UpdateService) prepareUpdateInternal(version, sha256, filePath string) error {
	log.Printf("[UpdateService] 准备更新 (version=%s)...", version)

	if filePath == "" {
		log.Printf("[UpdateService] ❌ 更新文件路径为空")
		return fmt.Errorf("更新文件路径为空")
	}

	log.Printf("[UpdateService] 更新文件: %s", filePath)

	// 写入待更新标记（包含 SHA256 用于重启后校验）
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	metadata := map[string]interface{}{
		"version":       version,
		"download_path": filePath,
		"download_time": time.Now().Format(time.RFC3339),
	}

	// 持久化 SHA256（关键：重启后 latestUpdateInfo 会丢失）
	if sha256 != "" {
		metadata["sha256"] = sha256
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}

	// P1-1 修复：pending 是重启后的权威来源，使用原子写避免崩溃/断电损坏
	if err := atomicWriteFile(pendingFile, data, 0o644); err != nil {
		log.Printf("[UpdateService] ❌ 写入 pending 标记失败: %v", err)
		return fmt.Errorf("写入标记文件失败: %w", err)
	}

	log.Printf("[UpdateService] ✅ 已写入 pending 标记: %s", pendingFile)

	us.mu.Lock()
	us.updateReady = true
	us.mu.Unlock()

	us.SaveState()

	log.Printf("[UpdateService] ✅ 更新已准备就绪，等待重启应用")

	return nil
}

// PrepareUpdate 准备更新（公开方法，保留兼容性）
// Deprecated: 建议使用 DownloadUpdate，它会自动调用内部 prepare 逻辑
func (us *UpdateService) PrepareUpdate() error {
	us.mu.Lock()
	version := us.latestVersion
	sha256 := ""
	if us.latestUpdateInfo != nil {
		sha256 = us.latestUpdateInfo.SHA256
	}
	filePath := us.updateFilePath
	us.mu.Unlock()

	return us.prepareUpdateInternal(version, sha256, filePath)
}

// ApplyUpdate 应用更新（启动时调用）
// 添加更新锁防止并发，SHA256 校验防止损坏文件
func (us *UpdateService) ApplyUpdate() error {
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")

	// 检查是否有待更新
	if _, err := os.Stat(pendingFile); os.IsNotExist(err) {
		return nil // 没有待更新
	}

	// 获取更新锁
	if err := us.acquireUpdateLock(); err != nil {
		log.Printf("[UpdateService] 获取更新锁失败，跳过更新: %v", err)
		return nil // 另一个更新正在进行，静默跳过
	}
	defer us.releaseUpdateLock()

	// 读取元数据
	data, err := os.ReadFile(pendingFile)
	if err != nil {
		us.clearPendingState()
		return fmt.Errorf("读取标记文件失败: %w", err)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		us.clearPendingState()
		return fmt.Errorf("解析元数据失败: %w", err)
	}

	downloadPath, ok := metadata["download_path"].(string)
	if !ok || downloadPath == "" {
		us.clearPendingState()
		return fmt.Errorf("元数据中缺少下载路径")
	}

	// 检查下载文件是否存在
	if _, err := os.Stat(downloadPath); os.IsNotExist(err) {
		us.clearPendingState()
		return fmt.Errorf("更新文件不存在: %s", downloadPath)
	}

	// 从元数据恢复 version 和 SHA256（供 downloadAndVerify 等方法使用）
	var expectedHash string
	us.mu.Lock()
	if version, ok := metadata["version"].(string); ok && version != "" {
		us.latestVersion = version
		log.Printf("[UpdateService] 从元数据恢复 version: %s", version)
	}
	if sha256Hash, ok := metadata["sha256"].(string); ok && sha256Hash != "" {
		expectedHash = sha256Hash
		us.latestUpdateInfo = &UpdateInfo{
			SHA256: sha256Hash,
		}
		log.Printf("[UpdateService] 从元数据恢复 SHA256: %s", sha256Hash)
	}
	us.mu.Unlock()

	// SHA256 校验（如果有）
	if expectedHash != "" {
		if err := us.verifyDownload(downloadPath, expectedHash); err != nil {
			log.Printf("[UpdateService] SHA256 校验失败: %v", err)
			us.clearPendingState()
			_ = os.Remove(downloadPath) // 删除损坏的文件
			return fmt.Errorf("更新文件校验失败: %w", err)
		}
		log.Println("[UpdateService] SHA256 校验通过")
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
		// UAC 取消：不清理 pending，允许用户重试
		if errors.Is(installErr, ErrUACDenied) {
			log.Printf("[UpdateService] 用户取消 UAC，保留待更新状态: %v", installErr)
			return installErr
		}
		// 其他安装失败：清理状态但保留下载文件（可能需要重试）
		us.clearPendingState()
		return installErr
	}

	// 清理标记文件（成功情况下由平台特定函数清理）
	return nil
}

// clearPendingState 统一清理更新状态（成功或失败后调用）
func (us *UpdateService) clearPendingState() {
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	_ = os.Remove(pendingFile)

	us.mu.Lock()
	us.updateReady = false
	us.downloadProgress = 0
	us.mu.Unlock()

	us.SaveState()
	log.Println("[UpdateService] 已清理更新状态")
}

// applyUpdateWindows Windows 平台更新
func (us *UpdateService) applyUpdateWindows(updatePath string) error {
	if us.isPortable {
		// 便携版：替换当前可执行文件
		return us.applyPortableUpdate(updatePath)
	}

	// 安装版：使用 updater.exe 辅助程序静默更新
	return us.applyInstalledUpdate(updatePath)
}

// applyPortableUpdate 便携版更新逻辑
// 使用 PowerShell 脚本等待当前进程退出后替换文件，解决 Windows 文件锁定问题
// P1-5 修复：pending 由脚本在成功时清理，lock 总是清理
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

	// 构建 PowerShell 脚本：等待进程退出 → 替换文件 → 启动新版本
	backupPath := currentExe + ".old"
	pid := os.Getpid()
	// P1-5: 传递 pending 和 lock 文件路径给脚本
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	lockFile := filepath.Join(us.updateDir, "update.lock")

	// PowerShell 脚本内容
	psScript := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$pid = %d
$currentExe = '%s'
$newExe = '%s'
$backupPath = '%s'
$pendingFile = '%s'
$lockFile = '%s'

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$logFile = Join-Path $scriptDir "update-portable.log"
function Log($msg) {
  $ts = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
  Add-Content -Path $logFile -Value "[update-portable] $ts $msg"
}

# P1-5: 总是清理 lock 文件（无论成功失败）
function Cleanup-Lock {
  if (Test-Path $lockFile) {
    Remove-Item $lockFile -Force -ErrorAction SilentlyContinue
    Log "cleanup lock"
  }
}

Log "start update: pid=$pid current=$currentExe new=$newExe backup=$backupPath"
if (-not (Test-Path $newExe)) {
  Log "error: new file not exists: $newExe"
  Cleanup-Lock
  throw "新文件不存在: $newExe"
}

# 等待主进程退出（最多 30 秒）
$proc = Get-Process -Id $pid -ErrorAction SilentlyContinue
if ($proc) {
    Log "waiting for process $pid to exit..."
    $proc.WaitForExit(30000) | Out-Null
}

# 短暂延迟确保文件释放
Start-Sleep -Milliseconds 500

try {
  # 备份旧文件
  if (Test-Path $currentExe) {
      Move-Item -Path $currentExe -Destination $backupPath -Force
      Log "backup ok: $backupPath"
  }

  # 复制新文件
  Copy-Item -Path $newExe -Destination $currentExe -Force
  Log "replace ok: $currentExe"

  # 启动新版本
  Start-Process -FilePath $currentExe | Out-Null
  Log "relaunch ok"

  # P1-5: 成功后清理 pending
  if (Test-Path $pendingFile) {
    Remove-Item $pendingFile -Force -ErrorAction SilentlyContinue
    Log "cleanup pending"
  }

  # 清理备份
  Start-Sleep -Seconds 2
  if (Test-Path $backupPath) {
      Remove-Item -Path $backupPath -Force -ErrorAction SilentlyContinue
      Log "cleanup backup"
  }

  Log "update completed"
  Cleanup-Lock
  exit 0
} catch {
  Log ("update failed: " + $_.Exception.Message)
  # 回滚：把旧 exe 放回去并尝试启动
  if (Test-Path $backupPath) {
    try {
      Move-Item -Path $backupPath -Destination $currentExe -Force
      Log "rollback ok"
      Start-Process -FilePath $currentExe | Out-Null
      Log "rollback relaunch ok"
    } catch {
      Log ("rollback failed: " + $_.Exception.Message)
    }
  }
  Cleanup-Lock
  exit 1
}
`,
		pid,
		strings.ReplaceAll(currentExe, `'`, `''`),
		strings.ReplaceAll(newExePath, `'`, `''`),
		strings.ReplaceAll(backupPath, `'`, `''`),
		strings.ReplaceAll(pendingFile, `'`, `''`),
		strings.ReplaceAll(lockFile, `'`, `''`),
	)

	// 将脚本写入临时文件
	scriptPath := filepath.Join(us.updateDir, "update-portable.ps1")
	if err := os.WriteFile(scriptPath, []byte(psScript), 0o644); err != nil {
		return fmt.Errorf("写入更新脚本失败: %w", err)
	}

	log.Printf("[UpdateService] 已创建更新脚本: %s", scriptPath)

	// 启动 PowerShell 执行脚本（-WindowStyle Hidden 隐藏窗口）
	cmd := exec.Command("powershell.exe",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-File", scriptPath,
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}

	// P1-5: 不再在此处调用 clearPendingState()，由脚本负责

	log.Printf("[UpdateService] 更新脚本已启动 (PID=%d)，准备退出主程序...", cmd.Process.Pid)

	// 释放更新锁（脚本也会清理，这里提前释放避免文件句柄问题）
	us.releaseUpdateLock()

	// 退出当前进程，让 PowerShell 脚本完成替换
	os.Exit(0)
	return nil
}

// applyUpdateDarwin macOS 平台更新
func (us *UpdateService) applyUpdateDarwin(zipPath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}

	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("解析符号链接失败: %w", err)
	}

	pid := os.Getpid()

	// 定位当前运行的 .app 包路径（支持安装版和便携版）
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
		return fmt.Errorf("无法定位当前应用包(.app)路径: %s", currentExe)
	}
	targetAppPath := appPath
	parentDir := filepath.Dir(targetAppPath)

	log.Printf("[UpdateService] macOS 更新目标应用: %s", targetAppPath)

	// P1-5: 获取 pending 和 lock 文件路径
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	lockFile := filepath.Join(us.updateDir, "update.lock")

	// 创建临时解压目录
	if err := os.MkdirAll(us.updateDir, 0o755); err != nil {
		return fmt.Errorf("创建更新目录失败: %w", err)
	}
	extractDir, err := os.MkdirTemp(us.updateDir, "darwin-update-*")
	if err != nil {
		return fmt.Errorf("创建临时解压目录失败: %w", err)
	}

	log.Printf("[UpdateService] 解压更新包: %s -> %s", zipPath, extractDir)
	unzipCmd := exec.Command("unzip", "-q", "-o", zipPath, "-d", extractDir)
	unzipOut, err := unzipCmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(extractDir)
		return fmt.Errorf("解压更新包失败: %w, 输出: %s", err, strings.TrimSpace(string(unzipOut)))
	}

	// 查找新 .app 包：优先同名、浅层优先、必要时递归
	preferredName := filepath.Base(targetAppPath) // e.g. CodeSwitch.app
	newAppPath, err := findNewAppBundle(extractDir, preferredName)
	if err != nil {
		_ = os.RemoveAll(extractDir)
		return err
	}
	log.Printf("[UpdateService] 已找到新应用包: %s", newAppPath)

	// 检查目标目录可写（/Applications 可能无权限）
	testFile := filepath.Join(parentDir, fmt.Sprintf(".updateservice-write-test-%d", pid))
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		log.Printf("[UpdateService] 目标目录不可写: %s, err=%v", parentDir, err)
		_ = os.RemoveAll(extractDir)
		return fmt.Errorf("目标目录不可写，无法自动更新到 %s，请手动安装或使用管理员权限", parentDir)
	}
	_ = os.Remove(testFile)

	// 构建 bash 脚本：等待进程退出 → 备份/替换 .app → 清除隔离属性 → 重启
	scriptPath := filepath.Join(us.updateDir, fmt.Sprintf("update-darwin-%d.sh", time.Now().UnixNano()))
	logFile := filepath.Join(us.updateDir, "update-darwin.log")
	backupAppPath := targetAppPath + ".old"

	bashScript := `#!/bin/bash
set -euo pipefail

PID="$1"
TARGET_APP="$2"
NEW_APP="$3"
BACKUP_APP="$4"
EXTRACT_DIR="$5"
LOG_FILE="$6"
PENDING_FILE="$7"
LOCK_FILE="$8"

log() {
  echo "[update-darwin] $(date '+%Y-%m-%d %H:%M:%S') $*" >> "$LOG_FILE"
}

# P1-5: 总是清理 lock 文件（无论成功失败）
cleanup_lock() {
  if [ -f "$LOCK_FILE" ]; then
    rm -f "$LOCK_FILE" 2>/dev/null || true
    log "cleanup lock"
  fi
}

# 使用 EXIT trap 确保任何退出路径都会清理 lock（包括 exit 1）
trap 'rc=$?; if [ $rc -ne 0 ]; then log "script exit with error, code=$rc"; fi; cleanup_lock' EXIT

log "start update: pid=$PID target=$TARGET_APP new=$NEW_APP backup=$BACKUP_APP"

# macOS PPID detection using ps
get_ppid() {
  ps -o ppid= -p "$$" 2>/dev/null | tr -d '[:space:]' || true
}

# Get initial PPID to verify parent-child relationship
PPID_INIT="$(get_ppid)"
USE_PPID_CHECK=0
if [ -n "$PPID_INIT" ] && [ "$PPID_INIT" = "$PID" ]; then
  USE_PPID_CHECK=1
  log "PPID check enabled: initial ppid=$PPID_INIT matches target pid=$PID"
else
  log "PPID check disabled: initial ppid=${PPID_INIT:-unknown} != target pid=$PID, using kill -0 only"
fi

# Wait for main process to exit (max ~30 seconds)
# Single loop: check both kill -0 and PPID change
exit_ok=0
for i in {1..300}; do
  # Primary check: process no longer exists
  if ! kill -0 "$PID" 2>/dev/null; then
    exit_ok=1
    log "main process exited (kill -0 failed)"
    break
  fi
  # Secondary check: PPID changed (if enabled)
  if [ "$USE_PPID_CHECK" -eq 1 ]; then
    PPID_NOW="$(get_ppid)"
    if [ -n "$PPID_NOW" ] && [ "$PPID_NOW" != "$PID" ]; then
      exit_ok=1
      log "main process exited (ppid changed: $PPID_INIT -> $PPID_NOW)"
      break
    fi
  fi
  sleep 0.1
done

if [ "$exit_ok" -ne 1 ]; then
  log "timeout: main process did not exit after 30s"
  exit 1
fi

sleep 0.5

# backup old app
if [ -d "$TARGET_APP" ]; then
  log "backup old app to $BACKUP_APP"
  rm -rf "$BACKUP_APP" 2>/dev/null || true
  mv "$TARGET_APP" "$BACKUP_APP"
fi

# replace with new app
log "replace new app to $TARGET_APP"
if ! mv "$NEW_APP" "$TARGET_APP"; then
  log "replace failed, rollback"
  if [ -d "$BACKUP_APP" ]; then
    mv "$BACKUP_APP" "$TARGET_APP" 2>/dev/null || true
  fi
  exit 1
fi

# remove quarantine attribute
if command -v xattr >/dev/null 2>&1; then
  log "remove quarantine attribute"
  xattr -cr "$TARGET_APP" 2>/dev/null || log "remove quarantine failed (ignored)"
fi

log "relaunch app"
if ! open -n -a "$TARGET_APP" >/dev/null 2>&1; then
  log "warning: open command failed, app may not have launched"
  log "backup preserved at: $BACKUP_APP"
  exit 1
fi
log "relaunch ok"

# P1-5: 成功后清理 pending 标记
if [ -f "$PENDING_FILE" ]; then
  rm -f "$PENDING_FILE" 2>/dev/null || true
  log "cleanup pending"
fi

sleep 2
rm -rf "$BACKUP_APP" 2>/dev/null || true
log "cleanup backup"

log "cleanup temp dir $EXTRACT_DIR"
rm -rf "$EXTRACT_DIR" 2>/dev/null || true

log "cleanup script $0"
rm -f "$0" 2>/dev/null || true

# cleanup_lock 由 EXIT trap 自动调用，无需手动调用
log "update completed"
exit 0
`

	if err := os.WriteFile(scriptPath, []byte(bashScript), 0o755); err != nil {
		_ = os.RemoveAll(extractDir)
		return fmt.Errorf("写入更新脚本失败: %w", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		_ = os.RemoveAll(extractDir)
		return fmt.Errorf("设置更新脚本执行权限失败: %w", err)
	}

	log.Printf("[UpdateService] 已创建 macOS 更新脚本: %s", scriptPath)

	cmd := exec.Command(
		"/bin/bash",
		scriptPath,
		fmt.Sprint(pid),
		targetAppPath,
		newAppPath,
		backupAppPath,
		extractDir,
		logFile,
		pendingFile,
		lockFile,
	)
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(extractDir)
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}

	log.Printf("[UpdateService] 更新脚本已启动 (PID=%d)，准备退出主程序...", cmd.Process.Pid)

	// P1-5: 不再在此处调用 clearPendingState()，由脚本负责
	us.releaseUpdateLock()

	os.Exit(0)
	return nil
}

// findNewAppBundle 在解压目录中查找 .app 包
// 优先策略：1) 同名优先 2) 浅层优先 3) 递归查找
func findNewAppBundle(extractDir, preferredName string) (string, error) {
	// 1. 根目录同名优先
	if preferredName != "" {
		candidate := filepath.Join(extractDir, preferredName)
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			log.Printf("[UpdateService] 找到同名 .app: %s", candidate)
			return candidate, nil
		}
	}

	var candidates []string

	// 2. 根目录直接 .app
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return "", fmt.Errorf("读取解压目录失败: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".app") {
			candidates = append(candidates, filepath.Join(extractDir, e.Name()))
		}
	}

	// 3. 一层子目录内的 .app
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subDir := filepath.Join(extractDir, e.Name())
		subEntries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if se.IsDir() && strings.HasSuffix(strings.ToLower(se.Name()), ".app") {
				candidates = append(candidates, filepath.Join(subDir, se.Name()))
			}
		}
	}

	// 4. 兜底递归，遇到 .app 就停止深入（避免 helper app）
	if len(candidates) == 0 {
		filepath.WalkDir(extractDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".app") {
				candidates = append(candidates, path)
				return filepath.SkipDir
			}
			return nil
		})
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("解压后未找到新的 .app 包（目录=%s）", extractDir)
	}

	// 5. 选择策略：同名优先；否则选择最浅层（路径深度最小）
	selected := candidates[0]
	minDepth := strings.Count(strings.TrimPrefix(selected, extractDir), string(os.PathSeparator))

	for _, cand := range candidates[1:] {
		depth := strings.Count(strings.TrimPrefix(cand, extractDir), string(os.PathSeparator))
		if depth < minDepth {
			selected = cand
			minDepth = depth
		}
	}

	log.Printf("[UpdateService] 从 %d 个候选中选择: %s (深度=%d)", len(candidates), selected, minDepth)
	return selected, nil
}

// applyUpdateLinux Linux 平台更新（脚本方式，避免 ETXTBSY）
func (us *UpdateService) applyUpdateLinux(appImagePath string) error {
	// 1. SHA256 校验
	us.mu.Lock()
	var expectedHash string
	if us.latestUpdateInfo != nil {
		expectedHash = us.latestUpdateInfo.SHA256
	}
	us.mu.Unlock()

	if expectedHash != "" {
		actualHash, err := calculateSHA256(appImagePath)
		if err != nil {
			return fmt.Errorf("计算 SHA256 失败: %w", err)
		}
		if !strings.EqualFold(actualHash, expectedHash) {
			return fmt.Errorf("SHA256 校验失败: 期望 %s, 实际 %s", expectedHash, actualHash)
		}
		log.Println("[UpdateService] SHA256 校验通过")
	}

	// 2. ELF 格式校验
	f, err := os.Open(appImagePath)
	if err != nil {
		return fmt.Errorf("无法打开 AppImage: %w", err)
	}
	magic := make([]byte, 4)
	_, err = f.Read(magic)
	f.Close()
	if err != nil || magic[0] != 0x7F || magic[1] != 'E' || magic[2] != 'L' || magic[3] != 'F' {
		return fmt.Errorf("无效的 AppImage 格式（非 ELF）")
	}

	// 3. 获取当前可执行文件路径
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(currentExe); err == nil {
		currentExe = resolved
	}

	// AppImage 运行时 os.Executable() 返回 /tmp/.mount_* 内部路径
	// 仅当检测到 AppImage 挂载特征时才信任 APPIMAGE 环境变量
	targetExe := currentExe
	appimageEnv := strings.TrimSpace(os.Getenv("APPIMAGE"))
	isAppImageMount := strings.Contains(currentExe, "/.mount_") // 支持 $TMPDIR 不同于 /tmp 的情况

	if isAppImageMount && appimageEnv != "" && filepath.IsAbs(appimageEnv) {
		// 确保 APPIMAGE 不指向挂载内部（避免误覆盖内层文件）
		if !strings.Contains(appimageEnv, "/.mount_") {
			if resolved, err := filepath.EvalSymlinks(appimageEnv); err == nil {
				appimageEnv = resolved
			}
			// 解析 symlink 后再次检查是否指向挂载内部
			if strings.Contains(appimageEnv, "/.mount_") {
				log.Printf("[UpdateService] APPIMAGE 解析后指向挂载内部，忽略: %s", appimageEnv)
			} else if fi, statErr := os.Stat(appimageEnv); statErr == nil && !fi.IsDir() {
				log.Printf("[UpdateService] 检测到 AppImage 挂载，使用 APPIMAGE 作为更新目标: %s (内部路径=%s)", appimageEnv, currentExe)
				targetExe = appimageEnv
			} else {
				log.Printf("[UpdateService] APPIMAGE 无效 (%v)，回退使用内部路径: %s", statErr, currentExe)
			}
		} else {
			log.Printf("[UpdateService] APPIMAGE 指向挂载内部，忽略: %s", appimageEnv)
		}
	} else if isAppImageMount {
		log.Printf("[UpdateService] 检测到 AppImage 挂载但 APPIMAGE 未设置或无效，使用内部路径: %s", currentExe)
	}

	pid := os.Getpid()
	parentDir := filepath.Dir(targetExe)

	// 4. 检查目标目录可写
	testFile := filepath.Join(parentDir, fmt.Sprintf(".updateservice-write-test-%d", pid))
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		log.Printf("[UpdateService] 目标目录不可写: %s, err=%v", parentDir, err)
		return fmt.Errorf("目标目录不可写，无法自动更新到 %s，请手动替换或使用管理员权限", parentDir)
	}
	_ = os.Remove(testFile)

	// 5. 构建 bash 脚本
	scriptPath := filepath.Join(us.updateDir, fmt.Sprintf("update-linux-%d.sh", time.Now().UnixNano()))
	logFile := filepath.Join(us.updateDir, "update-linux.log")
	backupPath := targetExe + ".old"

	// P1-5: 获取 pending 和 lock 文件路径
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	lockFile := filepath.Join(us.updateDir, "update.lock")

	bashScript := `#!/bin/bash
set -euo pipefail

PID="$1"
TARGET_EXE="$2"
NEW_EXE="$3"
BACKUP_EXE="$4"
LOG_FILE="$5"
PENDING_FILE="$6"
LOCK_FILE="$7"

log() {
  echo "[update-linux] $(date '+%Y-%m-%d %H:%M:%S') $*" >> "$LOG_FILE"
}

# P1-5: 总是清理 lock 文件（无论成功失败）
cleanup_lock() {
  if [ -f "$LOCK_FILE" ]; then
    rm -f "$LOCK_FILE" 2>/dev/null || true
    log "cleanup lock"
  fi
}

# 使用 EXIT trap 确保任何退出路径都会清理 lock（包括 exit 1）
trap 'rc=$?; if [ $rc -ne 0 ]; then log "script exit with error, code=$rc"; fi; cleanup_lock' EXIT

log "start update: pid=$PID target=$TARGET_EXE new=$NEW_EXE backup=$BACKUP_EXE"

# Linux-specific PPID detection using /proc (more reliable than ps -o)
get_ppid() {
  if [ -r "/proc/$$/stat" ]; then
    # /proc/$$/stat format: pid (comm) state ppid ...
    # comm may contain spaces/parens, so we strip everything up to ") " first
    local stat_content
    stat_content="$(cat /proc/$$/stat 2>/dev/null || true)"
    if [ -n "$stat_content" ]; then
      # Remove "pid (comm) " prefix, then get first field (state), second is ppid
      local after_comm="${stat_content#*) }"
      echo "$after_comm" | cut -d' ' -f2
      return
    fi
  fi
  # Fallback to ps
  ps -o ppid= -p "$$" 2>/dev/null | tr -d '[:space:]' || true
}

# Get initial PPID to verify parent-child relationship
PPID_INIT="$(get_ppid)"
USE_PPID_CHECK=0
if [ -n "$PPID_INIT" ] && [ "$PPID_INIT" = "$PID" ]; then
  USE_PPID_CHECK=1
  log "PPID check enabled: initial ppid=$PPID_INIT matches target pid=$PID"
else
  log "PPID check disabled: initial ppid=${PPID_INIT:-unknown} != target pid=$PID, using kill -0 only"
fi

# Wait for main process to exit (max ~30 seconds)
# Single loop: check both kill -0 and PPID change
exit_ok=0
for i in {1..300}; do
  # Primary check: process no longer exists
  if ! kill -0 "$PID" 2>/dev/null; then
    exit_ok=1
    log "main process exited (kill -0 failed)"
    break
  fi
  # Secondary check: PPID changed (if enabled)
  if [ "$USE_PPID_CHECK" -eq 1 ]; then
    PPID_NOW="$(get_ppid)"
    if [ -n "$PPID_NOW" ] && [ "$PPID_NOW" != "$PID" ]; then
      exit_ok=1
      log "main process exited (ppid changed: $PPID_INIT -> $PPID_NOW)"
      break
    fi
  fi
  sleep 0.1
done

if [ "$exit_ok" -ne 1 ]; then
  log "timeout: main process did not exit after 30s"
  exit 1
fi

sleep 0.5

# backup old executable
if [ -f "$TARGET_EXE" ]; then
  log "backup old executable to $BACKUP_EXE"
  rm -f "$BACKUP_EXE" 2>/dev/null || true
  mv "$TARGET_EXE" "$BACKUP_EXE"
fi

# copy new executable (mv may fail across filesystems)
log "copy new executable to $TARGET_EXE"
if ! cp "$NEW_EXE" "$TARGET_EXE"; then
  log "copy failed, rollback"
  if [ -f "$BACKUP_EXE" ]; then
    mv "$BACKUP_EXE" "$TARGET_EXE" 2>/dev/null || true
  fi
  exit 1
fi

# set executable permission
chmod 755 "$TARGET_EXE"

sleep 2
rm -f "$BACKUP_EXE" 2>/dev/null || true
rm -f "$NEW_EXE" 2>/dev/null || true

log "relaunch app"
nohup "$TARGET_EXE" >/dev/null 2>&1 &

# P1-5: 成功后清理 pending 标记
if [ -f "$PENDING_FILE" ]; then
  rm -f "$PENDING_FILE" 2>/dev/null || true
  log "cleanup pending"
fi

log "cleanup script $0"
rm -f "$0" 2>/dev/null || true

# cleanup_lock 由 EXIT trap 自动调用，无需手动调用
log "update completed"
exit 0
`

	if err := os.WriteFile(scriptPath, []byte(bashScript), 0o755); err != nil {
		return fmt.Errorf("写入更新脚本失败: %w", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		return fmt.Errorf("设置更新脚本执行权限失败: %w", err)
	}

	log.Printf("[UpdateService] 已创建 Linux 更新脚本: %s", scriptPath)

	// 查找 bash 路径（兼容 NixOS 等非标准 FHS 发行版）
	bashPath, lookErr := exec.LookPath("bash")
	if lookErr != nil {
		bashPath = "/bin/bash" // 兼容旧系统的默认路径
	}
	if _, statErr := os.Stat(bashPath); statErr != nil {
		return fmt.Errorf("未找到 bash（需要 bash 执行更新脚本），请手动替换 AppImage")
	}
	log.Printf("[UpdateService] 使用 bash: %s", bashPath)

	cmd := exec.Command(
		bashPath,
		scriptPath,
		fmt.Sprint(pid),
		targetExe,
		appImagePath,
		backupPath,
		logFile,
		pendingFile,
		lockFile,
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}

	log.Printf("[UpdateService] 更新脚本已启动 (PID=%d)，准备退出主程序...", cmd.Process.Pid)

	// P1-5: 不再在此处调用 clearPendingState()，由脚本负责
	us.releaseUpdateLock()

	os.Exit(0)
	return nil
}

// cleanupOldBackups 清理旧备份文件，保留最近 n 个
func (us *UpdateService) cleanupOldBackups(dir, pattern string, keep int) {
	matches, _ := filepath.Glob(filepath.Join(dir, pattern))
	if len(matches) <= keep {
		return
	}

	// 按修改时间排序（新 → 旧）
	sort.Slice(matches, func(i, j int) bool {
		fi, _ := os.Stat(matches[i])
		fj, _ := os.Stat(matches[j])
		if fi == nil || fj == nil {
			return false
		}
		return fi.ModTime().After(fj.ModTime())
	})

	// 删除旧的
	for _, f := range matches[keep:] {
		os.Remove(f)
		log.Printf("[UpdateService] 清理旧备份: %s", f)
	}
}

// RestartApp 重启应用
// 如果有待安装的更新，会先触发更新流程（Windows 安装版会请求 UAC）
func (us *UpdateService) RestartApp() error {
	// 有待安装的更新时直接触发安装（Windows 安装版会请求 UAC）
	if err := us.ApplyUpdate(); err != nil {
		log.Printf("[UpdateService] 应用更新失败，将执行普通重启: %v", err)
	}

	// ApplyUpdate 在成功安装更新时会退出进程；走到这里说明没有待安装任务或更新失败
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		cmd := hideWindowCmd(executable)
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
// P1-3 修复：单次持锁完成检查+调度，消除竞态窗口
func (us *UpdateService) StartDailyCheck() {
	us.mu.Lock()
	defer us.mu.Unlock()

	// 停止旧定时器
	if us.dailyCheckTimer != nil {
		us.dailyCheckTimer.Stop()
		us.dailyCheckTimer = nil
	}

	if !us.autoCheckEnabled {
		log.Println("[UpdateService] 自动检查已禁用，不启动定时器")
		return
	}

	// 注意：calculateNextCheckDuration 不访问共享状态，无需加锁
	duration := us.calculateNextCheckDuration()

	us.dailyCheckTimer = time.AfterFunc(duration, func() {
		// 检查是否仍然启用
		us.mu.Lock()
		enabled := us.autoCheckEnabled
		us.mu.Unlock()

		if !enabled {
			log.Println("[UpdateService] 自动检查已禁用，跳过本次检查")
			return
		}

		us.performDailyCheck()
		us.StartDailyCheck() // 重新调度下次检查
	})

	log.Printf("[UpdateService] 定时检查已启动，下次检查时间: %s", time.Now().Add(duration).Format("2006-01-02 15:04:05"))
}

// StopDailyCheck 停止定时检查（公开方法，供外部调用）
// P1-3 修复：对 dailyCheckTimer 访问加锁
func (us *UpdateService) StopDailyCheck() {
	us.mu.Lock()
	defer us.mu.Unlock()

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

	// DownloadUpdate 内部已调用 PrepareUpdate，无需重复调用
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
		us.StopDailyCheck()
	}

	us.SaveState()
}

// SaveState 保存状态（使用原子写入防止断电损坏）
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

	// P1-1 修复：使用原子写入替代直接 WriteFile
	return atomicWriteFile(us.stateFile, data, 0o644)
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

	// 预先检查 pending 标记文件，避免在持锁状态下做文件 IO
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	pendingExists := false
	if _, err := os.Stat(pendingFile); err == nil {
		pendingExists = true
	} else if err != nil && !os.IsNotExist(err) {
		// 其他错误（权限/IO 等）时保守处理为不存在，避免误显示 Ready
		log.Printf("[UpdateService] 检查 pending 标记失败: %v，将视为无待更新", err)
	}

	needSave := false

	us.mu.Lock()
	us.lastCheckTime = state.LastCheckTime
	us.checkFailures = state.ConsecutiveFailures
	us.latestVersion = state.LatestKnownVersion
	us.downloadProgress = state.DownloadProgress

	// 验证 updateReady 状态：pending 文件才是权威来源
	switch {
	case state.UpdateReady && !pendingExists:
		// 状态文件显示 updateReady=true 但实际没有待更新文件，重置状态
		log.Printf("[UpdateService] 检测到过期的 updateReady 状态，重置为 false")
		us.updateReady = false
		us.downloadProgress = 0
		needSave = true
	case !state.UpdateReady && pendingExists:
		// pending 文件存在但状态为 false（可能是上次 SaveState 失败），修正为 true
		log.Printf("[UpdateService] 检测到 pending 标记存在但状态为 false，修正为 true")
		us.updateReady = true
		if us.downloadProgress < 100 {
			us.downloadProgress = 100
		}
		needSave = true
	default:
		us.updateReady = state.UpdateReady
	}

	// 检查文件中是否包含 auto_check_enabled 字段
	// 如果包含，使用文件中的值；否则保持默认值 true（兼容老版本）
	dataStr := string(data)
	if strings.Contains(dataStr, "\"auto_check_enabled\"") {
		// 文件中包含 auto_check_enabled 字段，使用文件中的值
		us.autoCheckEnabled = state.AutoCheckEnabled
	}
	// 否则保持初始化时设置的默认值 true
	us.mu.Unlock()

	// 如果状态被修正，保存修正后的状态（需要在 unlock 后调用，避免死锁）
	if needSave {
		_ = us.SaveState()
	}

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

// ============================================================
// 以下为 Windows 安装版静默更新相关方法
// ============================================================

// acquireUpdateLock 获取更新锁（防止并发更新）
func (us *UpdateService) acquireUpdateLock() error {
	lockPath := filepath.Join(us.updateDir, "update.lock")

	// 最多尝试 2 次（初次 + 删除过期锁后重试）
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			// 成功获取锁，写入 PID 和时间戳
			if _, writeErr := fmt.Fprintf(f, "%d\n%s", os.Getpid(), time.Now().Format(time.RFC3339)); writeErr != nil {
				f.Close()
				os.Remove(lockPath)
				return fmt.Errorf("写入锁文件失败: %w", writeErr)
			}
			if closeErr := f.Close(); closeErr != nil {
				os.Remove(lockPath)
				return fmt.Errorf("关闭锁文件失败: %w", closeErr)
			}
			us.mu.Lock()
			us.lockFile = lockPath
			us.mu.Unlock()
			log.Printf("[UpdateService] 已获取更新锁: %s", lockPath)
			return nil
		}

		if !os.IsExist(err) {
			return fmt.Errorf("创建锁文件失败: %w", err)
		}

		// 锁文件已存在，检查是否过期
		info, statErr := os.Stat(lockPath)
		if statErr != nil {
			// stat 失败，锁可能已被删除，重试
			continue
		}

		// P1-6: 增加阈值到 30 分钟，因为：
		// - 3 次下载重试 × 5 分钟 HTTP 超时 = 15 分钟
		// - 重试退避 (2s + 4s) = 6 秒
		// - SHA256 校验 + prepare = ~1-2 分钟
		// - I/O 延迟/杀毒软件缓冲 = ~10 分钟
		// 总计最大锁持有时间: ~17 分钟，30 分钟提供安全余量
		if time.Since(info.ModTime()) > 30*time.Minute {
			log.Printf("[UpdateService] 检测到过期锁文件（超过30分钟，mtime=%v），强制删除: %s",
				info.ModTime().Format(time.RFC3339), lockPath)
			if rmErr := os.Remove(lockPath); rmErr != nil {
				return fmt.Errorf("删除过期锁文件失败: %w", rmErr)
			}
			continue // 重试获取
		}

		return fmt.Errorf("另一个更新正在进行中")
	}

	return fmt.Errorf("获取更新锁失败：重试次数耗尽")
}

// releaseUpdateLock 释放更新锁
func (us *UpdateService) releaseUpdateLock() {
	us.mu.Lock()
	lockFile := us.lockFile
	us.lockFile = ""
	us.mu.Unlock()

	if lockFile != "" {
		if err := os.Remove(lockFile); err != nil {
			log.Printf("[UpdateService] 释放锁文件失败: %v", err)
		} else {
			log.Printf("[UpdateService] 已释放更新锁: %s", lockFile)
		}
	}
}

// downloadAndVerify 下载文件并验证 SHA256
func (us *UpdateService) downloadAndVerify(assetName string) (string, error) {
	releaseBaseURL := "https://github.com/Rogers-F/code-switch-R/releases/download"

	// 检查版本是否已设置
	us.mu.Lock()
	version := us.latestVersion
	us.mu.Unlock()
	if version == "" {
		return "", fmt.Errorf("latestVersion 未设置，请先调用 CheckUpdate")
	}

	// 1. 下载主文件
	mainURL := fmt.Sprintf("%s/%s/%s", releaseBaseURL, version, assetName)
	mainPath := filepath.Join(us.updateDir, assetName)

	log.Printf("[UpdateService] 下载文件: %s", mainURL)
	if err := us.downloadFile(mainURL, mainPath); err != nil {
		return "", fmt.Errorf("下载 %s 失败: %w", assetName, err)
	}

	// 2. 下载哈希文件
	hashURL := mainURL + ".sha256"
	hashPath := mainPath + ".sha256"

	log.Printf("[UpdateService] 下载哈希文件: %s", hashURL)
	if err := us.downloadFile(hashURL, hashPath); err != nil {
		os.Remove(mainPath) // 清理已下载的主文件
		return "", fmt.Errorf("下载哈希文件失败: %w", err)
	}

	// 3. 解析哈希文件（格式: "hash  filename"）
	hashContent, err := os.ReadFile(hashPath)
	if err != nil {
		os.Remove(mainPath)
		os.Remove(hashPath)
		return "", fmt.Errorf("读取哈希文件失败: %w", err)
	}

	fields := strings.Fields(string(hashContent))
	if len(fields) == 0 {
		os.Remove(mainPath)
		os.Remove(hashPath)
		return "", fmt.Errorf("哈希文件格式错误")
	}
	expectedHash := fields[0]
	os.Remove(hashPath) // 哈希文件用完即删

	// 4. 校验主文件
	if err := us.verifyDownload(mainPath, expectedHash); err != nil {
		os.Remove(mainPath)
		return "", err
	}

	log.Printf("[UpdateService] 文件校验通过: %s", mainPath)
	return mainPath, nil
}

// downloadFile 下载单个文件
func (us *UpdateService) downloadFile(url, destPath string) error {
	client := &http.Client{
		Timeout: 5 * time.Minute, // 大文件下载超时
	}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// verifyDownload 验证下载文件的 SHA256
func (us *UpdateService) verifyDownload(filePath, expectedHash string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算哈希失败: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))

	if !strings.EqualFold(actual, expectedHash) {
		return fmt.Errorf("SHA256 校验失败: 期望 %s, 实际 %s", expectedHash, actual)
	}

	log.Printf("[UpdateService] SHA256 校验通过: %s", filePath)
	return nil
}

// downloadUpdater 从 GitHub Release 下载 updater.exe
// P1-4 修复：移除无校验降级，强制要求 SHA256 校验
func (us *UpdateService) downloadUpdater(targetPath string) error {
	// 下载带 SHA256 校验的 updater.exe
	updaterPath, err := us.downloadAndVerify("updater.exe")
	if err != nil {
		// 不再降级，直接返回错误
		return fmt.Errorf("下载 updater.exe 失败（需要 SHA256 校验）: %w", err)
	}

	// 如果下载路径不同，移动文件
	if updaterPath != targetPath {
		if err := os.Rename(updaterPath, targetPath); err != nil {
			// 重命名失败，尝试复制
			if err := copyUpdateFile(updaterPath, targetPath); err != nil {
				return fmt.Errorf("移动 updater.exe 失败: %w", err)
			}
			os.Remove(updaterPath)
		}
	}

	return nil
}

// calculateTimeout 根据文件大小动态计算超时时间
func calculateTimeout(fileSize int64) int {
	base := 30 // 基础 30 秒
	// 每 100MB 增加 10 秒
	extra := int(fileSize / (100 * 1024 * 1024)) * 10
	return base + extra
}

// applyInstalledUpdate 安装版更新逻辑（使用 PowerShell UAC 提权）
// 通过 PowerShell 的 Start-Process -Verb RunAs 触发 UAC 弹窗
func (us *UpdateService) applyInstalledUpdate(newExePath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}
	currentExe, _ = filepath.EvalSymlinks(currentExe)

	// 1. 获取或下载 updater.exe
	updaterPath := filepath.Join(us.updateDir, "updater.exe")
	if _, err := os.Stat(updaterPath); os.IsNotExist(err) {
		log.Printf("[UpdateService] updater.exe 不存在，开始下载...")
		if err := us.downloadUpdater(updaterPath); err != nil {
			return fmt.Errorf("下载更新器失败: %w", err)
		}
	}

	// 2. 计算超时时间
	fileInfo, err := os.Stat(newExePath)
	if err != nil {
		return fmt.Errorf("获取新版本文件信息失败: %w", err)
	}
	timeout := calculateTimeout(fileInfo.Size())

	// 3. 创建更新任务配置
	taskFile := filepath.Join(us.updateDir, "update-task.json")
	// P1-5: cleanup_paths 包含 pending 和 lock 文件，由 updater.exe 成功后清理
	lockFile := filepath.Join(us.updateDir, "update.lock")
	task := map[string]interface{}{
		"main_pid":     os.Getpid(),
		"target_exe":   currentExe,
		"new_exe_path": newExePath,
		"backup_path":  currentExe + ".old",
		"cleanup_paths": []string{
			newExePath,
			filepath.Join(filepath.Dir(us.stateFile), ".pending-update"),
			lockFile,
		},
		"timeout_sec": timeout,
	}

	taskData, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化任务配置失败: %w", err)
	}

	if err := os.WriteFile(taskFile, taskData, 0o644); err != nil {
		return fmt.Errorf("写入任务配置失败: %w", err)
	}

	log.Printf("[UpdateService] 已创建更新任务: %s", taskFile)
	log.Printf("[UpdateService] 任务配置: PID=%d, Timeout=%ds", os.Getpid(), timeout)

	// 4. 使用 PowerShell 以管理员权限启动 updater.exe
	// Start-Process -Verb RunAs 会触发 UAC 弹窗
	// 注意：-ArgumentList 需要用双引号包裹路径，防止空格路径被拆分
	log.Printf("[UpdateService] 使用 UAC 提权启动更新器: %s", updaterPath)
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-Command",
		fmt.Sprintf(`Start-Process -FilePath '%s' -ArgumentList ('"%s"') -Verb RunAs -WindowStyle Hidden`,
			strings.ReplaceAll(updaterPath, `'`, `''`),
			strings.ReplaceAll(taskFile, `'`, `''`),
		),
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.ToLower(string(out))
		// 兼容中英文提示，识别 UAC 取消
		if strings.Contains(outStr, "canceled by the user") ||
			strings.Contains(outStr, "cancelled by the user") ||
			strings.Contains(outStr, "operation was canceled") ||
			strings.Contains(outStr, "取消") {
			log.Printf("[UpdateService] 用户取消 UAC，输出: %s", strings.TrimSpace(string(out)))
			return ErrUACDenied
		}
		return fmt.Errorf("启动 UAC 提权更新器失败: %w, 输出: %s", err, strings.TrimSpace(string(out)))
	}

	// P1-5: 不再在此处调用 clearPendingState()，由 updater.exe 成功后通过 cleanup_paths 清理
	log.Printf("[UpdateService] UAC 提权请求已确认，准备退出主程序...")

	// 5. 释放更新锁
	us.releaseUpdateLock()

	// 6. 退出主程序
	os.Exit(0)
	return nil
}
