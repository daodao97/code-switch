// +build ignore

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 深入测试 88code 的 headers 检查逻辑
// Author: Half open flowers

func main() {
	apiKey := "88_784022b235595e84936fa42596b41bcad3dc24a79ced14a5d0d4489937ba36e8"
	url := "https://m.88code.org/api/v1/messages"
	body := `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`

	fmt.Println("================================================================")
	fmt.Println("深入测试 88code Headers 检查逻辑")
	fmt.Println("================================================================")
	fmt.Println("88code 返回 '暂不支持非 claude code 请求' 说明它在检查请求来源")
	fmt.Println("providerrelay 转发时保留了 Claude Code 的原始 headers")
	fmt.Println("我们需要找出哪些 headers 是关键的")
	fmt.Println()

	tests := []struct {
		name    string
		headers map[string]string
	}{
		// 基础测试
		{
			name: "1. 基础 x-api-key",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
			},
		},
		// User-Agent 测试
		{
			name: "2. User-Agent: claude-code",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"User-Agent":        "claude-code/1.0.0",
			},
		},
		{
			name: "3. User-Agent: Claude Code CLI",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"User-Agent":        "Claude Code CLI/1.0",
			},
		},
		{
			name: "4. User-Agent: anthropic-typescript",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"User-Agent":        "anthropic-typescript/0.39.0",
			},
		},
		// Claude Code 可能使用的特殊 headers
		{
			name: "5. 添加 x-client-name: claude-code",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"x-client-name":     "claude-code",
			},
		},
		{
			name: "6. 添加 x-request-source: claude-code",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"x-request-source":  "claude-code",
			},
		},
		{
			name: "7. 添加 anthropic-beta header",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"anthropic-beta":    "messages-2024-01-01",
			},
		},
		// 模拟完整的 Claude Code headers
		{
			name: "8. 模拟完整 Claude Code headers",
			headers: map[string]string{
				"Content-Type":        "application/json",
				"x-api-key":           apiKey,
				"anthropic-version":   "2023-06-01",
				"User-Agent":          "claude-code/1.0.0",
				"Accept":              "application/json",
				"Accept-Encoding":     "gzip, deflate, br",
				"Connection":          "keep-alive",
			},
		},
		// 测试同时使用 x-api-key 和特殊 headers
		{
			name: "9. x-api-key + x-stainless-* headers (Anthropic SDK)",
			headers: map[string]string{
				"Content-Type":            "application/json",
				"x-api-key":               apiKey,
				"anthropic-version":       "2023-06-01",
				"x-stainless-lang":        "js",
				"x-stainless-package-version": "0.39.0",
				"x-stainless-os":          "Windows",
				"x-stainless-arch":        "x64",
				"x-stainless-runtime":     "node",
			},
		},
		// 测试 Origin/Referer
		{
			name: "10. 添加 Origin header",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"Origin":            "vscode-file://vscode-app",
			},
		},
		// 测试 88code 特有的可能 headers
		{
			name: "11. 添加 x-88code-client header",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"x-88code-client":   "claude-code",
			},
		},
	}

	client := &http.Client{Timeout: 30 * time.Second}

	for _, test := range tests {
		fmt.Printf("\n--- %s ---\n", test.name)

		req, _ := http.NewRequest("POST", url, bytes.NewBufferString(body))
		for k, v := range test.headers {
			req.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 网络错误: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		respStr := string(respBody)

		// 检查是否成功
		if resp.StatusCode == 200 && !contains(respStr, "error") {
			fmt.Printf("✅ HTTP %d (%v) - 可能成功!\n", resp.StatusCode, latency.Round(time.Millisecond))
		} else {
			fmt.Printf("❌ HTTP %d (%v)\n", resp.StatusCode, latency.Round(time.Millisecond))
		}

		// 显示响应片段
		if len(respStr) > 150 {
			respStr = respStr[:150] + "..."
		}
		fmt.Printf("   响应: %s\n", respStr)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
