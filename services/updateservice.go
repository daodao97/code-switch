package services

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/singleflight"
)

// ==================== 状态定义 ====================

// UpdateState 更新状态枚举
type UpdateState string

const (
	StateIdle        UpdateState = "idle"        // 空闲，无更新任务
	StateChecking    UpdateState = "checking"    // 正在检查更新
	StateAvailable   UpdateState = "available"   // 有可用更新
	StateDownloading UpdateState = "downloading" // 正在下载
	StateReady       UpdateState = "ready"       // 下载完成，待安装
	StateApplying    UpdateState = "applying"    // 正在应用更新
	StateError       UpdateState = "error"       // 发生错误
)

// UpdatePolicy 更新策略
type UpdatePolicy string

const (
	PolicyAuto      UpdatePolicy = "auto"      // 自动检测（默认）
	PolicyPortable  UpdatePolicy = "portable"  // 便携版：自替换
	PolicyInstaller UpdatePolicy = "installer" // 安装版：下载安装器
)

// ==================== 数据结构 ====================

// UpdateInfo 更新信息
type UpdateInfo struct {
	Version     string    `json:"version"`
	PubDate     time.Time `json:"pub_date"`
	Notes       string    `json:"notes"`
	DownloadURL string    `json:"download_url"`
	SHA256      string    `json:"sha256"`
	Size        int64     `json:"size"`
}

// LatestManifest latest.json 清单格式
type LatestManifest struct {
	Version   string                     `json:"version"`
	PubDate   time.Time                  `json:"pub_date"`
	Notes     string                     `json:"notes"`
	Platforms map[string]PlatformRelease `json:"platforms"`
}

// PlatformRelease 平台发布信息
type PlatformRelease struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// DownloadState 断点续传状态
type DownloadState struct {
	URL             string `json:"url"`
	ExpectedSHA256  string `json:"expected_sha256"`
	ExpectedSize    int64  `json:"expected_size"`
	ETag            string `json:"etag"`
	LastModified    string `json:"last_modified"`
	DownloadedBytes int64  `json:"downloaded_bytes"`
	TempFilePath    string `json:"temp_file_path"`
}

// PendingApply 待应用更新标记
type PendingApply struct {
	TargetVersion string    `json:"target_version"`
	Method        string    `json:"method"` // "swap" | "installer"
	FilePath      string    `json:"file_path"`
	FileSHA256    string    `json:"file_sha256"`
	StartedAt     time.Time `json:"started_at"`
}

// UpdateStateSnapshot 状态快照（返回给前端）
type UpdateStateSnapshot struct {
	State           UpdateState `json:"state"`
	CurrentVersion  string      `json:"current_version"`
	LatestVersion   string      `json:"latest_version,omitempty"`
	Notes           string      `json:"notes,omitempty"`
	DownloadURL     string      `json:"download_url,omitempty"`
	DownloadedBytes int64       `json:"downloaded_bytes"`
	TotalBytes      int64       `json:"total_bytes"`
	Progress        float64     `json:"progress"` // 0-100
	Error           string      `json:"error,omitempty"`
	ErrorOp         string      `json:"error_op,omitempty"` // "check" | "download" | "apply"
	Policy          string      `json:"policy"`
}

// ==================== 服务定义 ====================

// UpdateService 自动更新服务
type UpdateService struct {
	mu sync.Mutex

	// 状态
	state           UpdateState
	currentVersion  string
	targetInfo      *UpdateInfo // 当前更新目标
	downloadState   *DownloadState
	downloadedBytes int64
	totalBytes      int64
	lastError       string
	errorOp         string // "check" | "download" | "apply"

	// 忽略的版本
	dismissedVersion string

	// 事件发送
	app *application.App

	// 并发控制
	checkGroup  singleflight.Group
	cancelFunc  context.CancelFunc
	downloadCtx context.Context

	// 进度事件节流
	lastEmitTime    time.Time
	lastEmitPercent int
	lastEmitState   UpdateState

	// 配置
	dataDir      string // 数据目录，用于存储临时文件和状态
	cachedPolicy string // 缓存的更新策略，避免重复检测
}

// 常量
const (
	latestJSONURL     = "https://github.com/Rogers-F/code-switch-R/releases/latest/download/latest.json"
	githubAPIURL      = "https://api.github.com/repos/Rogers-F/code-switch-R/releases/latest"
	checkCooldown     = 60 * time.Second // 检查更新冷却时间
	progressThrottle  = 100 * time.Millisecond
	progressMinChange = 1 // 最小进度变化（百分比）
)

// URL 白名单
var allowedURLPrefixes = []string{
	"https://github.com/Rogers-F/code-switch-R/releases/download/",
	"https://github.com/Rogers-F/code-switch-R/releases/latest/download/",
	"https://objects.githubusercontent.com/", // GitHub 重定向目标
}

