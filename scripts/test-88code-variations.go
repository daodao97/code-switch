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

// 测试 88code 的各种请求变体
// Author: Half open flowers

type ProviderConfig struct {
	Providers []struct {
		Name   string `json:"name"`
		APIURL string `json:"apiUrl"`
		APIKey string `json:"apiKey"`
	} `json:"providers"`
}

func main() {
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".code-switch", "claude-code.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Printf("读取配置失败: %v\n", err)
		return
	}

	var config ProviderConfig
	json.Unmarshal(data, &config)

	var apiKey, apiURL string
	for _, p := range config.Providers {
		if strings.Contains(strings.ToLower(p.Name), "88code") {
			apiKey = strings.TrimSpace(p.APIKey)
			apiURL = strings.TrimSpace(p.APIURL)
			break
		}
	}

	if apiKey == "" {
		fmt.Println("未找到 88code")
		return
	}

	fmt.Println("================================================================")
	fmt.Println("88code 请求变体测试")
	fmt.Println("================================================================")
	fmt.Printf("API Key 长度: %d\n", len(apiKey))
	fmt.Printf("API Key 前缀: %s\n", apiKey[:6])
	fmt.Println()

	client := &http.Client{Timeout: 30 * time.Second}

	// 尝试不同的 URL 变体
	urls := []string{
		strings.TrimSuffix(apiURL, "/") + "/v1/messages",
		"https://m.88code.org/v1/messages",
		"https://88code.org/api/v1/messages",
		"https://api.88code.org/v1/messages",
	}

	// 尝试不同的请求体格式
	bodies := []struct {
		name string
		body string
	}{
		{
			name: "标准 Anthropic 格式",
			body: `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "使用 claude-3-haiku",
			body: `{"model":"claude-3-haiku-20240307","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "使用 claude-3-5-sonnet",
			body: `{"model":"claude-3-5-sonnet-20241022","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "添加 stream: false",
			body: `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"stream":false,"messages":[{"role":"user","content":"hi"}]}`,
		},
	}

	// 只测试第一个 URL，但尝试不同的请求体
	url := urls[0]
	fmt.Printf("测试 URL: %s\n\n", url)

	for _, b := range bodies {
		fmt.Printf("--- %s ---\n", b.name)

		req, _ := http.NewRequest("POST", url, bytes.NewBufferString(b.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start)

		if err != nil {
			fmt.Printf("错误: %v\n\n", err)
			continue
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		respStr := string(respBody)

		if len(respStr) > 200 {
			respStr = respStr[:200] + "..."
		}

		fmt.Printf("HTTP %d (%v): %s\n\n", resp.StatusCode, latency.Round(time.Millisecond), respStr)
	}

	// 测试不同的 URL
	fmt.Println("================================================================")
	fmt.Println("测试不同的 URL")
	fmt.Println("================================================================")

	body := `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`

	for _, url := range urls {
		fmt.Printf("\n--- %s ---\n", url)

		req, _ := http.NewRequest("POST", url, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start)

		if err != nil {
			fmt.Printf("错误: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		respStr := string(respBody)

		if len(respStr) > 200 {
			respStr = respStr[:200] + "..."
		}

		fmt.Printf("HTTP %d (%v): %s\n", resp.StatusCode, latency.Round(time.Millisecond), respStr)
	}

	// 测试 API Key 去掉 88_ 前缀
	fmt.Println("\n================================================================")
	fmt.Println("测试 API Key 变体")
	fmt.Println("================================================================")

	keyVariants := []struct {
		name string
		key  string
	}{
		{"原始 Key", apiKey},
		{"去掉 88_ 前缀", strings.TrimPrefix(apiKey, "88_")},
		{"添加 sk- 前缀", "sk-" + strings.TrimPrefix(apiKey, "88_")},
	}

	for _, kv := range keyVariants {
		fmt.Printf("\n--- %s ---\n", kv.name)
		fmt.Printf("Key: %s...%s\n", kv.key[:min(10, len(kv.key))], kv.key[max(0, len(kv.key)-4):])

		req, _ := http.NewRequest("POST", urls[0], bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", kv.key)
		req.Header.Set("anthropic-version", "2023-06-01")

		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start)

		if err != nil {
			fmt.Printf("错误: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		respStr := string(respBody)

		if len(respStr) > 200 {
			respStr = respStr[:200] + "..."
		}

		fmt.Printf("HTTP %d (%v): %s\n", resp.StatusCode, latency.Round(time.Millisecond), respStr)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
