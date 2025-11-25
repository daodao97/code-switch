package services

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	_ "modernc.org/sqlite"
)

type ProviderRelayService struct {
	providerService  *ProviderService
	geminiService    *GeminiService
	blacklistService *BlacklistService
	server           *http.Server
	addr             string
}

func NewProviderRelayService(providerService *ProviderService, geminiService *GeminiService, blacklistService *BlacklistService, addr string) *ProviderRelayService {
	if addr == "" {
		addr = ":18100"
	}

	home, _ := os.UserHomeDir()

	if err := xdb.Inits([]xdb.Config{
		{
			Name:   "default",
			Driver: "sqlite",
			DSN:    filepath.Join(home, ".code-switch", "app.db?cache=shared&mode=rwc"),
		},
	}); err != nil {
		fmt.Printf("初始化数据库失败: %v\n", err)
	} else {
		if err := ensureRequestLogTable(); err != nil {
			fmt.Printf("初始化 request_log 表失败: %v\n", err)
		}
		if err := ensureBlacklistTables(); err != nil {
			fmt.Printf("初始化黑名单表失败: %v\n", err)
		}
	}

	return &ProviderRelayService{
		providerService:  providerService,
		geminiService:    geminiService,
		blacklistService: blacklistService,
		addr:             addr,
	}
}

func (prs *ProviderRelayService) Start() error {
	// 启动前验证配置
	if warnings := prs.validateConfig(); len(warnings) > 0 {
		fmt.Println("======== Provider 配置验证警告 ========")
		for _, warn := range warnings {
			fmt.Printf("⚠️  %s\n", warn)
		}
		fmt.Println("========================================")
	}

	router := gin.Default()
	prs.registerRoutes(router)

	prs.server = &http.Server{
		Addr:    prs.addr,
		Handler: router,
	}

	fmt.Printf("provider relay server listening on %s\n", prs.addr)

	go func() {
		if err := prs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("provider relay server error: %v\n", err)
		}
	}()
	return nil
}

// validateConfig 验证所有 provider 的配置
// 返回警告列表（非阻塞性错误）
func (prs *ProviderRelayService) validateConfig() []string {
	warnings := make([]string, 0)

	for _, kind := range []string{"claude", "codex"} {
		providers, err := prs.providerService.LoadProviders(kind)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("[%s] 加载配置失败: %v", kind, err))
			continue
		}

		enabledCount := 0
		for _, p := range providers {
			if !p.Enabled {
				continue
			}
			enabledCount++

			// 验证每个启用的 provider
			if errs := p.ValidateConfiguration(); len(errs) > 0 {
				for _, errMsg := range errs {
					warnings = append(warnings, fmt.Sprintf("[%s/%s] %s", kind, p.Name, errMsg))
				}
			}

			// 检查是否配置了模型白名单或映射
			if (p.SupportedModels == nil || len(p.SupportedModels) == 0) &&
				(p.ModelMapping == nil || len(p.ModelMapping) == 0) {
				warnings = append(warnings, fmt.Sprintf(
					"[%s/%s] 未配置 supportedModels 或 modelMapping，将假设支持所有模型（可能导致降级失败）",
					kind, p.Name))
			}
		}

		if enabledCount == 0 {
			warnings = append(warnings, fmt.Sprintf("[%s] 没有启用的 provider", kind))
		}
	}

	return warnings
}

func (prs *ProviderRelayService) Stop() error {
	if prs.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return prs.server.Shutdown(ctx)
}

func (prs *ProviderRelayService) Addr() string {
	return prs.addr
}

func (prs *ProviderRelayService) registerRoutes(router gin.IRouter) {
	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))
	router.POST("/responses", prs.proxyHandler("codex", "/responses"))

	// Gemini API 端点（使用专门的路径前缀避免与 Claude 冲突）
	router.POST("/gemini/v1beta/*any", prs.geminiProxyHandler("/v1beta"))
	router.POST("/gemini/v1/*any", prs.geminiProxyHandler("/v1"))
}

