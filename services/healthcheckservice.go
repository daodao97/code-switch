// services/healthcheckservice.go
// 可用性监控服务 - 健康检查核心引擎
// Author: Half open flowers

package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/daodao97/xgo/xdb"
)

// HealthStatus 健康状态常量
const (
	HealthStatusOperational     = "operational"       // 正常（响应 ≤6s）
	HealthStatusDegraded        = "degraded"          // 延迟（响应 >6s 但成功）
	HealthStatusFailed          = "failed"            // 故障（请求失败/超时）
	HealthStatusValidationError = "validation_failed" // 验证失败（回复内容异常）
)

// 默认配置常量
const (
	DefaultOperationalThresholdMs = 6000  // 默认正常阈值（毫秒）
	DefaultTimeoutMs              = 15000 // 默认超时（毫秒）
	DefaultPollIntervalSeconds    = 60    // 默认检测间隔（秒）
	DefaultFailureThreshold       = 2     // 默认拉黑阈值（连续失败次数）
	MaxConcurrentChecks           = 5     // 最大并发检测数
	MaxHistoryPerProvider         = 60    // 每个 Provider 最多保留历史数
)

// HealthCheckResult 健康检查结果
type HealthCheckResult struct {
	ID           int64     `json:"id"`
	ProviderID   int64     `json:"providerId"`
	ProviderName string    `json:"providerName"`
	Platform     string    `json:"platform"`
	Model        string    `json:"model,omitempty"`
	Endpoint     string    `json:"endpoint,omitempty"`
	Status       string    `json:"status"`       // operational/degraded/failed/validation_failed
	LatencyMs    int       `json:"latencyMs"`    // 响应延迟（毫秒）
	ErrorMessage string    `json:"errorMessage"` // 错误消息
	CheckedAt    time.Time `json:"checkedAt"`    // 检测时间
}

// HealthCheckHistory 健康检查历史（单个 Provider 的时间线）
type HealthCheckHistory struct {
	ProviderID   int64               `json:"providerId"`
	ProviderName string              `json:"providerName"`
	Platform     string              `json:"platform"`
	Items        []HealthCheckResult `json:"items"`        // 历史记录（最近 N 条）
	Latest       *HealthCheckResult  `json:"latest"`       // 最新一条
	Uptime       float64             `json:"uptime"`       // 可用率（%）
	AvgLatencyMs int                 `json:"avgLatencyMs"` // 平均延迟
}

// ProviderTimeline Provider 时间线（用于前端展示）
type ProviderTimeline struct {
	ProviderID                 int64                `json:"providerId"`
	ProviderName               string               `json:"providerName"`
	Platform                   string               `json:"platform"`
	AvailabilityMonitorEnabled bool                 `json:"availabilityMonitorEnabled"`
	ConnectivityAutoBlacklist  bool                 `json:"connectivityAutoBlacklist"`
	AvailabilityConfig         *AvailabilityConfig  `json:"availabilityConfig,omitempty"` // 高级配置
	Items                      []HealthCheckResult  `json:"items"`                        // 历史记录
	Latest                     *HealthCheckResult   `json:"latest"`                       // 最新一条
	Uptime                     float64              `json:"uptime"`                       // 可用率
	AvgLatencyMs               int                  `json:"avgLatencyMs"`                 // 平均延迟
}

// AvailabilityFailureCounter 可用性失败计数器（独立于真实请求）
type AvailabilityFailureCounter struct {
	Platform         string
	ProviderName     string
	ConsecutiveFails int       // 连续失败次数
	LastFailedAt     time.Time // 最后失败时间
}

// HealthCheckService 健康检查服务
type HealthCheckService struct {
	providerService  *ProviderService
	blacklistService *BlacklistService
	settingsService  *SettingsService

	mu            sync.RWMutex
	failCounters  map[string]*AvailabilityFailureCounter // key: platform:providerName
	latestResults map[string]map[int64]*HealthCheckResult // platform -> providerID -> result

	// 后台轮询
	running      bool
	stopChan     chan struct{}
	pollInterval time.Duration

	// HTTP 客户端（带连接池）
	client *http.Client
}

