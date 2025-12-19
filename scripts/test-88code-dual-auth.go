// +build ignore

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 测试 88code 的双认证模式（模拟 providerrelay 转发 Claude Code 请求）
// Author: Half open flowers

func main() {
	apiKey := "88_784022b235595e84936fa42596b41bcad3dc24a79ced14a5d0d4489937ba36e8"
	url := "https://m.88code.org/api/v1/messages"
	body := `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`

	fmt.Println("================================================================")
	fmt.Println("测试 88code 双认证模式")
	fmt.Println("================================================================")
	fmt.Println("providerrelay 会保留 Claude Code 的 headers，然后添加 Authorization: Bearer")
	fmt.Println("如果 Claude Code 发送了 x-api-key，最终请求会同时有两个认证 header")
	fmt.Println()

	tests := []struct {
		name    string
		headers map[string]string
	}{
		{
			name: "1. 只有 x-api-key",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
			},
		},
		{
			name: "2. 只有 Authorization: Bearer",
			headers: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer " + apiKey,
			},
		},
		{
			name: "3. 双认证：x-api-key + Authorization: Bearer (模拟 providerrelay)",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"Authorization":     "Bearer " + apiKey,
			},
		},
		{
			name: "4. 双认证 + Anthropic SDK headers",
			headers: map[string]string{
				"Content-Type":                "application/json",
				"x-api-key":                   apiKey,
				"anthropic-version":           "2023-06-01",
				"Authorization":               "Bearer " + apiKey,
				"User-Agent":                  "anthropic-typescript/0.39.0",
				"x-stainless-lang":            "js",
				"x-stainless-package-version": "0.39.0",
				"x-stainless-os":              "Windows",
				"x-stainless-arch":            "x64",
				"x-stainless-runtime":         "node",
				"x-stainless-runtime-version": "v20.0.0",
			},
		},
		{
			name: "5. 双认证 + Accept-Encoding (providerrelay 默认)",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"Authorization":     "Bearer " + apiKey,
				"Accept":            "application/json",
				"Accept-Encoding":   "gzip, deflate, br",
			},
		},
		{
			name: "6. 完整模拟 Claude Code + providerrelay",
			headers: map[string]string{
				"Content-Type":                "application/json",
				"x-api-key":                   apiKey,
				"anthropic-version":           "2023-06-01",
				"Authorization":               "Bearer " + apiKey,
				"Accept":                      "application/json",
				"Accept-Encoding":             "gzip",
				"User-Agent":                  "anthropic-typescript/0.39.0",
				"x-stainless-lang":            "js",
				"x-stainless-package-version": "0.39.0",
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

		// 检查是否成功（响应中包含 content 字段）
		isSuccess := resp.StatusCode == 200 &&
			(contains(respStr, `"content"`) || contains(respStr, `"text"`)) &&
			!contains(respStr, `"error"`)

		if isSuccess {
			fmt.Printf("✅ HTTP %d (%v) - 成功!\n", resp.StatusCode, latency.Round(time.Millisecond))
		} else {
			fmt.Printf("❌ HTTP %d (%v)\n", resp.StatusCode, latency.Round(time.Millisecond))
		}

		// 显示响应片段
		if len(respStr) > 200 {
			respStr = respStr[:200] + "..."
		}
		fmt.Printf("   响应: %s\n", respStr)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