func (prs *ProviderRelayService) proxyHandler(kind string, endpoint string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var bodyBytes []byte
		if c.Request.Body != nil {
			data, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
				return
			}
			bodyBytes = data
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		isStream := gjson.GetBytes(bodyBytes, "stream").Bool()
		requestedModel := gjson.GetBytes(bodyBytes, "model").String()

		// 如果未指定模型，记录警告但不拦截
		if requestedModel == "" {
			fmt.Printf("[WARN] 请求未指定模型名，无法执行模型智能降级\n")
		}

		providers, err := prs.providerService.LoadProviders(kind)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load providers"})
			return
		}

		active := make([]Provider, 0, len(providers))
		skippedCount := 0
		for _, provider := range providers {
			// 基础过滤：enabled、URL、APIKey
			if !provider.Enabled || provider.APIURL == "" || provider.APIKey == "" {
				continue
			}

			// 配置验证：失败则自动跳过
			if errs := provider.ValidateConfiguration(); len(errs) > 0 {
				fmt.Printf("[WARN] Provider %s 配置验证失败，已自动跳过: %v\n", provider.Name, errs)
				skippedCount++
				continue
			}

			// 核心过滤：只保留支持请求模型的 provider
			if requestedModel != "" && !provider.IsModelSupported(requestedModel) {
				fmt.Printf("[INFO] Provider %s 不支持模型 %s，已跳过\n", provider.Name, requestedModel)
				skippedCount++
				continue
			}

			// 黑名单检查：跳过已拉黑的 provider
			if isBlacklisted, until := prs.blacklistService.IsBlacklisted(kind, provider.Name); isBlacklisted {
				fmt.Printf("⛔ Provider %s 已拉黑，过期时间: %v\n", provider.Name, until.Format("15:04:05"))
				skippedCount++
				continue
			}

			active = append(active, provider)
		}

		if len(active) == 0 {
			if requestedModel != "" {
				c.JSON(http.StatusNotFound, gin.H{
					"error": fmt.Sprintf("没有可用的 provider 支持模型 '%s'（已跳过 %d 个不兼容的 provider）", requestedModel, skippedCount),
				})
			} else {
				c.JSON(http.StatusNotFound, gin.H{"error": "no providers available"})
			}
			return
		}

		fmt.Printf("[INFO] 找到 %d 个可用的 provider（已过滤 %d 个）：", len(active), skippedCount)
		for _, p := range active {
			fmt.Printf("%s ", p.Name)
		}
		fmt.Println()

		// 按 Level 分组
		levelGroups := make(map[int][]Provider)
		for _, provider := range active {
			level := provider.Level
			if level <= 0 {
				level = 1 // 未配置或零值时默认为 Level 1
			}
			levelGroups[level] = append(levelGroups[level], provider)
		}

		// 获取所有 level 并升序排序
		levels := make([]int, 0, len(levelGroups))
		for level := range levelGroups {
			levels = append(levels, level)
		}
		sort.Ints(levels)

		// 取第一个 Level 的第一个 provider（最高优先级）
		firstLevel := levels[0]
		firstProvider := levelGroups[firstLevel][0]

		fmt.Printf("[INFO] 选择 Provider: %s (Level %d) | 可用备选: %d 个 provider 分布在 %d 个 Level\n",
			firstProvider.Name, firstLevel, len(active), len(levels))

		query := flattenQuery(c.Request.URL.Query())
		clientHeaders := cloneHeaders(c.Request.Header)

		// 获取实际应该使用的模型名
		effectiveModel := firstProvider.GetEffectiveModel(requestedModel)

		// 如果需要映射，修改请求体
		currentBodyBytes := bodyBytes
		if effectiveModel != requestedModel && requestedModel != "" {
			fmt.Printf("[INFO] Provider %s 映射模型: %s -> %s\n", firstProvider.Name, requestedModel, effectiveModel)

			modifiedBody, err := ReplaceModelInRequestBody(bodyBytes, effectiveModel)
			if err != nil {
				fmt.Printf("[ERROR] 替换模型名失败: %v\n", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("模型映射失败: %v", err)})
				return
			}
			currentBodyBytes = modifiedBody
		}

		// 尝试发送请求
		startTime := time.Now()
		ok, err := prs.forwardRequest(c, kind, firstProvider, endpoint, query, clientHeaders, currentBodyBytes, isStream, effectiveModel)
		duration := time.Since(startTime)

		if ok {
			fmt.Printf("[INFO] ✓ 成功: %s (Level %d) | 耗时: %.2fs\n", firstProvider.Name, firstLevel, duration.Seconds())

			// 成功：清零连续失败计数
			if err := prs.blacklistService.RecordSuccess(kind, firstProvider.Name); err != nil {
				fmt.Printf("[WARN] 清零失败计数失败: %v\n", err)
			}

			return
		}

		// 失败：记录到黑名单并返回错误
		errorMsg := "未知错误"
		if err != nil {
			errorMsg = err.Error()
		}
		fmt.Printf("[ERROR] ✗ 失败: %s (Level %d) | 错误: %s | 耗时: %.2fs\n",
			firstProvider.Name, firstLevel, errorMsg, duration.Seconds())

		// 记录失败到黑名单系统
		if err := prs.blacklistService.RecordFailure(kind, firstProvider.Name); err != nil {
			fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
		}

		// 直接返回 502，不尝试其他 provider
		c.JSON(http.StatusBadGateway, gin.H{
			"error":    fmt.Sprintf("Provider %s 请求失败: %s", firstProvider.Name, errorMsg),
			"provider": firstProvider.Name,
			"duration": fmt.Sprintf("%.2fs", duration.Seconds()),
		})
	}
}