// NewHealthCheckService 创建健康检查服务
func NewHealthCheckService(
	providerService *ProviderService,
	blacklistService *BlacklistService,
	settingsService *SettingsService,
) *HealthCheckService {
	return &HealthCheckService{
		providerService:  providerService,
		blacklistService: blacklistService,
		settingsService:  settingsService,
		failCounters:     make(map[string]*AvailabilityFailureCounter),
		latestResults: map[string]map[int64]*HealthCheckResult{
			"claude": {},
			"codex":  {},
			"gemini": {},
		},
		pollInterval: time.Duration(DefaultPollIntervalSeconds) * time.Second,
		client: &http.Client{
			// 由每次请求的 context 控制超时，避免固定值截断自定义配置
			Timeout: 0,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  true,
				MaxIdleConnsPerHost: 5,
			},
		},
	}
}

// Start Wails 生命周期方法
func (hcs *HealthCheckService) Start() error {
	// 初始化数据库表
	if err := hcs.ensureTable(); err != nil {
		return fmt.Errorf("初始化健康检查表失败: %w", err)
	}
	return nil
}

// Stop Wails 生命周期方法
func (hcs *HealthCheckService) Stop() error {
	hcs.StopBackgroundPolling()
	return nil
}

// ensureTable 确保健康检查历史表存在
func (hcs *HealthCheckService) ensureTable() error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	const createTableSQL = `CREATE TABLE IF NOT EXISTS health_check_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		provider_id INTEGER NOT NULL,
		provider_name TEXT NOT NULL,
		platform TEXT NOT NULL,
		model TEXT,
		endpoint TEXT,
		status TEXT NOT NULL,
		latency_ms INTEGER,
		error_message TEXT,
		checked_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	if _, err := db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("创建 health_check_history 表失败: %w", err)
	}

	// 创建索引
	const createIndexSQL = `
		CREATE INDEX IF NOT EXISTS idx_health_provider ON health_check_history(platform, provider_name);
		CREATE INDEX IF NOT EXISTS idx_health_checked_at ON health_check_history(checked_at);
	`
	if _, err := db.Exec(createIndexSQL); err != nil {
		log.Printf("[HealthCheck] 创建索引警告: %v", err)
	}

	return nil
}

// GetLatestResults 获取所有 Provider 的最新状态（按平台分组）
// 优化：使用批量查询避免 N+1 查询问题
func (hcs *HealthCheckService) GetLatestResults() (map[string][]ProviderTimeline, error) {
	results := make(map[string][]ProviderTimeline)

	// 遍历所有平台
	for _, platform := range []string{"claude", "codex"} {
		providers, err := hcs.providerService.LoadProviders(platform)
		if err != nil {
			log.Printf("[HealthCheck] 加载 %s 供应商失败: %v", platform, err)
			continue
		}

		// 批量查询该平台的所有历史记录
		historiesMap, err := hcs.batchGetHistories(platform)
		if err != nil {
			log.Printf("[HealthCheck] 批量查询 %s 历史记录失败: %v", platform, err)
		}

		// 组装结果
		var timelines []ProviderTimeline
		for _, p := range providers {
			timeline := ProviderTimeline{
				ProviderID:                 p.ID,
				ProviderName:               p.Name,
				Platform:                   platform,
				AvailabilityMonitorEnabled: p.AvailabilityMonitorEnabled,
				ConnectivityAutoBlacklist:  p.ConnectivityAutoBlacklist,
				AvailabilityConfig:         p.AvailabilityConfig,
			}

			// 从批量查询结果中获取该 provider 的历史记录
			if history, ok := historiesMap[p.Name]; ok {
				timeline.Items = history.Items
				timeline.Latest = history.Latest
				timeline.Uptime = history.Uptime
				timeline.AvgLatencyMs = history.AvgLatencyMs
			}

			timelines = append(timelines, timeline)
		}

		results[platform] = timelines
	}

	return results, nil
}

// batchGetHistories 批量获取某平台所有 Provider 的历史记录（避免 N+1 查询）
func (hcs *HealthCheckService) batchGetHistories(platform string) (map[string]*HealthCheckHistory, error) {
	db, err := xdb.DB("default")
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 批量查询：按平台一次性拉取所有记录，按 checked_at 倒序排列
	// 限制最多 5000 条记录，避免全表扫描
	query := `
		SELECT id, provider_id, provider_name, platform, model, endpoint, status, latency_ms, error_message, checked_at
		FROM health_check_history
		WHERE platform = ?
		ORDER BY checked_at DESC
		LIMIT 5000
	`

	rows, err := db.Query(query, platform)
	if err != nil {
		return nil, fmt.Errorf("批量查询历史记录失败: %w", err)
	}
	defer rows.Close()

	// 分组收集：按 provider_name 分组，每个 provider 最多保留 MaxHistoryPerProvider 条
	historiesMap := make(map[string]*HealthCheckHistory)

	for rows.Next() {
		var r HealthCheckResult
		var model, endpoint, errorMsg sql.NullString
		var latencyMs sql.NullInt64

		if err := rows.Scan(
			&r.ID, &r.ProviderID, &r.ProviderName, &r.Platform,
			&model, &endpoint, &r.Status, &latencyMs, &errorMsg, &r.CheckedAt,
		); err != nil {
			log.Printf("[HealthCheck] 解析历史记录失败: %v", err)
			continue
		}

		if model.Valid {
			r.Model = model.String
		}
		if endpoint.Valid {
			r.Endpoint = endpoint.String
		}
		if latencyMs.Valid {
			r.LatencyMs = int(latencyMs.Int64)
		}
		if errorMsg.Valid {
			r.ErrorMessage = errorMsg.String
		}

		// 获取或创建该 provider 的 history
		history, ok := historiesMap[r.ProviderName]
		if !ok {
			history = &HealthCheckHistory{
				ProviderID:   r.ProviderID,
				ProviderName: r.ProviderName,
				Platform:     platform,
				Items:        make([]HealthCheckResult, 0, MaxHistoryPerProvider),
			}
			historiesMap[r.ProviderName] = history
		}

		// 限制每个 provider 最多保留 MaxHistoryPerProvider 条
		if len(history.Items) < MaxHistoryPerProvider {
			history.Items = append(history.Items, r)
		}
	}

	// 计算每个 provider 的 Uptime 和 AvgLatency
	for _, history := range historiesMap {
		if len(history.Items) == 0 {
			continue
		}

		var totalLatency int64
		var successCount int

		for _, item := range history.Items {
			if item.Status == HealthStatusOperational || item.Status == HealthStatusDegraded {
				successCount++
				totalLatency += int64(item.LatencyMs)
			}
		}

		history.Uptime = float64(successCount) / float64(len(history.Items)) * 100
		if successCount > 0 {
			history.AvgLatencyMs = int(totalLatency / int64(successCount))
		}
		history.Latest = &history.Items[0]
	}

	return historiesMap, nil
}

// GetHistory 获取单个 Provider 的历史记录
func (hcs *HealthCheckService) GetHistory(platform, providerName string, limit int) (*HealthCheckHistory, error) {
	db, err := xdb.DB("default")
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	if limit <= 0 {
		limit = MaxHistoryPerProvider
	}

	query := `
		SELECT id, provider_id, provider_name, platform, model, endpoint, status, latency_ms, error_message, checked_at
		FROM health_check_history
		WHERE platform = ? AND provider_name = ?
		ORDER BY checked_at DESC
		LIMIT ?
	`

	rows, err := db.Query(query, platform, providerName, limit)
	if err != nil {
		return nil, fmt.Errorf("查询历史记录失败: %w", err)
	}
	defer rows.Close()

	history := &HealthCheckHistory{
		ProviderName: providerName,
		Platform:     platform,
		Items:        make([]HealthCheckResult, 0),
	}

	var totalLatency int64
	var successCount int

	for rows.Next() {
		var r HealthCheckResult
		var model, endpoint, errorMsg sql.NullString
		var latencyMs sql.NullInt64

		if err := rows.Scan(
			&r.ID, &r.ProviderID, &r.ProviderName, &r.Platform,
			&model, &endpoint, &r.Status, &latencyMs, &errorMsg, &r.CheckedAt,
		); err != nil {
			continue
		}

		if model.Valid {
			r.Model = model.String
		}
		if endpoint.Valid {
			r.Endpoint = endpoint.String
		}
		if latencyMs.Valid {
			r.LatencyMs = int(latencyMs.Int64)
		}
		if errorMsg.Valid {
			r.ErrorMessage = errorMsg.String
		}

		history.Items = append(history.Items, r)
		history.ProviderID = r.ProviderID

		// 统计
		if r.Status == HealthStatusOperational || r.Status == HealthStatusDegraded {
			successCount++
			totalLatency += int64(r.LatencyMs)
		}
	}

	// 计算可用率和平均延迟
	if len(history.Items) > 0 {
		history.Uptime = float64(successCount) / float64(len(history.Items)) * 100
		if successCount > 0 {
			history.AvgLatencyMs = int(totalLatency / int64(successCount))
		}
		history.Latest = &history.Items[0]
	}

	return history, nil
}

// RunSingleCheck 手动触发单个 Provider 检测
func (hcs *HealthCheckService) RunSingleCheck(platform string, providerID int64) (*HealthCheckResult, error) {
	providers, err := hcs.providerService.LoadProviders(platform)
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

	// 执行检测（使用 Provider 配置的有效超时）
	timeout := hcs.getEffectiveTimeout(targetProvider)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Millisecond)
	defer cancel()

	result := hcs.checkProvider(ctx, *targetProvider, platform)

	// 保存结果
	if err := hcs.saveResult(result); err != nil {
		log.Printf("[HealthCheck] 保存结果失败: %v", err)
	}

	// 更新缓存
	hcs.updateCache(result)

	// 处理拉黑联动
	hcs.handleBlacklistIntegration(targetProvider, result)

	return result, nil
}

// RunAllChecks 手动触发全部检测
func (hcs *HealthCheckService) RunAllChecks() (map[string][]HealthCheckResult, error) {
	results := make(map[string][]HealthCheckResult)

	for _, platform := range []string{"claude", "codex"} {
		platformResults := hcs.checkAllProviders(platform)
		results[platform] = platformResults
	}

	return results, nil
}

// checkAllProviders 检测指定平台的所有启用监控的供应商
func (hcs *HealthCheckService) checkAllProviders(platform string) []HealthCheckResult {
	providers, err := hcs.providerService.LoadProviders(platform)
	if err != nil {
		log.Printf("[HealthCheck] 加载 %s 供应商失败: %v", platform, err)
		return nil
	}

	var results []HealthCheckResult
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, MaxConcurrentChecks)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, provider := range providers {
		// 只检测启用了可用性监控的供应商
		if !provider.AvailabilityMonitorEnabled {
			continue
		}

		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := hcs.checkProvider(ctx, p, platform)

			// 保存结果
			if err := hcs.saveResult(result); err != nil {
				log.Printf("[HealthCheck] 保存结果失败: %v", err)
			}

			// 更新缓存
			hcs.updateCache(result)

			// 处理拉黑联动
			hcs.handleBlacklistIntegration(&p, result)

			mu.Lock()
			results = append(results, *result)
			mu.Unlock()

			log.Printf("[HealthCheck] %s/%s: status=%s, latency=%dms",
				platform, p.Name, result.Status, result.LatencyMs)
		}(provider)
	}

	wg.Wait()
	return results
}

// checkProvider 执行单个 Provider 的健康检查
func (hcs *HealthCheckService) checkProvider(ctx context.Context, provider Provider, platform string) *HealthCheckResult {
	result := &HealthCheckResult{
		ProviderID:   provider.ID,
		ProviderName: provider.Name,
		Platform:     platform,
		Status:       HealthStatusFailed,
		CheckedAt:    time.Now(),
	}

	// 获取有效的测试参数
	model := hcs.getEffectiveModel(&provider, platform)
	endpoint := hcs.getEffectiveEndpoint(&provider, platform)
	timeout := hcs.getEffectiveTimeout(&provider)

	result.Model = model
	result.Endpoint = endpoint

	// 构建请求体
	reqBody := hcs.buildTestRequest(platform, model)
	if reqBody == nil {
		result.ErrorMessage = "无法构建测试请求"
		return result
	}

	// 构建目标 URL
	baseURL := strings.TrimSuffix(provider.APIURL, "/")
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	targetURL := baseURL + endpoint

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(reqBody))
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("创建请求失败: %v", err)
		return result
	}

	// 设置 Headers
	req.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		// 根据认证方式设置请求头
		authTypeRaw := strings.TrimSpace(provider.ConnectivityAuthType)
		authType := strings.ToLower(authTypeRaw)
		if authType == "" {
			// 空值时使用平台默认（claude: x-api-key, codex: bearer）
			if strings.ToLower(platform) == "claude" {
				authType = "x-api-key"
			} else {
				authType = "bearer"
			}
		}
		switch authType {
		case "x-api-key":
			req.Header.Set("x-api-key", provider.APIKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+provider.APIKey)
		default:
			// 自定义 Header 名
			headerName := authTypeRaw
			if headerName == "" || strings.EqualFold(headerName, "custom") {
				headerName = "Authorization"
			}
			req.Header.Set(headerName, provider.APIKey)
		}
	}

	// 发送请求并计时
	start := time.Now()

	// 使用 per-request context 控制超时（复用服务级客户端）
	reqCtx, cancelReq := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancelReq()
	req = req.WithContext(reqCtx)

	resp, err := hcs.client.Do(req)
	latencyMs := int(time.Since(start).Milliseconds())
	result.LatencyMs = latencyMs

	if err != nil {
		// 检测是否为超时错误
		if isTimeoutError(err) {
			result.Status = HealthStatusFailed
			result.ErrorMessage = fmt.Sprintf("响应超时 (>%dms)", timeout)
			log.Printf("[HealthCheck] [%s/%s] 请求超时: %dms (阈值: %dms)",
				platform, provider.Name, latencyMs, timeout)
			return result
		}
		result.ErrorMessage = fmt.Sprintf("网络错误: %v", err)
		log.Printf("[HealthCheck] [%s/%s] 网络错误: %v", platform, provider.Name, err)
		return result
	}
	defer resp.Body.Close()

	// 读取响应体（限制大小）
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		body = []byte{}
	}

	// 判定状态
	result.Status, result.ErrorMessage = hcs.determineStatus(resp.StatusCode, latencyMs, body)

	return result
}

// determineStatus 根据 HTTP 状态码和延迟判定健康状态
func (hcs *HealthCheckService) determineStatus(statusCode, latencyMs int, body []byte) (string, string) {
	// 获取正常阈值（全局配置）
	operationalThresholdMs := DefaultOperationalThresholdMs
	if hcs.settingsService != nil {
		if threshold := hcs.settingsService.GetIntSetting("availability_operational_threshold_ms"); threshold > 0 {
			operationalThresholdMs = threshold
		}
	}

	// 2xx = 成功
	if statusCode >= 200 && statusCode < 300 {
		if latencyMs > operationalThresholdMs {
			return HealthStatusDegraded, fmt.Sprintf("响应成功但耗时 %dms", latencyMs)
		}
		return HealthStatusOperational, ""
	}

	// 特殊错误码
	switch statusCode {
	case 401, 403:
		return HealthStatusFailed, "认证失败"
	case 429:
		return HealthStatusFailed, "请求频率限制"
	case 400:
		return HealthStatusFailed, "请求无效"
	}

	// 5xx = 服务器错误
	if statusCode >= 500 {
		return HealthStatusFailed, fmt.Sprintf("服务器错误 (%d)", statusCode)
	}

	// 其他 4xx
	if statusCode >= 400 {
		return HealthStatusFailed, fmt.Sprintf("客户端错误 (%d)", statusCode)
	}

	return HealthStatusFailed, fmt.Sprintf("异常状态码 (%d)", statusCode)
}

// getEffectiveModel 获取有效的测试模型
func (hcs *HealthCheckService) getEffectiveModel(provider *Provider, platform string) string {
	// 优先使用用户配置
	if provider.AvailabilityConfig != nil && provider.AvailabilityConfig.TestModel != "" {
		return provider.AvailabilityConfig.TestModel
	}

	// 平台默认模型
	switch strings.ToLower(platform) {
	case "claude":
		return "claude-3-5-haiku-20241022"
	case "codex":
		return "gpt-4o-mini"
	case "gemini":
		return "gemini-1.5-flash"
	default:
		return "gpt-3.5-turbo"
	}
}

// getEffectiveEndpoint 获取有效的测试端点
func (hcs *HealthCheckService) getEffectiveEndpoint(provider *Provider, platform string) string {
	// 优先级 1：用户配置的健康检查专用端点
	if provider.AvailabilityConfig != nil && provider.AvailabilityConfig.TestEndpoint != "" {
		return provider.AvailabilityConfig.TestEndpoint
	}

	// 优先级 2：用户配置的生产端点（如果配置了 apiEndpoint）
	if provider.APIEndpoint != "" {
		return provider.GetEffectiveEndpoint("")
	}

	// 优先级 3：平台默认端点
	switch strings.ToLower(platform) {
	case "claude":
		return "/v1/messages"
	case "codex":
		return "/responses"
	default:
		return "/v1/chat/completions"
	}
}

// getEffectiveTimeout 获取有效的超时时间（毫秒）
func (hcs *HealthCheckService) getEffectiveTimeout(provider *Provider) int {
	// 优先使用用户配置
	if provider.AvailabilityConfig != nil && provider.AvailabilityConfig.Timeout > 0 {
		return provider.AvailabilityConfig.Timeout
	}
	return DefaultTimeoutMs
}

// buildTestRequest 构建测试请求体
func (hcs *HealthCheckService) buildTestRequest(platform, model string) []byte {
	// Anthropic 格式
	if platform == "claude" {
		reqBody := map[string]interface{}{
			"model":      model,
			"max_tokens": 1,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
		}
		data, _ := json.Marshal(reqBody)
		return data
	}

	// OpenAI/Codex 格式
	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	data, _ := json.Marshal(reqBody)
	return data
}

// saveResult 保存检测结果到数据库
func (hcs *HealthCheckService) saveResult(result *HealthCheckResult) error {
	if GlobalDBQueue == nil {
		return fmt.Errorf("数据库写入队列未初始化")
	}

	const insertSQL = `
		INSERT INTO health_check_history (provider_id, provider_name, platform, model, endpoint, status, latency_ms, error_message, checked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	return GlobalDBQueue.Exec(insertSQL,
		result.ProviderID,
		result.ProviderName,
		result.Platform,
		result.Model,
		result.Endpoint,
		result.Status,
		result.LatencyMs,
		result.ErrorMessage,
		result.CheckedAt,
	)
}