// NewUpdateService 创建更新服务
func NewUpdateService(currentVersion string) *UpdateService {
	dataDir := getUpdateDataDir()

	// 确保数据目录存在
	os.MkdirAll(dataDir, 0755)

	us := &UpdateService{
		state:          StateIdle,
		currentVersion: currentVersion,
		dataDir:        dataDir,
	}

	// 读取已忽略的版本
	dismissPath := filepath.Join(dataDir, "dismissed_version.txt")
	if data, err := os.ReadFile(dismissPath); err == nil {
		us.dismissedVersion = strings.TrimSpace(string(data))
	}

	// 初始化时检测并缓存更新策略（只做一次 I/O）
	us.cachedPolicy = string(us.detectPolicy())

	// 启动时检查是否有待应用的更新
	us.checkPendingApply()

	return us
}

// SetApp 设置 Wails App 引用
func (us *UpdateService) SetApp(app *application.App) {
	us.app = app
}

// ==================== 公开 API ====================

// CheckUpdate 检查更新
// 返回更新信息，如果无更新则返回 nil
func (us *UpdateService) CheckUpdate() (*UpdateInfo, error) {
	us.mu.Lock()
	// 如果已在下载/准备/应用状态，不覆盖当前目标
	if us.state == StateDownloading || us.state == StateReady || us.state == StateApplying {
		info := us.targetInfo
		us.mu.Unlock()
		return info, nil
	}
	us.state = StateChecking
	us.mu.Unlock()

	// 使用 singleflight 防止并发重复检查
	result, err, _ := us.checkGroup.Do("check", func() (interface{}, error) {
		return us.doCheckUpdate()
	})

	us.mu.Lock()
	defer us.mu.Unlock()

	if err != nil {
		us.state = StateError
		us.lastError = err.Error()
		us.errorOp = "check"
		us.emitState()
		return nil, err
	}

	info, ok := result.(*UpdateInfo)
	if !ok || info == nil {
		us.state = StateIdle
		us.emitState()
		return nil, nil
	}

	// 检查是否被忽略
	if us.dismissedVersion == info.Version {
		us.state = StateIdle
		us.emitState()
		return nil, nil
	}

	// 检查是否需要更新
	if !us.isNewerVersion(info.Version) {
		us.state = StateIdle
		us.emitState()
		return nil, nil
	}

	us.targetInfo = info
	us.totalBytes = info.Size
	us.state = StateAvailable
	us.emitState()

	return info, nil
}

// DownloadUpdate 下载更新
func (us *UpdateService) DownloadUpdate() error {
	us.mu.Lock()

	switch us.state {
	case StateDownloading:
		// 幂等：已在下载，直接返回
		us.mu.Unlock()
		return nil
	case StateReady:
		// 幂等：已下载完成
		us.mu.Unlock()
		return nil
	case StateAvailable:
		// 可以开始下载
	case StateError:
		if us.errorOp == "download" && us.targetInfo != nil {
			// 可以重试下载
		} else {
			us.mu.Unlock()
			return fmt.Errorf("invalid state for download: %s", us.state)
		}
	default:
		us.mu.Unlock()
		return fmt.Errorf("invalid state for download: %s (expected: available)", us.state)
	}

	if us.targetInfo == nil {
		us.mu.Unlock()
		return fmt.Errorf("no update target set")
	}

	// P0: SHA256 必须存在（fallback 无 SHA 时拒绝自动下载）
	if us.targetInfo.SHA256 == "" {
		us.state = StateError
		us.lastError = "SHA256 checksum not available. Please download manually from GitHub Releases."
		us.errorOp = "download"
		us.emitState()
		us.mu.Unlock()
		return fmt.Errorf("SHA256 checksum required for automatic download")
	}

	// 验证 URL 白名单
	if !isURLAllowed(us.targetInfo.DownloadURL) {
		us.mu.Unlock()
		return fmt.Errorf("download URL not in whitelist: %s", us.targetInfo.DownloadURL)
	}

	us.state = StateDownloading
	us.lastError = ""
	us.errorOp = ""
	us.emitState() // P0: emit downloading 状态

	// 创建取消上下文
	ctx, cancel := context.WithCancel(context.Background())
	us.cancelFunc = cancel
	us.downloadCtx = ctx

	targetInfo := us.targetInfo
	us.mu.Unlock()

	// 异步下载
	go us.doDownload(ctx, targetInfo)

	return nil
}

// CancelDownload 取消下载
func (us *UpdateService) CancelDownload() error {
	us.mu.Lock()
	defer us.mu.Unlock()

	if us.state != StateDownloading {
		return fmt.Errorf("not downloading")
	}

	if us.cancelFunc != nil {
		us.cancelFunc()
	}

	us.state = StateAvailable
	us.emitState()

	return nil
}