func (prs *ProviderRelayService) forwardRequest(
	c *gin.Context,
	kind string,
	provider Provider,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
	isStream bool,
	model string,
) (bool, error) {
	targetURL := joinURL(provider.APIURL, endpoint)
	headers := cloneMap(clientHeaders)
	headers["Authorization"] = fmt.Sprintf("Bearer %s", provider.APIKey)
	if _, ok := headers["Accept"]; !ok {
		headers["Accept"] = "application/json"
	}

	requestLog := &ReqeustLog{
		Platform: kind,
		Provider: provider.Name,
		Model:    model,
		IsStream: isStream,
	}
	start := time.Now()
	defer func() {
		requestLog.DurationSec = time.Since(start).Seconds()
		if _, err := xdb.New("request_log").Insert(xdb.Record{
			"platform":            requestLog.Platform,
			"model":               requestLog.Model,
			"provider":            requestLog.Provider,
			"http_code":           requestLog.HttpCode,
			"input_tokens":        requestLog.InputTokens,
			"output_tokens":       requestLog.OutputTokens,
			"cache_create_tokens": requestLog.CacheCreateTokens,
			"cache_read_tokens":   requestLog.CacheReadTokens,
			"reasoning_tokens":    requestLog.ReasoningTokens,
			"is_stream":           boolToInt(requestLog.IsStream),
			"duration_sec":        requestLog.DurationSec,
		}); err != nil {
			fmt.Printf("写入 request_log 失败: %v\n", err)
		}
	}()

	req := xrequest.New().
		SetHeaders(headers).
		SetQueryParams(query).
		SetRetry(1, 500*time.Millisecond).
		SetTimeout(3 * time.Hour) // 3小时超时，适配大型项目分析

	reqBody := bytes.NewReader(bodyBytes)
	req = req.SetBody(reqBody)

	resp, err := req.Post(targetURL)
	if err != nil {
		return false, err
	}

	if resp == nil {
		return false, fmt.Errorf("empty response")
	}

	// 先获取状态码，确保即使后续返回错误，也能记录正确的 HTTP 状态码
	status := resp.StatusCode()
	requestLog.HttpCode = status

	if resp.Error() != nil {
		return false, resp.Error()
	}

	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		_, copyErr := resp.ToHttpResponseWriter(c.Writer, ReqeustLogHook(c, kind, requestLog))
		return copyErr == nil, copyErr
	}

	return false, fmt.Errorf("upstream status %d", status)
}

func cloneHeaders(header http.Header) map[string]string {
	cloned := make(map[string]string, len(header))
	for key, values := range header {
		if len(values) > 0 {
			cloned[key] = values[len(values)-1]
		}
	}
	return cloned
}

func cloneMap(m map[string]string) map[string]string {
	cloned := make(map[string]string, len(m))
	for k, v := range m {
		cloned[k] = v
	}
	return cloned
}

func flattenQuery(values map[string][]string) map[string]string {
	query := make(map[string]string, len(values))
	for key, items := range values {
		if len(items) > 0 {
			query[key] = items[len(items)-1]
		}
	}
	return query
}

func joinURL(base string, endpoint string) string {
	base = strings.TrimSuffix(base, "/")
	endpoint = "/" + strings.TrimPrefix(endpoint, "/")
	return base + endpoint
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func ensureRequestLogColumn(db *sql.DB, column string, definition string) error {
	query := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('request_log') WHERE name = '%s'", column)
	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		alter := fmt.Sprintf("ALTER TABLE request_log ADD COLUMN %s %s", column, definition)
		if _, err := db.Exec(alter); err != nil {
			return err
		}
	}
	return nil
}

func ensureRequestLogTable() error {
	db, err := xdb.DB("default")
	if err != nil {
		return err
	}
	return ensureRequestLogTableWithDB(db)
}

