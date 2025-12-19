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

// 完全模拟前端调用 TestProviderManual 的过程
// Author: Half open flowers

func main() {
	// 模拟前端传递的参数（直接从配置文件读取的值）
	platform := "claude"
	apiURL := "https://m.88code.org/api"
	apiKey := "88_784022b235595e84936fa42596b41bcad3dc24a79ced14a5d0d4489937ba36e8"
	model := ""                // 用户可能没有配置 connectivityTestModel
	endpoint := "/v1/messages" // 从配置文件读取
	authType := "x-api-key"    // 从配置文件读取

	fmt.Println("================================================================")
	fmt.Println("模拟前端调用 TestProviderManual")
	fmt.Println("================================================================")
	fmt.Printf("参数：\n")
	fmt.Printf("  platform: %q\n", platform)
	fmt.Printf("  apiURL:   %q\n", apiURL)
	fmt.Printf("  apiKey:   %q (len=%d)\n", apiKey[:15]+"...", len(apiKey))
	fmt.Printf("  model:    %q (空=使用默认值)\n", model)
	fmt.Printf("  endpoint: %q\n", endpoint)
	fmt.Printf("  authType: %q\n", authType)

	// ========== 模拟 TestProviderManual 逻辑 ==========

	// 1. 平台参数校验
	if platform == "" {
		platform = "claude"
	}

	// 2. 模拟 buildTargetURL
	baseURL := strings.TrimSuffix(apiURL, "/")
	effectiveEndpoint := getEffectiveEndpoint(endpoint, platform)
	if !strings.HasPrefix(effectiveEndpoint, "/") {
		effectiveEndpoint = "/" + effectiveEndpoint
	}
	targetURL := baseURL + effectiveEndpoint
	fmt.Printf("\n构建的 targetURL: %s\n", targetURL)

	// 3. 模拟 buildTestRequest
	effectiveModel := model
	if effectiveModel == "" {
		// 平台默认模型
		defaults := map[string]string{
			"claude": "claude-haiku-4-5-20251001",
			"codex":  "gpt-5.1",
			"gemini": "gemini-2.5-flash",
		}
		effectiveModel = defaults[strings.ToLower(platform)]
	}
	if effectiveModel == "" {
		effectiveModel = "gpt-3.5-turbo"
	}
	fmt.Printf("使用的模型: %s\n", effectiveModel)

	reqBody := map[string]interface{}{
		"model":      effectiveModel,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	fmt.Printf("请求体: %s\n", string(bodyBytes))

	// 4. 创建请求
	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Printf("❌ 创建请求失败: %v\n", err)
		return
	}

	// 5. 模拟 getEffectiveAuthType
	effectiveAuthType := getEffectiveAuthType(authType, platform)
	fmt.Printf("有效认证方式: %s\n", effectiveAuthType)

	// 6. 设置 Headers
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		if effectiveAuthType == "x-api-key" {
			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("anthropic-version", "2023-06-01")
			fmt.Println("设置 Header: x-api-key + anthropic-version")
		} else {
			req.Header.Set("Authorization", "Bearer "+apiKey)
			fmt.Println("设置 Header: Authorization: Bearer")
		}
	}

	// 打印所有请求头
	fmt.Println("\n所有请求头:")
	for k, v := range req.Header {
		if k == "X-Api-Key" || k == "Authorization" {
			fmt.Printf("  %s: %s...%s\n", k, v[0][:20], v[0][len(v[0])-4:])
		} else {
			fmt.Printf("  %s: %s\n", k, v[0])
		}
	}

	// 7. 发送请求
	client := &http.Client{Timeout: 15 * time.Second}
	fmt.Println("\n发送请求中...")
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		fmt.Printf("❌ 请求失败: %v (耗时: %v)\n", err, latency)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	fmt.Printf("\n================================================================\n")
	fmt.Printf("HTTP 状态码: %d (耗时: %v)\n", resp.StatusCode, latency.Round(time.Millisecond))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Println("✅ 测试成功!")
	} else {
		fmt.Println("❌ 测试失败!")
		fmt.Printf("响应内容: %s\n", string(body))
	}

	// ========== 额外测试：检查 API Key 是否有隐藏字符 ==========
	fmt.Println("\n================================================================")
	fmt.Println("API Key 隐藏字符检查")
	fmt.Println("================================================================")
	fmt.Printf("原始长度: %d\n", len(apiKey))
	fmt.Printf("Trim后长度: %d\n", len(strings.TrimSpace(apiKey)))
	if len(apiKey) != len(strings.TrimSpace(apiKey)) {
		fmt.Println("⚠️  警告: API Key 包含前后空白字符!")
	}
	// 检查零宽字符
	hasZeroWidth := false
	for _, r := range apiKey {
		if r == '\u200B' || r == '\u200C' || r == '\u200D' || r == '\uFEFF' {
			hasZeroWidth = true
			break
		}
	}
	if hasZeroWidth {
		fmt.Println("⚠️  警告: API Key 包含零宽字符!")
	}
	if !hasZeroWidth && len(apiKey) == len(strings.TrimSpace(apiKey)) {
		fmt.Println("✅ API Key 没有发现隐藏字符")
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