// RequestRestart 请求重启并更新
func (us *UpdateService) RequestRestart() error {
	us.mu.Lock()

	if us.state != StateReady {
		us.mu.Unlock()
		return fmt.Errorf("invalid state for restart: %s (expected: ready)", us.state)
	}

	if us.downloadState == nil || us.targetInfo == nil {
		us.mu.Unlock()
		return fmt.Errorf("no downloaded update")
	}

	us.state = StateApplying
	downloadState := us.downloadState
	targetInfo := us.targetInfo
	us.mu.Unlock()

	// 写入 pending_apply.json
	policy := us.detectPolicy()
	method := "swap"
	if policy == PolicyInstaller {
		method = "installer"
	}

	pending := &PendingApply{
		TargetVersion: targetInfo.Version,
		Method:        method,
		FilePath:      downloadState.TempFilePath,
		FileSHA256:    downloadState.ExpectedSHA256,
		StartedAt:     time.Now(),
	}

	pendingPath := filepath.Join(us.dataDir, "pending_apply.json")
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		us.mu.Lock()
		us.state = StateError
		us.lastError = fmt.Sprintf("failed to marshal pending apply: %v", err)
		us.errorOp = "apply"
		us.mu.Unlock()
		us.emitStateUnlocked() // P1: 错误路径补发状态事件
		return err
	}

	if err := os.WriteFile(pendingPath, data, 0644); err != nil {
		us.mu.Lock()
		us.state = StateError
		us.lastError = fmt.Sprintf("failed to write pending apply: %v", err)
		us.errorOp = "apply"
		us.mu.Unlock()
		us.emitStateUnlocked() // P1: 错误路径补发状态事件
		return err
	}

	// 启动更新脚本
	if err := us.launchUpdater(pending); err != nil {
		us.mu.Lock()
		us.state = StateError
		us.lastError = fmt.Sprintf("failed to launch updater: %v", err)
		us.errorOp = "apply"
		us.mu.Unlock()
		us.emitStateUnlocked() // P1: 错误路径补发状态事件
		return err
	}

	// 通知前端准备退出
	us.emitStateUnlocked() // P1: 改用 emitStateUnlocked（未持锁）

	// P0: 退出应用，让更新脚本接管
	if us.app != nil {
		us.app.Quit()
	}

	return nil
}

// GetState 获取当前状态快照
func (us *UpdateService) GetState() *UpdateStateSnapshot {
	us.mu.Lock()
	defer us.mu.Unlock()

	policy := us.cachedPolicy
	if policy == "" {
		policy = "auto"
	}

	snapshot := &UpdateStateSnapshot{
		State:           us.state,
		CurrentVersion:  us.currentVersion,
		DownloadedBytes: us.downloadedBytes,
		TotalBytes:      us.totalBytes,
		Error:           us.lastError,
		ErrorOp:         us.errorOp,
		Policy:          policy,
	}

	if us.targetInfo != nil {
		snapshot.LatestVersion = us.targetInfo.Version
		snapshot.Notes = us.targetInfo.Notes
		snapshot.DownloadURL = us.targetInfo.DownloadURL
	}

	if us.totalBytes > 0 {
		snapshot.Progress = float64(us.downloadedBytes) / float64(us.totalBytes) * 100
	}

	return snapshot
}

// DismissUpdate 忽略指定版本
func (us *UpdateService) DismissUpdate(version string) error {
	us.mu.Lock()

	if us.state == StateDownloading || us.state == StateReady || us.state == StateApplying {
		us.mu.Unlock()
		return fmt.Errorf("cannot dismiss while downloading/ready/applying")
	}

	us.dismissedVersion = version
	us.state = StateIdle
	us.targetInfo = nil
	us.mu.Unlock()

	// 持久化到本地存储（无需持锁）
	dismissPath := filepath.Join(us.dataDir, "dismissed_version.txt")
	_ = os.WriteFile(dismissPath, []byte(version), 0644)

	// 发送状态事件（无需持锁）
	us.emitStateUnlocked()

	return nil
}

// GetDismissedVersion 获取被忽略的版本
func (us *UpdateService) GetDismissedVersion() string {
	us.mu.Lock()
	defer us.mu.Unlock()
	return us.dismissedVersion
}

// ==================== 内部方法 ====================

// doCheckUpdate 执行检查更新
func (us *UpdateService) doCheckUpdate() (*UpdateInfo, error) {
	// 首先尝试从 latest.json 获取
	info, err := us.fetchFromLatestJSON()
	if err == nil && info != nil {
		return info, nil
	}

	// Fallback: 从 GitHub API 获取
	return us.fetchFromGitHubAPI()
}

// fetchFromLatestJSON 从 latest.json 获取更新信息
func (us *UpdateService) fetchFromLatestJSON() (*UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", latestJSONURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("latest.json returned status %d", resp.StatusCode)
	}

	var manifest LatestManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode latest.json: %w", err)
	}

	// 获取当前平台的发布信息
	platformKey := us.getPlatformKey()
	release, ok := manifest.Platforms[platformKey]
	if !ok {
		return nil, fmt.Errorf("no release for platform: %s", platformKey)
	}

	return &UpdateInfo{
		Version:     manifest.Version,
		PubDate:     manifest.PubDate,
		Notes:       manifest.Notes,
		DownloadURL: release.URL,
		SHA256:      release.SHA256,
		Size:        release.Size,
	}, nil
}

