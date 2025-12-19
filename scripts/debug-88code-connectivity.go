// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 模拟 connectivitytestservice.go 的逻辑，诊断问题
// Author: Half open flowers

func main() {
	// 用户配置（从截图/UI中获取）
	config := struct {
		APIURL   string
		APIKey   string
		Platform string
		Endpoint string
		AuthType string
		Model    string
	}{
		APIURL:   "https://m.88code.org/api",
		APIKey:   "88_784022b235595e84936fa42596b41bcad3dc24a79ced14a5d0d4489937ba36e8",
		Platform: "claude",
		Endpoint: "/v1/messages",        // 用户可能配置的端点
		AuthType: "x-api-key",           // 用户可能配置的认证方式
		Model:    "claude-haiku-4-5-20251001",
	}

	fmt.Println("==========================================================")
	fmt.Println("88code 连通性测试诊断脚本")
	fmt.Println("==========================================================")
	fmt.Printf("\n配置信息：\n")
	fmt.Printf("  APIURL:   %s\n", config.APIURL)
	fmt.Printf("  APIKey:   %s...%s\n", config.APIKey[:10], config.APIKey[len(config.APIKey)-4:])
	fmt.Printf("  Platform: %s\n", config.Platform)
	fmt.Printf("  Endpoint: %s\n", config.Endpoint)
	fmt.Printf("  AuthType: %s\n", config.AuthType)
	fmt.Printf("  Model:    %s\n", config.Model)

	client := &http.Client{Timeout: 15 * time.Second}

	// 测试 1: 完全模拟 connectivitytestservice.go 的行为
	fmt.Println("\n----------------------------------------------------------")
	fmt.Println("测试 1: 模拟 connectivitytestservice.go 的行为")
	fmt.Println("----------------------------------------------------------")
	testConnectivityServiceBehavior(client, config.APIURL, config.APIKey, config.Platform, config.Endpoint, config.AuthType, config.Model)

	// 测试 2: 强制使用 x-api-key（无论配置如何）
	fmt.Println("\n----------------------------------------------------------")
	fmt.Println("测试 2: 强制使用 x-api-key + anthropic-version")
	fmt.Println("----------------------------------------------------------")
	testForceXAPIKey(client, config.APIURL, config.APIKey, config.Model)

	// 测试 3: 测试不同的 authType 值处理
	fmt.Println("\n----------------------------------------------------------")
	fmt.Println("测试 3: 测试不同 authType 值的处理")
	fmt.Println("----------------------------------------------------------")
	authTypes := []string{"x-api-key", "X-API-KEY", "bearer", "Bearer", ""}
	for _, at := range authTypes {
		testAuthTypeVariant(client, config.APIURL, config.APIKey, config.Model, at, config.Platform)
	}

	// 测试 4: 检查 URL 拼接是否正确
	fmt.Println("\n----------------------------------------------------------")
	fmt.Println("测试 4: URL 拼接测试")
	fmt.Println("----------------------------------------------------------")
	urlVariants := []struct {
		baseURL  string
		endpoint string
	}{
		{"https://m.88code.org/api", "/v1/messages"},
		{"https://m.88code.org/api/", "/v1/messages"},
		{"https://m.88code.org/api", "v1/messages"},
		{"https://m.88code.org", "/api/v1/messages"},
	}
	for _, v := range urlVariants {
		testURLVariant(client, v.baseURL, v.endpoint, config.APIKey, config.Model)
	}
}

