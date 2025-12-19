// +build ignore

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 测试新的 88code API Key
// Author: Half open flowers

func main() {
	apiKey := "88_01f315e7e8ea55846f8088e713a9226f693551df66f535288e613934cb918f4a"
	url := "https://m.88code.org/api/v1/messages"
	body := `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`

	fmt.Println("================================================================")
	fmt.Println("测试新的 88code API Key")
	fmt.Println("================================================================")
	fmt.Printf("Key: %s...%s (len=%d)\n\n", apiKey[:10], apiKey[len(apiKey)-4:], len(apiKey))

	client := &http.Client{Timeout: 30 * time.Second}

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
			name: "2. x-api-key + Bearer (模拟 providerrelay)",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
				"Authorization":     "Bearer " + apiKey,
			},
		},
		{
			name: "3. 只有 Bearer",
			headers: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer " + apiKey,
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
			fmt.Printf("❌ 网络错误: %v\n\n", err)
			continue
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		respStr := string(respBody)

		// 判断成功
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
