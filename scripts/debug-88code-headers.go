// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 测试不同 header 组合对 88code 的影响
// Author: Half open flowers

func main() {
	apiURL := "https://m.88code.org/api/v1/messages"
	apiKey := "88_784022b235595e84936fa42596b41bcad3dc24a79ced14a5d0d4489937ba36e8"
	model := "claude-haiku-4-5-20251001"

	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	fmt.Println("================================================================")
	fmt.Println("88code Header 组合测试")
	fmt.Println("================================================================")

	tests := []struct {
		name    string
		headers map[string]string
	}{
		{
			name: "标准 x-api-key + anthropic-version",
			headers: map[string]string{
				"Content-Type":       "application/json",
				"x-api-key":          apiKey,
				"anthropic-version":  "2023-06-01",
			},
		},
		{
			name: "添加 User-Agent: Claude Code",
			headers: map[string]string{
				"Content-Type":       "application/json",
				"x-api-key":          apiKey,
				"anthropic-version":  "2023-06-01",
				"User-Agent":         "Claude Code/1.0",
			},
		},
		{
			name: "添加 Accept header",
			headers: map[string]string{
				"Content-Type":       "application/json",
				"x-api-key":          apiKey,
				"anthropic-version":  "2023-06-01",
				"Accept":             "application/json",
			},
		},
		{
			name: "不带 anthropic-version",
			headers: map[string]string{
				"Content-Type": "application/json",
				"x-api-key":    apiKey,
			},
		},
		{
			name: "Bearer 认证",
			headers: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer " + apiKey,
			},
		},
		{
			name: "Bearer + anthropic-version",
			headers: map[string]string{
				"Content-Type":       "application/json",
				"Authorization":      "Bearer " + apiKey,
				"anthropic-version":  "2023-06-01",
			},
		},
		{
			name: "同时设置 x-api-key 和 Authorization",
			headers: map[string]string{
				"Content-Type":       "application/json",
				"x-api-key":          apiKey,
				"Authorization":      "Bearer " + apiKey,
				"anthropic-version":  "2023-06-01",
			},
		},
	}

	client := &http.Client{Timeout: 15 * time.Second}

	for _, test := range tests {
		fmt.Printf("\n--- %s ---\n", test.name)

		req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(bodyBytes))
		for k, v := range test.headers {
			req.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 错误: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			fmt.Printf("✅ HTTP %d (%v)\n", resp.StatusCode, latency.Round(time.Millisecond))
		} else {
			fmt.Printf("❌ HTTP %d (%v)\n", resp.StatusCode, latency.Round(time.Millisecond))
			fmt.Printf("   响应: %s\n", string(body))
		}
	}

	// 测试空模型
	fmt.Println("\n================================================================")
	fmt.Println("测试：空模型 vs 无效模型")
	fmt.Println("================================================================")

	modelTests := []string{
		"claude-haiku-4-5-20251001",
		"",
		"invalid-model-xyz",
		"gpt-4",
	}

	for _, testModel := range modelTests {
		testBody := map[string]interface{}{
			"max_tokens": 1,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
		}
		if testModel != "" {
			testBody["model"] = testModel
		}
		data, _ := json.Marshal(testBody)

		req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("model=%q → ❌ 错误: %v\n", testModel, err)
			continue
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			fmt.Printf("model=%q → ✅ HTTP %d\n", testModel, resp.StatusCode)
		} else {
			fmt.Printf("model=%q → ❌ HTTP %d: %s\n", testModel, resp.StatusCode, string(body))
		}
	}
}