// fetchFromGitHubAPI 从 GitHub API 获取更新信息（Fallback）
func (us *UpdateService) fetchFromGitHubAPI() (*UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", githubAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release struct {
		TagName     string    `json:"tag_name"`
		PublishedAt time.Time `json:"published_at"`
		Body        string    `json:"body"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub API response: %w", err)
	}

	// 查找当前平台的资产
	assetName := us.getAssetName(release.TagName)
	var downloadURL string
	var size int64

	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			size = asset.Size
			break
		}
	}

	if downloadURL == "" {
		return nil, fmt.Errorf("no asset found for: %s", assetName)
	}

	return &UpdateInfo{
		Version:     release.TagName,
		PubDate:     release.PublishedAt,
		Notes:       release.Body,
		DownloadURL: downloadURL,
		SHA256:      "", // GitHub API 不提供 SHA256
		Size:        size,
	}, nil
}

// doDownload 执行下载
func (us *UpdateService) doDownload(ctx context.Context, info *UpdateInfo) {
	// 准备下载路径
	tempDir := filepath.Join(us.dataDir, "downloads")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		us.setDownloadError(fmt.Sprintf("failed to create temp dir: %v", err))
		return
	}

	// 根据 URL 确定文件名
	fileName := filepath.Base(info.DownloadURL)
	tempPath := filepath.Join(tempDir, fileName+".download")
	finalPath := filepath.Join(tempDir, fileName)

	// 尝试加载断点续传状态
	stateFile := filepath.Join(us.dataDir, "download_state.json")
	var dlState *DownloadState

	if data, err := os.ReadFile(stateFile); err == nil {
		var state DownloadState
		if json.Unmarshal(data, &state) == nil && state.URL == info.DownloadURL {
			dlState = &state
		}
	}

	// 检查临时文件是否存在
	var startByte int64 = 0
	if dlState != nil {
		if fi, err := os.Stat(dlState.TempFilePath); err == nil {
			startByte = fi.Size()
			if startByte > dlState.ExpectedSize {
				// 本地文件比预期大，删除重下
				os.Remove(dlState.TempFilePath)
				startByte = 0
				dlState = nil
			} else if startByte == dlState.ExpectedSize {
				// 已下载完成，跳到校验
				us.mu.Lock()
				us.downloadedBytes = startByte
				us.downloadState = dlState
				us.mu.Unlock()
				us.verifyAndFinalize(dlState.TempFilePath, finalPath, info)
				return
			}
		} else {
			dlState = nil
		}
	}

	// 初始化新的下载状态
	if dlState == nil {
		dlState = &DownloadState{
			URL:            info.DownloadURL,
			ExpectedSHA256: info.SHA256,
			ExpectedSize:   info.Size,
			TempFilePath:   tempPath,
		}
	}

	// HEAD 请求获取 ETag/Last-Modified
	headReq, err := http.NewRequestWithContext(ctx, "HEAD", info.DownloadURL, nil)
	if err != nil {
		us.setDownloadError(fmt.Sprintf("failed to create HEAD request: %v", err))
		return
	}

	headResp, err := http.DefaultClient.Do(headReq)
	if err != nil {
		us.setDownloadError(fmt.Sprintf("HEAD request failed: %v", err))
		return
	}
	headResp.Body.Close()

	// P0: 校验重定向后的最终 URL
	finalURL := headResp.Request.URL.String()
	if !isURLAllowed(finalURL) {
		us.setDownloadError(fmt.Sprintf("redirected URL not in whitelist: %s", finalURL))
		return
	}

	newETag := headResp.Header.Get("ETag")
	newLastModified := headResp.Header.Get("Last-Modified")

	// 检查是否需要重新下载
	if startByte > 0 {
		if (dlState.ETag != "" && newETag != dlState.ETag) ||
			(dlState.LastModified != "" && newLastModified != dlState.LastModified) {
			// 远端文件已变更，删除本地文件重下
			os.Remove(tempPath)
			startByte = 0
		}
	}

	dlState.ETag = newETag
	dlState.LastModified = newLastModified

	// 保存下载状态
	us.saveDownloadState(stateFile, dlState)

	// 发起下载请求
	req, err := http.NewRequestWithContext(ctx, "GET", info.DownloadURL, nil)
	if err != nil {
		us.setDownloadError(fmt.Sprintf("failed to create GET request: %v", err))
		return
	}

	// 设置 Range 和 If-Range
	if startByte > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startByte))
		if newETag != "" {
			req.Header.Set("If-Range", newETag)
		} else if newLastModified != "" {
			req.Header.Set("If-Range", newLastModified)
		}
	}
	req.Header.Set("Accept-Encoding", "identity") // 避免透明压缩

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		us.setDownloadError(fmt.Sprintf("download request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	// P0: 校验重定向后的最终 URL
	getFinalURL := resp.Request.URL.String()
	if !isURLAllowed(getFinalURL) {
		us.setDownloadError(fmt.Sprintf("GET redirected URL not in whitelist: %s", getFinalURL))
		return
	}

	// 处理响应
	var file *os.File
	switch resp.StatusCode {
	case http.StatusOK:
		// 200: 全量下载（忽略 Range 或远端变更）
		file, err = os.Create(tempPath)
		startByte = 0
	case http.StatusPartialContent:
		// 206: 断点续传
		// 验证 Content-Range
		contentRange := resp.Header.Get("Content-Range")
		if contentRange != "" {
			// 格式: bytes start-end/total
			var rangeStart, rangeEnd, rangeTotal int64
			fmt.Sscanf(contentRange, "bytes %d-%d/%d", &rangeStart, &rangeEnd, &rangeTotal)
			if rangeStart != startByte || rangeTotal != info.Size {
				// 不一致，全量重下
				os.Remove(tempPath)
				file, err = os.Create(tempPath)
				startByte = 0
			} else {
				file, err = os.OpenFile(tempPath, os.O_APPEND|os.O_WRONLY, 0644)
			}
		} else {
			file, err = os.OpenFile(tempPath, os.O_APPEND|os.O_WRONLY, 0644)
		}
	case http.StatusRequestedRangeNotSatisfiable:
		// 416: Range 无法满足，全量重下
		os.Remove(tempPath)
		file, err = os.Create(tempPath)
		startByte = 0
	default:
		us.setDownloadError(fmt.Sprintf("unexpected status code: %d", resp.StatusCode))
		return
	}

	if err != nil {
		us.setDownloadError(fmt.Sprintf("failed to open file: %v", err))
		return
	}
	defer file.Close()

	// 更新状态
	us.mu.Lock()
	us.downloadedBytes = startByte
	us.downloadState = dlState
	us.mu.Unlock()

	// 下载数据
	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		select {
		case <-ctx.Done():
			// 下载被取消
			dlState.DownloadedBytes = us.downloadedBytes
			us.saveDownloadState(stateFile, dlState)
			return
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				us.setDownloadError(fmt.Sprintf("failed to write file: %v", writeErr))
				return
			}

			us.mu.Lock()
			us.downloadedBytes += int64(n)
			downloaded := us.downloadedBytes
			us.mu.Unlock()

			// 发送进度事件（节流）
			us.emitProgressThrottled(downloaded, info.Size)
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			// P0: 检查是否是取消导致的错误，不要误报为 error
			if ctx.Err() != nil {
				// 下载被取消，保存续传状态后正常返回
				dlState.DownloadedBytes = us.downloadedBytes
				us.saveDownloadState(stateFile, dlState)
				return
			}
			us.setDownloadError(fmt.Sprintf("download error: %v", readErr))
			// 保存状态以便续传
			dlState.DownloadedBytes = us.downloadedBytes
			us.saveDownloadState(stateFile, dlState)
			return
		}
	}

	// 下载完成，验证并完成
	us.verifyAndFinalize(tempPath, finalPath, info)
}

// verifyAndFinalize 验证下载并完成
func (us *UpdateService) verifyAndFinalize(tempPath, finalPath string, info *UpdateInfo) {
	// SHA256 校验
	if info.SHA256 != "" {
		hash, err := computeSHA256(tempPath)
		if err != nil {
			us.setDownloadError(fmt.Sprintf("failed to compute SHA256: %v", err))
			return
		}
		if !strings.EqualFold(hash, info.SHA256) {
			os.Remove(tempPath)
			us.setDownloadError(fmt.Sprintf("SHA256 mismatch: expected %s, got %s", info.SHA256, hash))
			return
		}
	}

	// 移动到最终路径
	if err := os.Rename(tempPath, finalPath); err != nil {
		// 跨卷可能失败，尝试复制
		if copyErr := copyFileForUpdate(tempPath, finalPath); copyErr != nil {
			us.setDownloadError(fmt.Sprintf("failed to move file: %v", err))
			return
		}
		os.Remove(tempPath)
	}

	// 如果是 macOS 的 zip 文件，解压
	if runtime.GOOS == "darwin" && strings.HasSuffix(finalPath, ".zip") {
		extractDir := filepath.Join(us.dataDir, "downloads", "extracted")
		if err := unzip(finalPath, extractDir); err != nil {
			us.setDownloadError(fmt.Sprintf("failed to extract zip: %v", err))
			return
		}
		// 查找 .app 目录
		entries, _ := os.ReadDir(extractDir)
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".app") {
				finalPath = filepath.Join(extractDir, entry.Name())
				break
			}
		}
	}

	// 更新状态
	us.mu.Lock()
	us.downloadState.TempFilePath = finalPath
	us.downloadState.DownloadedBytes = us.downloadedBytes
	us.state = StateReady
	us.mu.Unlock()

	// 清理下载状态文件
	stateFile := filepath.Join(us.dataDir, "download_state.json")
	os.Remove(stateFile)

	us.emitStateUnlocked() // P1: 改用 emitStateUnlocked（未持锁）
}

// setDownloadError 设置下载错误
func (us *UpdateService) setDownloadError(msg string) {
	us.mu.Lock()
	us.state = StateError
	us.lastError = msg
	us.errorOp = "download"
	us.mu.Unlock()
	us.emitStateUnlocked() // P1: 改用 emitStateUnlocked（未持锁）
}

// saveDownloadState 保存下载状态
func (us *UpdateService) saveDownloadState(path string, state *DownloadState) {
	data, _ := json.MarshalIndent(state, "", "  ")
	_ = os.WriteFile(path, data, 0644)
}

// launchUpdater 启动更新程序
func (us *UpdateService) launchUpdater(pending *PendingApply) error {
	switch runtime.GOOS {
	case "windows":
		return us.launchWindowsUpdater(pending)
	case "darwin":
		return us.launchMacOSUpdater(pending)
	case "linux":
		return us.launchLinuxUpdater(pending)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// launchWindowsUpdater Windows 更新器
func (us *UpdateService) launchWindowsUpdater(pending *PendingApply) error {
	if pending.Method == "installer" {
		// 安装版：直接运行 installer
		cmd := exec.Command(pending.FilePath, "/S") // NSIS 静默安装
		return cmd.Start()
	}

	// 便携版：使用 PowerShell 脚本
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	pid := os.Getpid()
	script := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$oldExe = '%s'
$newExe = '%s'
$pid = %d
$maxWait = 60

# 等待旧进程退出
$waited = 0
while ($waited -lt $maxWait) {
    try {
        $proc = Get-Process -Id $pid -ErrorAction SilentlyContinue
        if (-not $proc) { break }
    } catch { break }
    Start-Sleep -Milliseconds 500
    $waited += 0.5
}

if ($waited -ge $maxWait) {
    Write-Error "Timeout waiting for process to exit"
    exit 1
}

# 同卷 staging
$stagingPath = "$oldExe.new"
Copy-Item -Path $newExe -Destination $stagingPath -Force

# 验证复制成功
if (-not (Test-Path $stagingPath)) {
    Write-Error "Failed to copy new executable"
    exit 1
}

# 重命名交换（原子操作）
$backupPath = "$oldExe.old.exe"
$retries = 20
for ($i = 0; $i -lt $retries; $i++) {
    try {
        if (Test-Path $backupPath) { Remove-Item $backupPath -Force }
        Rename-Item -Path $oldExe -NewName (Split-Path $backupPath -Leaf) -Force
        Rename-Item -Path $stagingPath -NewName (Split-Path $oldExe -Leaf) -Force
        break
    } catch {
        if ($i -eq ($retries - 1)) {
            # 回滚
            if (Test-Path $backupPath) {
                Rename-Item -Path $backupPath -NewName (Split-Path $oldExe -Leaf) -Force -ErrorAction SilentlyContinue
            }
            throw
        }
        Start-Sleep -Milliseconds 100
    }
}

# 启动新版本
Start-Process -FilePath $oldExe -WorkingDirectory (Split-Path $oldExe)

# 清理（延迟）
Start-Sleep -Seconds 2
Remove-Item $backupPath -Force -ErrorAction SilentlyContinue
Remove-Item $newExe -Force -ErrorAction SilentlyContinue
`, exePath, pending.FilePath, pid)

	scriptPath := filepath.Join(us.dataDir, "update.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return err
	}

	cmd := exec.Command("powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Start()
}