// 完全模拟 connectivitytestservice.go 的 TestProvider 逻辑
func testConnectivityServiceBehavior(client *http.Client, apiURL, apiKey, platform, endpoint, authType, model string) {
	// 模拟 buildTargetURL
	baseURL := strings.TrimSuffix(apiURL, "/")
	effectiveEndpoint := getEffectiveEndpoint(endpoint, platform)
	if !strings.HasPrefix(effectiveEndpoint, "/") {
		effectiveEndpoint = "/" + effectiveEndpoint
	}
	targetURL := baseURL + effectiveEndpoint

	// 模拟 buildTestRequest
	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Printf("  ❌ 创建请求失败: %v\n", err)
		return
	}

	// 模拟 getEffectiveAuthType
	effectiveAuthType := getEffectiveAuthType(authType, platform)

	// 设置 Headers（完全模拟 connectivitytestservice.go 第 126-136 行）
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		if effectiveAuthType == "x-api-key" {
			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	fmt.Printf("  目标 URL: %s\n", targetURL)
	fmt.Printf("  有效认证方式: %s\n", effectiveAuthType)
	fmt.Printf("  请求头:\n")
	for k, v := range req.Header {
		if k == "X-Api-Key" || k == "Authorization" {
			fmt.Printf("    %s: %s...%s\n", k, v[0][:15], v[0][len(v[0])-4:])
		} else {
			fmt.Printf("    %s: %s\n", k, v[0])
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		fmt.Printf("  ❌ 请求失败: %v (耗时: %v)\n", err, latency)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	fmt.Printf("  HTTP 状态码: %d (耗时: %v)\n", resp.StatusCode, latency.Round(time.Millisecond))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Printf("  ✅ 成功\n")
	} else {
		fmt.Printf("  ❌ 失败\n")
		fmt.Printf("  响应内容: %s\n", truncate(string(body), 300))
	}
}

func getEffectiveEndpoint(endpoint, platform string) string {
	ep := strings.TrimSpace(endpoint)
	if ep != "" {
		return ep
	}
	switch strings.ToLower(platform) {
	case "claude":
		return "/v1/messages"
	case "codex":
		return "/responses"
	default:
		return "/v1/chat/completions"
	}
}

func getEffectiveAuthType(authType, platform string) string {
	at := strings.ToLower(strings.TrimSpace(authType))
	if at != "" {
		return at
	}
	switch strings.ToLower(platform) {
	case "claude":
		return "x-api-key"
	case "codex":
		return "bearer"
	default:
		return "bearer"
	}
}

func testForceXAPIKey(client *http.Client, apiURL, apiKey, model string) {
	targetURL := strings.TrimSuffix(apiURL, "/") + "/v1/messages"

	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	fmt.Printf("  目标 URL: %s\n", targetURL)

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		fmt.Printf("  ❌ 请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Printf("  ✅ 成功 | HTTP %d | 耗时: %v\n", resp.StatusCode, latency.Round(time.Millisecond))
	} else {
		fmt.Printf("  ❌ 失败 | HTTP %d | 耗时: %v\n", resp.StatusCode, latency.Round(time.Millisecond))
		fmt.Printf("  响应: %s\n", truncate(string(body), 200))
	}
}

func testAuthTypeVariant(client *http.Client, apiURL, apiKey, model, authType, platform string) {
	effectiveAuthType := getEffectiveAuthType(authType, platform)
	fmt.Printf("  输入 authType=\"%s\" → 有效值=\"%s\"", authType, effectiveAuthType)

	targetURL := strings.TrimSuffix(apiURL, "/") + "/v1/messages"
	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	if effectiveAuthType == "x-api-key" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		fmt.Printf(" → ❌ 错误: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Printf(" → ✅ HTTP %d (%v)\n", resp.StatusCode, latency.Round(time.Millisecond))
	} else {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		fmt.Printf(" → ❌ HTTP %d | %s\n", resp.StatusCode, truncate(string(body), 80))
	}
}

func testURLVariant(client *http.Client, baseURL, endpoint, apiKey, model string) {
	base := strings.TrimSuffix(baseURL, "/")
	ep := endpoint
	if !strings.HasPrefix(ep, "/") {
		ep = "/" + ep
	}
	targetURL := base + ep

	fmt.Printf("  baseURL=\"%s\" + endpoint=\"%s\" → %s", baseURL, endpoint, targetURL)

	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		fmt.Printf(" → ❌ 错误: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Printf(" → ✅ HTTP %d (%v)\n", resp.StatusCode, latency.Round(time.Millisecond))
	} else {
		fmt.Printf(" → ❌ HTTP %d\n", resp.StatusCode)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
