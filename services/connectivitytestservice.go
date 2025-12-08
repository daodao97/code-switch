package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ConnectivityStatus 连通性状态常量（与 relay-pulse 一致）
const (
	StatusAvailable   = 1  // 绿色：可用
	StatusDegraded    = 2  // 黄色：波动
	StatusUnavailable = 0  // 红色：不可用
	StatusMissing     = -1 // 灰色：无数据
)

// SubStatus 细分状态常量
const (
	SubStatusNone            = ""
	SubStatusSlowLatency     = "slow_latency"
	SubStatusRateLimit       = "rate_limit"
	SubStatusServerError     = "server_error"
	SubStatusClientError     = "client_error"
	SubStatusAuthError       = "auth_error"
	SubStatusInvalidRequest  = "invalid_request"
	SubStatusNetworkError    = "network_error"
	SubStatusContentMismatch = "content_mismatch"
)

// ConnectivityResult 连通性测试结果
type ConnectivityResult struct {
	ProviderID   int64     `json:"providerId"`
	ProviderName string    `json:"providerName"`
	Platform     string    `json:"platform"`
	Status       int       `json:"status"`
	SubStatus    string    `json:"subStatus"`
	LatencyMs    int       `json:"latencyMs"`
	LastChecked  time.Time `json:"lastChecked"`
	Message      string    `json:"message,omitempty"`
	HTTPCode     int       `json:"httpCode,omitempty"`
}

// ConnectivityTestService 连通性测试服务
type ConnectivityTestService struct {
	providerService  *ProviderService
	blacklistService *BlacklistService
	settingsService  *SettingsService

	mu      sync.RWMutex
	results map[string]map[int64]*ConnectivityResult // platform -> providerID -> result

	autoTestEnabled bool
	stopChan        chan struct{}
	running         bool

	client *http.Client
}

// NewConnectivityTestService 创建连通性测试服务
func NewConnectivityTestService(
	providerService *ProviderService,
	blacklistService *BlacklistService,
	settingsService *SettingsService,
) *ConnectivityTestService {
	return &ConnectivityTestService{
		providerService:  providerService,
		blacklistService: blacklistService,
		settingsService:  settingsService,
		results: map[string]map[int64]*ConnectivityResult{
			"claude": {},
			"codex":  {},
			"gemini": {},
		},
		autoTestEnabled: false,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  true,
				MaxIdleConnsPerHost: 5,
			},
		},
	}
}

// TestProvider 测试单个供应商连通性
func (cts *ConnectivityTestService) TestProvider(ctx context.Context, provider Provider, platform string) *ConnectivityResult {
	result := &ConnectivityResult{
		ProviderID:   provider.ID,
		ProviderName: provider.Name,
		Platform:     platform,
		Status:       StatusUnavailable,
		SubStatus:    SubStatusNone,
		LastChecked:  time.Now(),
	}

	// 构建测试请求
	reqBody, contentField := cts.buildTestRequest(platform, &provider)
	if reqBody == nil {
		result.Message = "无法构建测试请求"
		result.SubStatus = SubStatusClientError
		return result
	}

	// 根据平台拼接正确的端点路径（与 providerrelay.go 保持一致）
	targetURL := cts.buildTargetURL(provider.APIURL, platform)

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(reqBody))
	if err != nil {
		result.Message = fmt.Sprintf("创建请求失败: %v", err)
		result.SubStatus = SubStatusNetworkError
		return result
	}

	// 设置 Headers
	req.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		// Claude/Anthropic 使用 x-api-key，OpenAI 使用 Authorization: Bearer
		if strings.Contains(strings.ToLower(provider.APIURL), "anthropic") ||
			strings.Contains(strings.ToLower(platform), "claude") {
			req.Header.Set("x-api-key", provider.APIKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+provider.APIKey)
		}
	}

	// 发送请求并计时
	start := time.Now()
	resp, err := cts.client.Do(req)
	latencyMs := int(time.Since(start).Milliseconds())
	result.LatencyMs = latencyMs

	if err != nil {
		// 检测是否为超时错误 - 超时应视为"慢但可用"（黄色），而非"不可用"（红色）
		// 这样可以避免慢响应的 Provider 被误判为失败而拉黑
		if isTimeoutError(err) {
			result.Status = StatusDegraded
			result.SubStatus = SubStatusSlowLatency
			result.Message = fmt.Sprintf("响应超时 (>%ds)", int(cts.client.Timeout.Seconds()))
			return result
		}
		// 真正的网络错误（连接失败、DNS 解析失败等）
		result.Status = StatusUnavailable
		result.SubStatus = SubStatusNetworkError
		result.Message = cts.truncateMessage(fmt.Sprintf("网络错误: %v", err))
		return result
	}
	defer resp.Body.Close()

	result.HTTPCode = resp.StatusCode

	// 读取响应体
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		body = []byte{}
	}

	// 第一阶段：HTTP 状态码 + 延迟判定
	result.Status, result.SubStatus = cts.determineStatus(resp.StatusCode, latencyMs, 5000)

	// 第二阶段：内容校验（仅对成功响应）
	if result.Status != StatusUnavailable && contentField != "" {
		result.Status, result.SubStatus = cts.evaluateContent(result.Status, result.SubStatus, body, contentField)
	}

	// 设置错误消息
	if result.Status == StatusUnavailable {
		result.Message = cts.truncateMessage(string(body))
	}

	return result
}