// updateCache 更新内存缓存
func (hcs *HealthCheckService) updateCache(result *HealthCheckResult) {
	hcs.mu.Lock()
	defer hcs.mu.Unlock()

	if hcs.latestResults[result.Platform] == nil {
		hcs.latestResults[result.Platform] = make(map[int64]*HealthCheckResult)
	}
	hcs.latestResults[result.Platform][result.ProviderID] = result
}

// handleBlacklistIntegration 处理与拉黑服务的联动
func (hcs *HealthCheckService) handleBlacklistIntegration(provider *Provider, result *HealthCheckResult) {
	// 未启用自动拉黑则跳过
	if !provider.ConnectivityAutoBlacklist {
		return
	}

	// 获取失败阈值（全局配置）
	failureThreshold := DefaultFailureThreshold
	if hcs.settingsService != nil {
		if threshold := hcs.settingsService.GetIntSetting("availability_failure_threshold"); threshold > 0 {
			failureThreshold = threshold
		}
	}

	// 获取或创建失败计数器
	counterKey := fmt.Sprintf("%s:%s", result.Platform, provider.Name)
	hcs.mu.Lock()
	counter, exists := hcs.failCounters[counterKey]
	if !exists {
		counter = &AvailabilityFailureCounter{
			Platform:     result.Platform,
			ProviderName: provider.Name,
		}
		hcs.failCounters[counterKey] = counter
	}

	// 在锁内更新计数器，避免并发竞态
	var shouldTriggerBlacklist bool
	var shouldRecordSuccess bool
	var prevFails int

	if result.Status == HealthStatusFailed {
		counter.ConsecutiveFails++
		counter.LastFailedAt = time.Now()
		prevFails = counter.ConsecutiveFails

		log.Printf("[HealthCheck] Provider %s 检测失败，连续失败: %d/%d",
			provider.Name, prevFails, failureThreshold)

		// 检查是否达到拉黑阈值
		if prevFails >= failureThreshold && hcs.blacklistService != nil {
			shouldTriggerBlacklist = true
		}
	} else if result.Status == HealthStatusOperational {
		// 成功，清零失败计数
		prevFails = counter.ConsecutiveFails
		counter.ConsecutiveFails = 0

		if prevFails > 0 {
			log.Printf("[HealthCheck] Provider %s 恢复正常，清零失败计数（之前: %d）",
				provider.Name, prevFails)
		}

		// 标记需要通知拉黑服务恢复
		if hcs.blacklistService != nil {
			shouldRecordSuccess = true
		}
	}
	hcs.mu.Unlock()

	// 在锁外执行耗时的 RPC 调用，避免阻塞其他检测
	if shouldTriggerBlacklist {
		if err := hcs.blacklistService.RecordFailure(result.Platform, provider.Name); err != nil {
			log.Printf("[HealthCheck] 触发拉黑失败: %v", err)
		} else {
			log.Printf("[HealthCheck] Provider %s 连续失败 %d 次，已触发拉黑！", provider.Name, failureThreshold)
		}
	}

	if shouldRecordSuccess {
		if err := hcs.blacklistService.RecordSuccess(result.Platform, provider.Name); err != nil {
			log.Printf("[HealthCheck] RecordSuccess 失败: %v", err)
		}
	}
	// degraded 状态不触发拉黑，也不清零计数
}