func ensureRequestLogTableWithDB(db *sql.DB) error {
	const createTableSQL = `CREATE TABLE IF NOT EXISTS request_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT,
		model TEXT,
		provider TEXT,
		http_code INTEGER,
		input_tokens INTEGER,
		output_tokens INTEGER,
		cache_create_tokens INTEGER,
		cache_read_tokens INTEGER,
		reasoning_tokens INTEGER,
		is_stream INTEGER DEFAULT 0,
		duration_sec REAL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`

	if _, err := db.Exec(createTableSQL); err != nil {
		return err
	}

	if err := ensureRequestLogColumn(db, "created_at", "DATETIME DEFAULT CURRENT_TIMESTAMP"); err != nil {
		return err
	}
	if err := ensureRequestLogColumn(db, "is_stream", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureRequestLogColumn(db, "duration_sec", "REAL DEFAULT 0"); err != nil {
		return err
	}

	return nil
}

func ReqeustLogHook(c *gin.Context, kind string, usage *ReqeustLog) func(data []byte) (bool, []byte) { // SSE 钩子：累计字节和解析 token 用量
	return func(data []byte) (bool, []byte) {
		payload := strings.TrimSpace(string(data))

		parserFn := ClaudeCodeParseTokenUsageFromResponse
		if kind == "codex" {
			parserFn = CodexParseTokenUsageFromResponse
		}
		parseEventPayload(payload, parserFn, usage)

		return true, data
	}
}

func parseEventPayload(payload string, parser func(string, *ReqeustLog), usage *ReqeustLog) {
	lines := strings.Split(payload, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			parser(strings.TrimPrefix(line, "data: "), usage)
		}
	}
}

type ReqeustLog struct {
	ID                int64   `json:"id"`
	Platform          string  `json:"platform"` // claude code or codex
	Model             string  `json:"model"`
	Provider          string  `json:"provider"` // provider name
	HttpCode          int     `json:"http_code"`
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
	CacheCreateTokens int     `json:"cache_create_tokens"`
	CacheReadTokens   int     `json:"cache_read_tokens"`
	ReasoningTokens   int     `json:"reasoning_tokens"`
	IsStream          bool    `json:"is_stream"`
	DurationSec       float64 `json:"duration_sec"`
	CreatedAt         string  `json:"created_at"`
	InputCost         float64 `json:"input_cost"`
	OutputCost        float64 `json:"output_cost"`
	CacheCreateCost   float64 `json:"cache_create_cost"`
	CacheReadCost     float64 `json:"cache_read_cost"`
	Ephemeral5mCost   float64 `json:"ephemeral_5m_cost"`
	Ephemeral1hCost   float64 `json:"ephemeral_1h_cost"`
	TotalCost         float64 `json:"total_cost"`
	HasPricing        bool    `json:"has_pricing"`
}

// claude code usage parser
func ClaudeCodeParseTokenUsageFromResponse(data string, usage *ReqeustLog) {
	usage.InputTokens += int(gjson.Get(data, "message.usage.input_tokens").Int())
	usage.OutputTokens += int(gjson.Get(data, "message.usage.output_tokens").Int())
	usage.CacheCreateTokens += int(gjson.Get(data, "message.usage.cache_creation_input_tokens").Int())
	usage.CacheReadTokens += int(gjson.Get(data, "message.usage.cache_read_input_tokens").Int())

	usage.InputTokens += int(gjson.Get(data, "usage.input_tokens").Int())
	usage.OutputTokens += int(gjson.Get(data, "usage.output_tokens").Int())
}

// codex usage parser
func CodexParseTokenUsageFromResponse(data string, usage *ReqeustLog) {
	usage.InputTokens += int(gjson.Get(data, "response.usage.input_tokens").Int())
	usage.OutputTokens += int(gjson.Get(data, "response.usage.output_tokens").Int())
	usage.CacheReadTokens += int(gjson.Get(data, "response.usage.input_tokens_details.cached_tokens").Int())
	usage.ReasoningTokens += int(gjson.Get(data, "response.usage.output_tokens_details.reasoning_tokens").Int())
	fmt.Println("data ---->", data, fmt.Sprintf("%v", usage))
}

// ReplaceModelInRequestBody 替换请求体中的模型名
// 使用 gjson + sjson 实现高性能 JSON 操作，避免完整反序列化
func ReplaceModelInRequestBody(bodyBytes []byte, newModel string) ([]byte, error) {
	// 检查请求体中是否存在 model 字段
	result := gjson.GetBytes(bodyBytes, "model")
	if !result.Exists() {
		return bodyBytes, fmt.Errorf("请求体中未找到 model 字段")
	}

	// 使用 sjson.SetBytes 替换模型名（高性能操作）
	modified, err := sjson.SetBytes(bodyBytes, "model", newModel)
	if err != nil {
		return bodyBytes, fmt.Errorf("替换模型名失败: %w", err)
	}

	return modified, nil
}