// buildTestRequest 根据平台构建测试请求体
func (cts *ConnectivityTestService) buildTestRequest(platform string, provider *Provider) ([]byte, string) {
	var model string
	var contentField string

	// 确定模型名称
	switch strings.ToLower(platform) {
	case "claude":
		model = "claude-3-haiku-20240307"
		contentField = "content"
		// 如果 provider 配置了支持的模型，使用第一个
		if provider.SupportedModels != nil && len(provider.SupportedModels) > 0 {
			for m := range provider.SupportedModels {
				if provider.SupportedModels[m] {
					model = m
					break
				}
			}
		}
		// Claude/Anthropic 格式
		reqBody := map[string]interface{}{
			"model":      model,
			"max_tokens": 1,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
		}
		data, _ := json.Marshal(reqBody)
		return data, contentField

	case "codex":
		model = "gpt-4o-mini"
		contentField = "choices"
		if provider.SupportedModels != nil && len(provider.SupportedModels) > 0 {
			for m := range provider.SupportedModels {
				if provider.SupportedModels[m] {
					model = m
					break
				}
			}
		}
		// OpenAI 格式
		reqBody := map[string]interface{}{
			"model":      model,
			"max_tokens": 1,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
		}
		data, _ := json.Marshal(reqBody)
		return data, contentField

	case "gemini":
		contentField = "candidates"
		// Gemini 格式
		reqBody := map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"parts": []map[string]string{
						{"text": "hi"},
					},
				},
			},
		}
		data, _ := json.Marshal(reqBody)
		return data, contentField

	default:
		// 默认使用 OpenAI 格式
		model = "gpt-3.5-turbo"
		contentField = "choices"
		reqBody := map[string]interface{}{
			"model":      model,
			"max_tokens": 1,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
		}
		data, _ := json.Marshal(reqBody)
		return data, contentField
	}
}

// determineStatus 根据 HTTP 状态码和延迟判定状态
func (cts *ConnectivityTestService) determineStatus(statusCode, latencyMs, slowThresholdMs int) (int, string) {
	// 2xx = 成功
	if statusCode >= 200 && statusCode < 300 {
		if slowThresholdMs > 0 && latencyMs > slowThresholdMs {
			return StatusDegraded, SubStatusSlowLatency
		}
		return StatusAvailable, SubStatusNone
	}

	// 3xx = 重定向，视为正常
	if statusCode >= 300 && statusCode < 400 {
		return StatusAvailable, SubStatusNone
	}

	// 特殊 4xx
	if statusCode == 400 {
		return StatusUnavailable, SubStatusInvalidRequest
	}
	if statusCode == 401 || statusCode == 403 {
		return StatusUnavailable, SubStatusAuthError
	}
	if statusCode == 429 {
		return StatusUnavailable, SubStatusRateLimit
	}

	// 5xx = 服务器错误
	if statusCode >= 500 {
		return StatusUnavailable, SubStatusServerError
	}

	// 其他 4xx
	if statusCode >= 400 {
		return StatusUnavailable, SubStatusClientError
	}

	// 其他异常
	return StatusUnavailable, SubStatusClientError
}