// StartBackgroundPolling 启动后台定时巡检
func (hcs *HealthCheckService) StartBackgroundPolling() {
	hcs.mu.Lock()
	defer hcs.mu.Unlock()

	if hcs.running {
		return
	}

	// 获取配置的轮询间隔
	pollIntervalSeconds := DefaultPollIntervalSeconds
	if hcs.settingsService != nil {
		if interval := hcs.settingsService.GetIntSetting("availability_poll_interval_seconds"); interval > 0 {
			pollIntervalSeconds = interval
		}
	}
	hcs.pollInterval = time.Duration(pollIntervalSeconds) * time.Second

	hcs.stopChan = make(chan struct{})
	hcs.running = true

	go func() {
		// 启动时延迟随机时间（0-10s），避免整点风暴
		jitter := time.Duration(rand.Intn(10000)) * time.Millisecond
		time.Sleep(jitter)

		// 立即执行一次
		hcs.runAllPlatformChecks()

		// 添加抖动（±10%）
		ticker := time.NewTicker(hcs.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				hcs.runAllPlatformChecks()
			case <-hcs.stopChan:
				log.Println("[HealthCheck] 后台巡检已停止")
				return
			}
		}
	}()

	log.Printf("[HealthCheck] 后台巡检已启动（间隔: %v）", hcs.pollInterval)
}

