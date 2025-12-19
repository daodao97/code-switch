// +build ignore

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 通过本地 providerrelay 代理测试 88code
// 需要先启动 Code-Switch 应用
// Author: Half open flowers

func main() {
	// 本地代理地址
	relayURL := "http://127.0.0.1:18100/v1/messages"

	// 直接测试 88code
	directURL := "https://m.88code.org/api/v1/messages"

	apiKey := "88_01f315e7e8ea55846f8088e713a9226f693551df66f535288e613934cb918f4a"
	body := `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`

	fmt.Println("================================================================")
	fmt.Println("通过 providerrelay 代理测试 88code")
	fmt.Println("================================================================")
	fmt.Println("请确保 Code-Switch 应用正在运行！")
	fmt.Println()

	client := &http.Client{Timeout: 30 * time.Second}

	// 测试 1: 直接调用 88code
	fmt.Println("--- 1. 直接调用 88code ---")
	testRequest(client, directURL, map[string]string{
		"Content-Type":      "application/json",
		"x-api-key":         apiKey,
		"anthropic-version": "2023-06-01",
	}, body)

	// 测试 2: 通过本地代理（模拟 Claude Code 的请求方式）
	fmt.Println("\n--- 2. 通过本地 providerrelay 代理 (localhost:18100) ---")
	testRequest(client, relayURL, map[string]string{
		"Content-Type":      "application/json",
		"x-api-key":         apiKey,  // Claude Code 会发送 x-api-key
		"anthropic-version": "2023-06-01",
		"User-Agent":        "anthropic-typescript/0.39.0",
		"x-stainless-lang":  "js",
	}, body)

	// 测试 3: 通过代理，但不带 x-api-key（看 providerrelay 如何处理）
	fmt.Println("\n--- 3. 通过代理，不带 x-api-key ---")
	testRequest(client, relayURL, map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
	}, body)

	fmt.Println("\n================================================================")
	fmt.Println("如果测试 2 成功但测试 1 失败:")
	fmt.Println("  → providerrelay 做了某些特殊处理使 88code 接受请求")
	fmt.Println()
	fmt.Println("如果两个测试都失败:")
	fmt.Println("  → API Key 确实无效，或 88code 有其他验证机制")
	fmt.Println("================================================================")
}

func testRequest(client *http.Client, url string, headers map[string]string, body string) {
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(body))
	if err != nil {
		fmt.Printf("❌ 创建请求失败: %v\n", err)
		return
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		fmt.Printf("❌ 请求失败 (%v): %v\n", latency.Round(time.Millisecond), err)
		fmt.Println("   (如果是连接被拒绝，请确保 Code-Switch 正在运行)")
		return
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

	if len(respStr) > 200 {
		respStr = respStr[:200] + "..."
	}
	fmt.Printf("响应: %s\n", respStr)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