// launchMacOSUpdater macOS 更新器
func (us *UpdateService) launchMacOSUpdater(pending *PendingApply) error {
	// 获取当前 .app 路径
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	// 从 exe 路径推断 .app 路径
	// 例如: /Applications/CodeSwitch.app/Contents/MacOS/CodeSwitch
	appPath := exePath
	if idx := strings.Index(exePath, ".app/"); idx != -1 {
		appPath = exePath[:idx+4]
	}

	pid := os.Getpid()
	script := fmt.Sprintf(`#!/bin/bash
set -e

OLD_APP="%s"
NEW_APP="%s"
PID=%d
MAX_WAIT=60

# 等待旧进程退出
waited=0
while [ $waited -lt $MAX_WAIT ]; do
    if ! kill -0 $PID 2>/dev/null; then
        break
    fi
    sleep 0.5
    waited=$((waited + 1))
done

if [ $waited -ge $MAX_WAIT ]; then
    echo "Timeout waiting for process to exit" >&2
    exit 1
fi

# 同目录 staging（确保同卷）
STAGING_PATH="${OLD_APP}.new"
ditto "$NEW_APP" "$STAGING_PATH"

# 移除 quarantine
xattr -dr com.apple.quarantine "$STAGING_PATH" 2>/dev/null || true

# 重命名交换
BACKUP_PATH="${OLD_APP}.old"
if [ -d "$BACKUP_PATH" ]; then
    rm -rf "$BACKUP_PATH"
fi
mv "$OLD_APP" "$BACKUP_PATH"
mv "$STAGING_PATH" "$OLD_APP"

# 启动新版本
open "$OLD_APP"

# 清理
sleep 2
rm -rf "$BACKUP_PATH"
rm -rf "$NEW_APP"
`, appPath, pending.FilePath, pid)

	scriptPath := filepath.Join(us.dataDir, "update.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}

	cmd := exec.Command("/bin/bash", scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Start()
}

// launchLinuxUpdater Linux 更新器
func (us *UpdateService) launchLinuxUpdater(pending *PendingApply) error {
	// AppImage 更新
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	pid := os.Getpid()
	script := fmt.Sprintf(`#!/bin/bash
set -e

OLD_APP="%s"
NEW_APP="%s"
PID=%d
MAX_WAIT=60

# 等待旧进程退出
waited=0
while [ $waited -lt $MAX_WAIT ]; do
    if ! kill -0 $PID 2>/dev/null; then
        break
    fi
    sleep 0.5
    waited=$((waited + 1))
done

if [ $waited -ge $MAX_WAIT ]; then
    echo "Timeout waiting for process to exit" >&2
    exit 1
fi

# 备份并替换
BACKUP_PATH="${OLD_APP}.old"
cp "$OLD_APP" "$BACKUP_PATH"
cp "$NEW_APP" "$OLD_APP"
chmod +x "$OLD_APP"

# 启动新版本
"$OLD_APP" &

# 清理
sleep 2
rm -f "$BACKUP_PATH"
rm -f "$NEW_APP"
`, exePath, pending.FilePath, pid)

	scriptPath := filepath.Join(us.dataDir, "update.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}

	cmd := exec.Command("/bin/bash", scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Start()
}

// checkPendingApply 启动时检查待应用的更新
func (us *UpdateService) checkPendingApply() {
	pendingPath := filepath.Join(us.dataDir, "pending_apply.json")
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		return // 无待应用更新
	}

	var pending PendingApply
	if err := json.Unmarshal(data, &pending); err != nil {
		os.Remove(pendingPath)
		return
	}

	// 检查是否更新成功
	if us.isNewerOrEqualVersion(pending.TargetVersion) {
		// 更新成功，清理
		os.Remove(pendingPath)
		os.Remove(pending.FilePath)

		// 清理下载目录
		downloadsDir := filepath.Join(us.dataDir, "downloads")
		os.RemoveAll(downloadsDir)
		return
	}

	// 更新未成功（可能用户取消了安装）
	// 如果下载文件还在且校验通过，恢复到 ready 状态
	if pending.FilePath != "" {
		if _, err := os.Stat(pending.FilePath); err == nil {
			if pending.FileSHA256 != "" {
				hash, _ := computeSHA256(pending.FilePath)
				if strings.EqualFold(hash, pending.FileSHA256) {
					us.state = StateReady
					us.downloadState = &DownloadState{
						TempFilePath:   pending.FilePath,
						ExpectedSHA256: pending.FileSHA256,
					}
					us.targetInfo = &UpdateInfo{
						Version: pending.TargetVersion,
					}
					return
				}
			}
		}
	}

	// 文件不存在或校验失败，清理并回到 idle
	os.Remove(pendingPath)
	os.Remove(pending.FilePath)
}

// ==================== 辅助方法 ====================

// detectPolicy 检测更新策略
func (us *UpdateService) detectPolicy() UpdatePolicy {
	us.mu.Lock()
	defer us.mu.Unlock()
	return us.detectPolicyLocked()
}

// detectPolicyLocked 检测更新策略（需持有锁）
func (us *UpdateService) detectPolicyLocked() UpdatePolicy {
	// 可以通过构建时注入 UpdatePolicy 变量来覆盖
	// 这里实现运行时检测

	exePath, err := os.Executable()
	if err != nil {
		return PolicyPortable // 默认便携版
	}

	// Windows: 检查是否在 Program Files
	if runtime.GOOS == "windows" {
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		exePathLower := strings.ToLower(exePath)

		if (programFiles != "" && strings.HasPrefix(exePathLower, strings.ToLower(programFiles))) ||
			(programFilesX86 != "" && strings.HasPrefix(exePathLower, strings.ToLower(programFilesX86))) {
			// 在 Program Files，但还需验证是否可写
			if !us.canWriteToDir(filepath.Dir(exePath)) {
				return PolicyInstaller
			}
		}
	}

	// 其他情况：检查目录是否可写
	if us.canWriteToDir(filepath.Dir(exePath)) {
		return PolicyPortable
	}

	return PolicyInstaller
}

// canWriteToDir 检查目录是否可写
func (us *UpdateService) canWriteToDir(dir string) bool {
	testFile := filepath.Join(dir, ".write-test-"+fmt.Sprintf("%d", time.Now().UnixNano()))

	// 尝试创建文件
	f, err := os.Create(testFile)
	if err != nil {
		return false
	}
	f.Close()

	// 尝试重命名（模拟替换操作）
	testFile2 := testFile + ".renamed"
	if err := os.Rename(testFile, testFile2); err != nil {
		os.Remove(testFile)
		return false
	}

	os.Remove(testFile2)
	return true
}

// isNewerVersion 检查是否是更新版本
func (us *UpdateService) isNewerVersion(version string) bool {
	return compareVersions(version, us.currentVersion) > 0
}

// isNewerOrEqualVersion 检查是否是更新或相同版本
func (us *UpdateService) isNewerOrEqualVersion(version string) bool {
	return compareVersions(us.currentVersion, version) >= 0
}

// compareVersions 比较两个语义化版本号
// 返回：1 如果 a > b，-1 如果 a < b，0 如果相等
func compareVersions(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	// 确保至少有 3 个部分
	for len(partsA) < 3 {
		partsA = append(partsA, "0")
	}
	for len(partsB) < 3 {
		partsB = append(partsB, "0")
	}

	for i := 0; i < 3; i++ {
		numA := parseVersionPart(partsA[i])
		numB := parseVersionPart(partsB[i])
		if numA > numB {
			return 1
		}
		if numA < numB {
			return -1
		}
	}
	return 0
}

// parseVersionPart 解析版本号部分为整数
func parseVersionPart(s string) int {
	// 处理预发布标识符（如 1.0.0-alpha）
	if idx := strings.Index(s, "-"); idx != -1 {
		s = s[:idx]
	}
	var num int
	fmt.Sscanf(s, "%d", &num)
	return num
}

// getPlatformKey 获取平台标识符
func (us *UpdateService) getPlatformKey() string {
	os := runtime.GOOS
	arch := runtime.GOARCH

	// 映射到 latest.json 的 key
	switch {
	case os == "windows" && arch == "amd64":
		// P2: 使用 cachedPolicy 避免无锁调用 detectPolicyLocked()
		if us.cachedPolicy == string(PolicyInstaller) {
			return "windows-x86_64-installer"
		}
		return "windows-x86_64"
	case os == "darwin" && arch == "arm64":
		return "darwin-aarch64"
	case os == "darwin" && arch == "amd64":
		return "darwin-x86_64"
	case os == "linux" && arch == "amd64":
		return "linux-x86_64"
	default:
		return fmt.Sprintf("%s-%s", os, arch)
	}
}

// getAssetName 获取资产文件名（用于 GitHub API fallback）
// version 参数应为 GitHub Release 的 tag_name，如 "v2.6.23"
func (us *UpdateService) getAssetName(version string) string {
	// 去除版本号前缀 v
	v := strings.TrimPrefix(version, "v")

	switch {
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		// P2: 使用 cachedPolicy 避免无锁调用 detectPolicyLocked()
		if us.cachedPolicy == string(PolicyInstaller) {
			return fmt.Sprintf("CodeSwitch-v%s-amd64-installer.exe", v)
		}
		return fmt.Sprintf("CodeSwitch-v%s.exe", v)
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return fmt.Sprintf("CodeSwitch-v%s-macos-arm64.zip", v)
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		return fmt.Sprintf("CodeSwitch-v%s-macos-amd64.zip", v)
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		return fmt.Sprintf("CodeSwitch-v%s.AppImage", v)
	default:
		return ""
	}
}

// emitState 发送状态事件（调用前必须持有锁）
// 使用异步发送避免死锁
func (us *UpdateService) emitState() {
	if us.app == nil {
		return
	}
	// 在持有锁时构建快照
	snapshot := us.getStateLocked()
	// 异步发送事件，避免在持有锁时阻塞
	go us.app.Event.Emit("update:state", snapshot)
}

// emitStateUnlocked 发送状态事件（调用前不持有锁）
func (us *UpdateService) emitStateUnlocked() {
	if us.app == nil {
		return
	}
	us.app.Event.Emit("update:state", us.GetState())
}

// getStateLocked 获取状态快照（调用前必须持有锁）
func (us *UpdateService) getStateLocked() *UpdateStateSnapshot {
	// 缓存 policy，避免在 detectPolicyLocked 中进行 I/O
	policy := "auto"
	if us.cachedPolicy != "" {
		policy = us.cachedPolicy
	}

	snapshot := &UpdateStateSnapshot{
		State:           us.state,
		CurrentVersion:  us.currentVersion,
		DownloadedBytes: us.downloadedBytes,
		TotalBytes:      us.totalBytes,
		Error:           us.lastError,
		ErrorOp:         us.errorOp,
		Policy:          policy,
	}

	if us.targetInfo != nil {
		snapshot.LatestVersion = us.targetInfo.Version
		snapshot.Notes = us.targetInfo.Notes
		snapshot.DownloadURL = us.targetInfo.DownloadURL
	}

	if us.totalBytes > 0 {
		snapshot.Progress = float64(us.downloadedBytes) / float64(us.totalBytes) * 100
	}

	return snapshot
}

// emitProgressThrottled 节流发送进度事件
func (us *UpdateService) emitProgressThrottled(downloaded, total int64) {
	if us.app == nil {
		return
	}

	us.mu.Lock()
	now := time.Now()
	percent := 0
	if total > 0 {
		percent = int(downloaded * 100 / total)
	}

	// 状态变更立即发送
	if us.state != us.lastEmitState {
		us.lastEmitTime = now
		us.lastEmitPercent = percent
		us.lastEmitState = us.state
		us.mu.Unlock()
		us.app.Event.Emit("update:progress", map[string]interface{}{
			"downloaded": downloaded,
			"total":      total,
			"percent":    percent,
		})
		return
	}

	// 节流检查
	if now.Sub(us.lastEmitTime) < progressThrottle {
		us.mu.Unlock()
		return
	}
	if percent == us.lastEmitPercent {
		us.mu.Unlock()
		return
	}

	us.lastEmitTime = now
	us.lastEmitPercent = percent
	us.mu.Unlock()

	us.app.Event.Emit("update:progress", map[string]interface{}{
		"downloaded": downloaded,
		"total":      total,
		"percent":    percent,
	})
}

// isURLAllowed 检查 URL 是否在白名单中
func isURLAllowed(url string) bool {
	for _, prefix := range allowedURLPrefixes {
		if strings.HasPrefix(url, prefix) {
			return true
		}
	}
	return false
}

// getUpdateDataDir 获取更新数据目录
func getUpdateDataDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".code-switch-update")
	}
	return filepath.Join(homeDir, ".code-switch", "update")
}

// computeSHA256 计算文件 SHA256
func computeSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFileForUpdate 复制文件（用于更新）
func copyFileForUpdate(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// unzip 解压 zip 文件
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	os.MkdirAll(dest, 0755)

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		// 防止 zip slip 攻击
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, f.Mode())
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}