// StopBackgroundPolling 停止后台巡检
func (hcs *HealthCheckService) StopBackgroundPolling() {
	hcs.mu.Lock()
	defer hcs.mu.Unlock()

	if !hcs.running {
		return
	}

	close(hcs.stopChan)
	hcs.running = false
}

// IsPollingRunning 检查后台巡检是否运行中
func (hcs *HealthCheckService) IsPollingRunning() bool {
	hcs.mu.RLock()
	defer hcs.mu.RUnlock()
	return hcs.running
}

// SetAutoAvailabilityPolling 设置是否自动轮询（立即生效）
func (hcs *HealthCheckService) SetAutoAvailabilityPolling(enabled bool) {
	if enabled {
		// 启动轮询（StartBackgroundPolling 内部有锁）
		hcs.StartBackgroundPolling()
		log.Println("[HealthCheck] 已启用自动可用性监控")
	} else {
		// 停止轮询（StopBackgroundPolling 内部有锁）
		hcs.StopBackgroundPolling()
		log.Println("[HealthCheck] 已禁用自动可用性监控")
	}
}

// runAllPlatformChecks 执行所有平台的检测
func (hcs *HealthCheckService) runAllPlatformChecks() {
	platforms := []string{"claude", "codex"}
	for _, platform := range platforms {
		hcs.checkAllProviders(platform)
	}
}

