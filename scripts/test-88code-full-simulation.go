// +build ignore

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 完整模拟 Claude Code 请求
// Author: Half open flowers

func main() {
	apiKey := "88_01f315e7e8ea55846f8088e713a9226f693551df66f535288e613934cb918f4a"
	url := "https://m.88code.org/api/v1/messages"
	body := `{"model":"claude-sonnet-4-20250514","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`

	fmt.Println("================================================================")
	fmt.Println("完整模拟 Claude Code 请求")
	fmt.Println("================================================================\n")

	client := &http.Client{Timeout: 30 * time.Second}

	tests := []struct {
		name    string
		headers map[string]string
	}{
		{
			name: "1. 最小化 Claude Code headers",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
			},
		},
		{
			name: "2. 添加 anthropic-beta",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"anthropic-beta":    "max-tokens-3-5-sonnet-2024-07-15",
			},
		},
		{
			name: "3. Claude Code User-Agent",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"User-Agent":        "claude-code/1.0.0",
			},
		},
		{
			name: "4. Anthropic SDK headers (完整)",
			headers: map[string]string{
				"Content-Type":                "application/json",
				"x-api-key":                   apiKey,
				"anthropic-version":           "2023-06-01",
				"User-Agent":                  "anthropic-typescript/0.39.0",
				"x-stainless-lang":            "js",
				"x-stainless-package-version": "0.39.0",
				"x-stainless-os":              "Windows",
				"x-stainless-arch":            "x64",
				"x-stainless-runtime":         "node",
				"x-stainless-runtime-version": "v20.18.0",
			},
		},
		{
			name: "5. 添加 Accept headers",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"Accept":            "text/event-stream",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
			},
		},
		{
			name: "6. 使用旧版 anthropic-version",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-01-01",
			},
		},
		{
			name: "7. 不带 anthropic-version",
			headers: map[string]string{
				"Content-Type": "application/json",
				"x-api-key":    apiKey,
			},
		},
		{
			name: "8. 模拟浏览器请求",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"User-Agent":        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
				"Accept":            "*/*",
				"Origin":            "vscode-file://vscode-app",
			},
		},
	}

	for _, test := range tests {
		fmt.Printf("--- %s ---\n", test.name)

		req, _ := http.NewRequest("POST", url, bytes.NewBufferString(body))
		for k, v := range test.headers {
			req.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 错误: %v\n\n", err)
			continue
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		respStr := string(respBody)

		isSuccess := resp.StatusCode == 200 &&
			(contains(respStr, `"content"`) || contains(respStr, `"text"`)) &&
			!contains(respStr, `"error"`)

		if isSuccess {
			fmt.Printf("✅ HTTP %d (%v) - 成功!\n", resp.StatusCode, latency.Round(time.Millisecond))
		} else {
			fmt.Printf("❌ HTTP %d (%v)\n", resp.StatusCode, latency.Round(time.Millisecond))
		}

		if len(respStr) > 150 {
			respStr = respStr[:150] + "..."
		}
		fmt.Printf("响应: %s\n\n", respStr)
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
