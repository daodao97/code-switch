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

// 对比测试多个供应商，验证请求逻辑是否正确
// Author: Half open flowers

type Provider struct {
	Name    string `json:"name"`
	APIURL  string `json:"apiUrl"`
	APIKey  string `json:"apiKey"`
	Enabled bool   `json:"enabled"`
}

type Config struct {
	Providers []Provider `json:"providers"`
}

func main() {
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".code-switch", "claude-code.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Printf("读取配置失败: %v\n", err)
		return
	}

	var config Config
	json.Unmarshal(data, &config)

	fmt.Println("================================================================")
	fmt.Println("多供应商对比测试")
	fmt.Println("================================================================")
	fmt.Println("使用相同的请求格式测试所有启用的供应商")
	fmt.Println("如果其他供应商成功但 88code 失败，说明 88code 的 Key 有问题")
	fmt.Println()

	client := &http.Client{Timeout: 30 * time.Second}
	body := `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`

	for _, p := range config.Providers {
		if !p.Enabled || p.APIKey == "" || p.APIURL == "" {
			continue
		}

		url := strings.TrimSuffix(p.APIURL, "/") + "/v1/messages"
		fmt.Printf("--- %s ---\n", p.Name)
		fmt.Printf("URL: %s\n", url)
		fmt.Printf("Key: %s...%s\n", p.APIKey[:min(8, len(p.APIKey))], p.APIKey[max(0, len(p.APIKey)-4):])

		req, _ := http.NewRequest("POST", url, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", p.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")

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

		// 判断是否成功
		isSuccess := resp.StatusCode == 200 &&
			strings.Contains(respStr, `"content"`) &&
			!strings.Contains(respStr, `"error"`)

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

	fmt.Println("================================================================")
	fmt.Println("结论")
	fmt.Println("================================================================")
	fmt.Println("如果其他供应商成功但 88code 失败:")
	fmt.Println("  → 88code 的 API Key 无效/过期，需要联系 88code 服务商")
	fmt.Println()
	fmt.Println("如果所有供应商都失败:")
	fmt.Println("  → 请求格式或网络有问题")
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