// SetAvailabilityMonitorEnabled 启用/禁用指定 Provider 的可用性监控
func (hcs *HealthCheckService) SetAvailabilityMonitorEnabled(platform string, providerID int64, enabled bool) error {
	providers, err := hcs.providerService.LoadProviders(platform)
	if err != nil {
		return fmt.Errorf("加载供应商失败: %w", err)
	}

	found := false
	for i := range providers {
		if providers[i].ID == providerID {
			providers[i].AvailabilityMonitorEnabled = enabled
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("未找到供应商 ID: %d", providerID)
	}

	if err := hcs.providerService.SaveProviders(platform, providers); err != nil {
		return fmt.Errorf("保存供应商配置失败: %w", err)
	}

	log.Printf("[HealthCheck] Provider %d 可用性监控已%s", providerID, map[bool]string{true: "启用", false: "禁用"}[enabled])
	return nil
}

// SetConnectivityAutoBlacklist 启用/禁用指定 Provider 的连通性自动拉黑
func (hcs *HealthCheckService) SetConnectivityAutoBlacklist(platform string, providerID int64, enabled bool) error {
	providers, err := hcs.providerService.LoadProviders(platform)
	if err != nil {
		return fmt.Errorf("加载供应商失败: %w", err)
	}

	found := false
	for i := range providers {
		if providers[i].ID == providerID {
			// 前置条件检查：必须先启用可用性监控
			if enabled && !providers[i].AvailabilityMonitorEnabled {
				return fmt.Errorf("请先在可用性页面启用监控")
			}
			providers[i].ConnectivityAutoBlacklist = enabled
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("未找到供应商 ID: %d", providerID)
	}

	if err := hcs.providerService.SaveProviders(platform, providers); err != nil {
		return fmt.Errorf("保存供应商配置失败: %w", err)
	}

	log.Printf("[HealthCheck] Provider %d 自动拉黑已%s", providerID, map[bool]string{true: "启用", false: "禁用"}[enabled])
	return nil
}

// SaveAvailabilityConfig 保存 Provider 的可用性高级配置
func (hcs *HealthCheckService) SaveAvailabilityConfig(platform string, providerID int64, config *AvailabilityConfig) error {
	providers, err := hcs.providerService.LoadProviders(platform)
	if err != nil {
		return fmt.Errorf("加载供应商失败: %w", err)
	}

	found := false
	for i := range providers {
		if providers[i].ID == providerID {
			providers[i].AvailabilityConfig = config
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("未找到供应商 ID: %d", providerID)
	}

	if err := hcs.providerService.SaveProviders(platform, providers); err != nil {
		return fmt.Errorf("保存供应商配置失败: %w", err)
	}

	log.Printf("[HealthCheck] Provider %d 高级配置已保存", providerID)
	return nil
}

// CleanupOldRecords 清理过期的历史记录（保留最近 N 天）
func (hcs *HealthCheckService) CleanupOldRecords(daysToKeep int) (int64, error) {
	if daysToKeep <= 0 {
		daysToKeep = 7 // 默认保留 7 天
	}

	db, err := xdb.DB("default")
	if err != nil {
		return 0, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -daysToKeep)

	result, err := db.Exec(`DELETE FROM health_check_history WHERE checked_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("清理历史记录失败: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("[HealthCheck] 已清理 %d 条过期历史记录", rowsAffected)
	}

	return rowsAffected, nil
}