// evaluateContent 内容校验
func (cts *ConnectivityTestService) evaluateContent(baseStatus int, subStatus string, body []byte, successContains string) (int, string) {
	if successContains == "" {
		return baseStatus, subStatus
	}

	if baseStatus == StatusUnavailable {
		return baseStatus, subStatus
	}

	if !strings.Contains(string(body), successContains) {
		return StatusUnavailable, SubStatusContentMismatch
	}

	return baseStatus, subStatus
}

// truncateMessage 截断消息（最多 512 字符）
func (cts *ConnectivityTestService) truncateMessage(msg string) string {
	if len(msg) > 512 {
		return msg[:512] + "..."
	}
	return msg
}

// buildTargetURL 根据平台拼接正确的 API 端点路径
// 与 providerrelay.go 中的转发路径保持一致
func (cts *ConnectivityTestService) buildTargetURL(baseURL, platform string) string {
	baseURL = strings.TrimSuffix(baseURL, "/")

	switch strings.ToLower(platform) {
	case "claude":
		return baseURL + "/v1/messages"
	case "codex":
		return baseURL + "/responses"
	case "gemini":
		// Gemini 使用不同的路径格式，暂不支持
		return baseURL
	default:
		// 默认使用 OpenAI 格式
		return baseURL + "/v1/chat/completions"
	}
}

// isTimeoutError 检测错误是否为超时类型
// 超时包括：context.DeadlineExceeded、net.Error.Timeout()、以及错误消息中包含 timeout 的情况
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	// 检查 context.DeadlineExceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// 检查 os.ErrDeadlineExceeded（Go 1.15+）
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}

	// 检查 net.Error 接口的 Timeout() 方法
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// 检查错误消息（兜底方案）
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "deadline exceeded") ||
		strings.Contains(errMsg, "context canceled")
}

// TestAll 测试指定平台的所有启用检测的供应商
func (cts *ConnectivityTestService) TestAll(platform string) []ConnectivityResult {
	providers, err := cts.providerService.LoadProviders(platform)
	if err != nil {
		log.Printf("[ConnectivityTest] 加载 %s 供应商失败: %v", platform, err)
		return nil
	}

	var results []ConnectivityResult
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 5) // 最多 5 个并发

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, provider := range providers {
		// 只测试启用了连通性检测的供应商
		if !provider.ConnectivityCheck {
			continue
		}

		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := cts.TestProvider(ctx, p, platform)

			// 保存结果
			cts.mu.Lock()
			if cts.results[platform] == nil {
				cts.results[platform] = make(map[int64]*ConnectivityResult)
			}
			cts.results[platform][p.ID] = result
			cts.mu.Unlock()

			// 与拉黑服务联动
			cts.handleBlacklistIntegration(platform, p.Name, result)

			mu.Lock()
			results = append(results, *result)
			mu.Unlock()

			log.Printf("[ConnectivityTest] %s/%s: status=%d, subStatus=%s, latency=%dms",
				platform, p.Name, result.Status, result.SubStatus, result.LatencyMs)
		}(provider)
	}

	wg.Wait()
	return results
}

// handleBlacklistIntegration 处理与拉黑服务的联动
func (cts *ConnectivityTestService) handleBlacklistIntegration(platform, providerName string, result *ConnectivityResult) {
	if cts.blacklistService == nil {
		return
	}

	switch result.Status {
	case StatusAvailable:
		// 绿色：调用 RecordSuccess 清零失败计数
		if err := cts.blacklistService.RecordSuccess(platform, providerName); err != nil {
			log.Printf("[ConnectivityTest] RecordSuccess 失败: %v", err)
		}
	case StatusUnavailable:
		// 红色：调用 RecordFailure 累计失败
		if err := cts.blacklistService.RecordFailure(platform, providerName); err != nil {
			log.Printf("[ConnectivityTest] RecordFailure 失败: %v", err)
		}
	case StatusDegraded:
		// 黄色：不操作，避免误判
	}
}

