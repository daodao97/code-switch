// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 最终测试：精确模拟 providerrelay 的行为
// Author: Half open flowers

type ProviderConfig struct {
	Providers []struct {
		Name   string `json:"name"`
		APIURL string `json:"apiUrl"`
		APIKey string `json:"apiKey"`
	} `json:"providers"`
}

func main() {
	// 从用户配置文件读取 88code 配置
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".code-switch", "claude-code.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Printf("读取配置文件失败: %v\n", err)
		return
	}

	var config ProviderConfig
	if err := json.Unmarshal(data, &config); err != nil {
		fmt.Printf("解析配置文件失败: %v\n", err)
		return
	}

	// 找到 88code provider
	var apiKey, apiURL string
	for _, p := range config.Providers {
		if strings.Contains(strings.ToLower(p.Name), "88code") {
			apiKey = p.APIKey
			apiURL = p.APIURL
			break
		}
	}

	if apiKey == "" {
		fmt.Println("未找到 88code 供应商配置")
		return
	}

	fmt.Println("================================================================")
	fmt.Println("88code 最终测试")
	fmt.Println("================================================================")
	fmt.Printf("API URL: %s\n", apiURL)
	fmt.Printf("API Key: %s...%s (len=%d)\n", apiKey[:10], apiKey[len(apiKey)-4:], len(apiKey))
	fmt.Printf("API Key 原始字节: %v\n", []byte(apiKey[:20]))
	fmt.Println()

	url := strings.TrimSuffix(apiURL, "/") + "/v1/messages"
	body := `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`

	client := &http.Client{Timeout: 30 * time.Second}

	tests := []struct {
		name    string
		headers map[string]string
	}{
		{
			name: "1. 只有 x-api-key（连通性测试模式）",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
			},
		},
		{
			name: "2. 只有 Bearer（无 x-api-key）",
			headers: map[string]string{
				"Content-Type": "application/json",
				"Authorization": "Bearer " + apiKey,
			},
		},
		{
			name: "3. 双认证模拟 providerrelay（Claude Code headers + Bearer）",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"Authorization":     "Bearer " + apiKey,
				// Claude Code 可能发送的其他 headers
				"Accept":         "application/json",
				"Accept-Encoding": "gzip",
			},
		},
		{
			name: "4. 完整 Claude Code SDK headers",
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
			name: "5. Claude Code SDK + Bearer (最接近 providerrelay 实际行为)",
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
				"x-stainless-runtime-version": "v20.18.0",
				"Accept":                      "application/json",
			},
		},
		{
			name: "6. 去掉 x-api-key，只用 anthropic-version + Bearer",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"anthropic-version": "2023-06-01",
				"Authorization":     "Bearer " + apiKey,
			},
		},
	}

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
			fmt.Printf("网络错误: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		respStr := string(respBody)

		// 分析响应
		isSuccess := resp.StatusCode == 200 &&
			(strings.Contains(respStr, `"content"`) || strings.Contains(respStr, `"text"`)) &&
			!strings.Contains(respStr, `"error"`)

		if isSuccess {
			fmt.Printf("✅ HTTP %d (%v) - 成功!\n", resp.StatusCode, latency.Round(time.Millisecond))
		} else {
			fmt.Printf("❌ HTTP %d (%v)\n", resp.StatusCode, latency.Round(time.Millisecond))
		}

		// 显示完整响应
		if len(respStr) > 300 {
			respStr = respStr[:300] + "..."
		}
		fmt.Printf("   响应: %s\n", respStr)

		// 分析错误类型
		if strings.Contains(respStr, "API Key 配置错误") {
			fmt.Println("   分析: API Key 被认为无效")
		} else if strings.Contains(respStr, "暂不支持非 claude code") {
			fmt.Println("   分析: 88code 检测到非 Claude Code 请求")
		} else if strings.Contains(respStr, "content") {
			fmt.Println("   分析: 请求成功")
		}
	}

	fmt.Println("\n================================================================")
	fmt.Println("结论分析")
	fmt.Println("================================================================")
	fmt.Println("如果测试 1 返回 'API Key 配置错误':")
	fmt.Println("  → API Key 本身可能无效/过期，与 providerrelay 无关")
	fmt.Println()
	fmt.Println("如果测试 4 成功但测试 5 失败:")
	fmt.Println("  → 88code 拒绝 Bearer header，需要修改 providerrelay 不添加 Bearer")
	fmt.Println()
	fmt.Println("如果所有测试都失败:")
	fmt.Println("  → 需要捕获真实 Claude Code 请求的 headers 来分析")
}
