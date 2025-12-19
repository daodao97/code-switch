// +build ignore

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 对比 Go http 和 curl 的请求差异
// Author: Half open flowers

func main() {
	apiKey := "88_784022b235595e84936fa42596b41bcad3dc24a79ced14a5d0d4489937ba36e8"
	url := "https://m.88code.org/api/v1/messages"
	body := `{"max_tokens":1,"messages":[{"content":"hi","role":"user"}],"model":"claude-haiku-4-5-20251001"}`

	fmt.Println("=== Go http.Client 测试 ===")

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("POST", url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	fmt.Println("请求头:")
	for k, v := range req.Header {
		fmt.Printf("  %s: %s\n", k, v[0])
	}
	fmt.Printf("请求体长度: %d\n", len(body))
	fmt.Printf("请求体: %s\n", body)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("\n响应状态: %d\n", resp.StatusCode)
	fmt.Printf("响应内容: %s\n", string(respBody)[:min(200, len(respBody))])

	// 检查响应头
	fmt.Println("\n响应头:")
	for k, v := range resp.Header {
		fmt.Printf("  %s: %s\n", k, v[0])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
