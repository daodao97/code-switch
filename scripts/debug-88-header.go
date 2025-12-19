// +build ignore

package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httputil"
)

// 测试 88_ 开头的 API Key 在 HTTP header 中的实际传输情况
// Author: Half open flowers

func main() {
	apiKey := "88_784022b235595e84936fa42596b41bcad3dc24a79ced14a5d0d4489937ba36e8"
	url := "https://m.88code.org/api/v1/messages"
	body := []byte(`{"max_tokens":1,"messages":[{"content":"hi","role":"user"}],"model":"claude-haiku-4-5-20251001"}`)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	// 打印实际发送的 HTTP 请求
	dump, _ := httputil.DumpRequestOut(req, false)
	fmt.Println("=== 实际发送的 HTTP 请求头 ===")
	fmt.Println(string(dump))

	// 检查 header 值
	fmt.Println("=== Header 值检查 ===")
	fmt.Printf("原始 API Key: %q (len=%d)\n", apiKey, len(apiKey))
	fmt.Printf("Header 中的值: %q (len=%d)\n", req.Header.Get("x-api-key"), len(req.Header.Get("x-api-key")))

	// 检查是否相等
	if apiKey == req.Header.Get("x-api-key") {
		fmt.Println("✅ Header 值与原始值完全相同")
	} else {
		fmt.Println("❌ Header 值与原始值不同！")
		// 逐字符对比
		for i := 0; i < len(apiKey); i++ {
			h := req.Header.Get("x-api-key")
			if i < len(h) && apiKey[i] != h[i] {
				fmt.Printf("  位置 %d: 原始=%q, header=%q\n", i, apiKey[i], h[i])
			}
		}
	}

	// 检查 88_ 开头是否被解析为数字
	fmt.Println("\n=== 字符分析 ===")
	fmt.Printf("前5个字符: ")
	for i := 0; i < 5 && i < len(apiKey); i++ {
		fmt.Printf("%q(0x%02x) ", apiKey[i], apiKey[i])
	}
	fmt.Println()
}