// GetResults 获取指定平台的测试结果
func (cts *ConnectivityTestService) GetResults(platform string) []ConnectivityResult {
	cts.mu.RLock()
	defer cts.mu.RUnlock()

	var results []ConnectivityResult
	if platformResults, ok := cts.results[platform]; ok {
		for _, r := range platformResults {
			results = append(results, *r)
		}
	}
	return results
}

// GetAllResults 获取所有平台的测试结果
func (cts *ConnectivityTestService) GetAllResults() map[string][]ConnectivityResult {
	cts.mu.RLock()
	defer cts.mu.RUnlock()

	allResults := make(map[string][]ConnectivityResult)
	for platform, platformResults := range cts.results {
		var results []ConnectivityResult
		for _, r := range platformResults {
			results = append(results, *r)
		}
		allResults[platform] = results
	}
	return allResults
}

// RunSingleTest 手动触发单个供应商测试
func (cts *ConnectivityTestService) RunSingleTest(platform string, providerID int64) (*ConnectivityResult, error) {
	providers, err := cts.providerService.LoadProviders(platform)
	if err != nil {
		return nil, fmt.Errorf("加载供应商失败: %w", err)
	}

	var targetProvider *Provider
	for i := range providers {
		if providers[i].ID == providerID {
			targetProvider = &providers[i]
			break
		}
	}

	if targetProvider == nil {
		return nil, fmt.Errorf("未找到供应商 ID: %d", providerID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := cts.TestProvider(ctx, *targetProvider, platform)

	// 保存结果
	cts.mu.Lock()
	if cts.results[platform] == nil {
		cts.results[platform] = make(map[int64]*ConnectivityResult)
	}
	cts.results[platform][providerID] = result
	cts.mu.Unlock()

	// 与拉黑服务联动
	cts.handleBlacklistIntegration(platform, targetProvider.Name, result)

	return result, nil
}

// SetAutoTestEnabled 设置自动测试开关
func (cts *ConnectivityTestService) SetAutoTestEnabled(enabled bool) error {
	cts.mu.Lock()
	defer cts.mu.Unlock()

	if enabled == cts.autoTestEnabled {
		return nil
	}

	cts.autoTestEnabled = enabled

	if enabled {
		cts.startAutoTest()
	} else {
		cts.stopAutoTest()
	}

	log.Printf("[ConnectivityTest] 自动测试已%s", map[bool]string{true: "开启", false: "关闭"}[enabled])
	return nil
}

// GetAutoTestEnabled 获取自动测试开关状态
func (cts *ConnectivityTestService) GetAutoTestEnabled() bool {
	cts.mu.RLock()
	defer cts.mu.RUnlock()
	return cts.autoTestEnabled
}

// startAutoTest 启动自动测试定时器
func (cts *ConnectivityTestService) startAutoTest() {
	if cts.running {
		return
	}

	cts.stopChan = make(chan struct{})
	cts.running = true

	go func() {
		// 启动时立即执行一次
		cts.runAllPlatformTests()

		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				cts.runAllPlatformTests()
			case <-cts.stopChan:
				log.Println("[ConnectivityTest] 自动测试定时器已停止")
				return
			}
		}
	}()

	log.Println("[ConnectivityTest] 自动测试定时器已启动（间隔: 1分钟）")
}

// stopAutoTest 停止自动测试定时器
func (cts *ConnectivityTestService) stopAutoTest() {
	if !cts.running {
		return
	}

	close(cts.stopChan)
	cts.running = false
}

// runAllPlatformTests 执行所有平台的测试
func (cts *ConnectivityTestService) runAllPlatformTests() {
	// 仅轮询 ProviderService 支持的平台，避免无意义的错误日志
	// Gemini 使用独立的 GeminiService，暂未接入
	platforms := []string{"claude", "codex"}
	for _, platform := range platforms {
		cts.TestAll(platform)
	}
}

// Wails 生命周期方法
func (cts *ConnectivityTestService) Start() error {
	return nil
}

func (cts *ConnectivityTestService) Stop() error {
	cts.mu.Lock()
	defer cts.mu.Unlock()

	if cts.running {
		close(cts.stopChan)
		cts.running = false
	}
	return nil
}
