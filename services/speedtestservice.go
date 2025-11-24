package services

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultTimeoutSecs = 8
	maxTimeoutSecs     = 30
	minTimeoutSecs     = 2
)

// EndpointLatency 端点延迟测试结果
type EndpointLatency struct {
	URL     string  `json:"url"`              // 端点 URL
	Latency *uint64 `json:"latency"`          // 延迟（毫秒），nil 表示失败
	Status  *int    `json:"status,omitempty"` // HTTP 状态码
	Error   *string `json:"error,omitempty"`  // 错误信息
}

// SpeedTestService 测速服务
type SpeedTestService struct{}

// NewSpeedTestService 创建测速服务
func NewSpeedTestService() *SpeedTestService {
	return &SpeedTestService{}
}

// TestEndpoints 测试一组端点的响应延迟
// 使用并发请求，每个端点先进行一次热身请求，再测量第二次请求的延迟
func (s *SpeedTestService) TestEndpoints(urls []string, timeoutSecs *int) []EndpointLatency {
	if len(urls) == 0 {
		return []EndpointLatency{}
	}

	timeout := s.sanitizeTimeout(timeoutSecs)
	client := s.buildClient(timeout)

	// 并发测试所有端点
	results := make([]EndpointLatency, len(urls))
	var wg sync.WaitGroup

	for i, rawURL := range urls {
		wg.Add(1)
		go func(index int, urlStr string) {
			defer wg.Done()
			results[index] = s.testSingleEndpoint(client, urlStr)
		}(i, rawURL)
	}

	wg.Wait()
	return results
}

// testSingleEndpoint 测试单个端点
func (s *SpeedTestService) testSingleEndpoint(client *http.Client, rawURL string) EndpointLatency {
	trimmed := trimSpace(rawURL)
	if trimmed == "" {
		errMsg := "URL 不能为空"
		return EndpointLatency{
			URL:     rawURL,
			Latency: nil,
			Status:  nil,
			Error:   &errMsg,
		}
	}

	// 验证 URL
	parsedURL, err := url.Parse(trimmed)
	if err != nil {
		errMsg := fmt.Sprintf("URL 无效: %v", err)
		return EndpointLatency{
			URL:     trimmed,
			Latency: nil,
			Status:  nil,
			Error:   &errMsg,
		}
	}

	// 热身请求（忽略结果，用于建立连接）
	_, _ = s.makeRequest(client, parsedURL.String())

	// 第二次请求：测量延迟
	start := time.Now()
	resp, err := s.makeRequest(client, parsedURL.String())
	latency := uint64(time.Since(start).Milliseconds())

	if err != nil {
		errMsg := s.formatError(err)
		return EndpointLatency{
			URL:     trimmed,
			Latency: nil,
			Status:  nil,
			Error:   &errMsg,
		}
	}
	defer resp.Body.Close()

	statusCode := resp.StatusCode
	return EndpointLatency{
		URL:     trimmed,
		Latency: &latency,
		Status:  &statusCode,
		Error:   nil,
	}
}

// makeRequest 发送 HTTP GET 请求
func (s *SpeedTestService) makeRequest(client *http.Client, urlStr string) (*http.Response, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}

	// 设置 User-Agent
	req.Header.Set("User-Agent", "cc-r-speedtest/1.0")

	return client.Do(req)
}

// formatError 格式化错误信息
func (s *SpeedTestService) formatError(err error) string {
	// 检查是否是超时错误
	if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
		return "请求超时"
	}

	// 其他错误
	return fmt.Sprintf("请求失败: %v", err)
}

// buildClient 构建 HTTP 客户端
func (s *SpeedTestService) buildClient(timeoutSecs int) *http.Client {
	return &http.Client{
		Timeout: time.Duration(timeoutSecs) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 限制重定向次数为 5
			if len(via) >= 5 {
				return fmt.Errorf("重定向次数过多")
			}
			return nil
		},
	}
}

// sanitizeTimeout 规范化超时参数
func (s *SpeedTestService) sanitizeTimeout(timeoutSecs *int) int {
	if timeoutSecs == nil {
		return defaultTimeoutSecs
	}

	secs := *timeoutSecs
	if secs < minTimeoutSecs {
		return minTimeoutSecs
	}
	if secs > maxTimeoutSecs {
		return maxTimeoutSecs
	}
	return secs
}

// trimSpace 去除字符串首尾空格
func trimSpace(s string) string {
	start := 0
	end := len(s)

	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}