// geminiProxyHandler 处理 Gemini API 请求
func (prs *ProviderRelayService) geminiProxyHandler(apiVersion string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取完整路径（例如 /v1beta/models/gemini-2.5-pro:generateContent）
		fullPath := c.Param("any") // 获取 *any 通配符匹配的部分
		endpoint := apiVersion + fullPath

		fmt.Printf("[Gemini] 收到请求: %s\n", endpoint)

		// 读取请求体
		var bodyBytes []byte
		if c.Request.Body != nil {
			data, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
				return
			}
			bodyBytes = data
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// 判断是否为流式请求
		isStream := strings.Contains(endpoint, ":streamGenerateContent")

		// 加载 Gemini providers
		providers := prs.geminiService.GetProviders()
		if len(providers) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "no gemini providers configured"})
			return
		}

		// 查找启用的 provider
		var activeProvider *GeminiProvider
		for i := range providers {
			if providers[i].Enabled && providers[i].BaseURL != "" {
				activeProvider = &providers[i]
				break
			}
		}

		if activeProvider == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "no active gemini provider"})
			return
		}

		fmt.Printf("[Gemini] 使用 Provider: %s | BaseURL: %s\n", activeProvider.Name, activeProvider.BaseURL)

		// 创建请求日志
		requestLog := &ReqeustLog{
			Provider:     activeProvider.Name,
			Platform:     "gemini",
			Model:        activeProvider.Model,
			IsStream:     isStream,
			InputTokens:  0,
			OutputTokens: 0,
		}

		// 记录开始时间并在函数结束时保存日志
		start := time.Now()
		defer func() {
			requestLog.DurationSec = time.Since(start).Seconds()
			if _, err := xdb.New("request_log").Insert(xdb.Record{
				"platform":            requestLog.Platform,
				"model":               requestLog.Model,
				"provider":            requestLog.Provider,
				"http_code":           requestLog.HttpCode,
				"input_tokens":        requestLog.InputTokens,
				"output_tokens":       requestLog.OutputTokens,
				"cache_create_tokens": requestLog.CacheCreateTokens,
				"cache_read_tokens":   requestLog.CacheReadTokens,
				"reasoning_tokens":    requestLog.ReasoningTokens,
				"is_stream":           boolToInt(requestLog.IsStream),
				"duration_sec":        requestLog.DurationSec,
			}); err != nil {
				fmt.Printf("[Gemini] 写入 request_log 失败: %v\n", err)
			}
		}()

		// 构建目标 URL
		targetURL := strings.TrimSuffix(activeProvider.BaseURL, "/") + endpoint
		fmt.Printf("[Gemini] 转发到: %s\n", targetURL)

		// 创建 HTTP 请求
		req, err := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
		if err != nil {
			requestLog.HttpCode = http.StatusInternalServerError
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("创建请求失败: %v", err)})
			return
		}

		// 复制请求头
		for key, values := range c.Request.Header {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}

		// 设置 API Key（如果有）
		if activeProvider.APIKey != "" {
			// Gemini API 使用 x-goog-api-key 头
			req.Header.Set("x-goog-api-key", activeProvider.APIKey)
		}

		// 发送请求
		client := &http.Client{Timeout: 300 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			requestLog.HttpCode = http.StatusBadGateway
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("请求失败: %v", err)})
			return
		}
		defer resp.Body.Close()

		requestLog.HttpCode = resp.StatusCode
		fmt.Printf("[Gemini] Provider %s 响应: %d | 耗时: %.2fs\n", activeProvider.Name, resp.StatusCode, time.Since(start).Seconds())

		// 如果不是成功响应，直接返回错误
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errorBody, _ := io.ReadAll(resp.Body)
			c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), errorBody)
			return
		}

		// 复制响应头
		for key, values := range resp.Header {
			for _, value := range values {
				c.Header(key, value)
			}
		}

		c.Status(resp.StatusCode)

		// 处理响应
		if isStream {
			// 流式响应 - 直接复制（暂不解析 token usage）
			c.Writer.Flush()
			if _, err := io.Copy(c.Writer, resp.Body); err != nil {
				fmt.Printf("[Gemini] 流式传输失败: %v\n", err)
			}
		} else {
			// 非流式响应 - 读取并返回
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "读取响应失败"})
				return
			}

			// TODO: 解析 Gemini 的 token usage from body
			// Gemini API 的 usage 格式可能在 body 中的 usageMetadata 字段

			c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
		}

		fmt.Printf("[Gemini] ✓ 请求完成 | Provider: %s | 耗时: %.2fs\n", activeProvider.Name, time.Since(start).Seconds())
	}
}
