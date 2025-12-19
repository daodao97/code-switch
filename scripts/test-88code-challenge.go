// +build ignore

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 模拟 check-cx 的随机数学题挑战验证
// Author: Half open flowers

type Challenge struct {
	Prompt         string
	ExpectedAnswer int
}

func generateChallenge() Challenge {
	rand.Seed(time.Now().UnixNano())
	a := rand.Intn(50) + 1
	b := rand.Intn(50) + 1

	if rand.Float32() > 0.5 {
		return Challenge{
			Prompt:         fmt.Sprintf("%d + %d = ?", a, b),
			ExpectedAnswer: a + b,
		}
	} else {
		larger := max(a, b)
		smaller := min(a, b)
		return Challenge{
			Prompt:         fmt.Sprintf("%d - %d = ?", larger, smaller),
			ExpectedAnswer: larger - smaller,
		}
	}
}

func validateResponse(response string, expected int) (bool, []int) {
	re := regexp.MustCompile(`-?\d+`)
	matches := re.FindAllString(response, -1)
	if len(matches) == 0 {
		return false, nil
	}

	var numbers []int
	for _, m := range matches {
		n, _ := strconv.Atoi(m)
		numbers = append(numbers, n)
		if n == expected {
			return true, numbers
		}
	}
	return false, numbers
}

func main() {
	apiKey := "88_784022b235595e84936fa42596b41bcad3dc24a79ced14a5d0d4489937ba36e8"

	fmt.Println("================================================================")
	fmt.Println("check-cx 风格：随机数学题挑战验证")
	fmt.Println("================================================================")

	challenge := generateChallenge()
	fmt.Printf("\n挑战: %s\n", challenge.Prompt)
	fmt.Printf("期望答案: %d\n\n", challenge.ExpectedAnswer)

	// Anthropic 格式请求
	reqBody := map[string]interface{}{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 16,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": challenge.Prompt},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	url := "https://m.88code.org/api/v1/messages"
	fmt.Printf("请求 URL: %s\n", url)
	fmt.Printf("请求体: %s\n\n", string(bodyBytes))

	req, _ := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 45 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		fmt.Printf("❌ 网络错误: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("HTTP 状态: %d | 延迟: %v\n\n", resp.StatusCode, latency.Round(time.Millisecond))

	// 处理流式响应
	var collectedResponse strings.Builder
	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	if isSSE {
		fmt.Println("=== 流式响应 (SSE) ===")
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data == "[DONE]" {
					continue
				}
				var event map[string]interface{}
				if json.Unmarshal([]byte(data), &event) == nil {
					if delta, ok := event["delta"].(map[string]interface{}); ok {
						if text, ok := delta["text"].(string); ok {
							collectedResponse.WriteString(text)
							fmt.Print(text)
						}
					}
				}
			}
		}
		fmt.Println()
	} else {
		// 非流式响应
		body, _ := io.ReadAll(resp.Body)
		fmt.Println("=== 响应内容 ===")
		fmt.Println(string(body))

		var respData map[string]interface{}
		if json.Unmarshal(body, &respData) == nil {
			// 检查是否是错误响应
			if errObj, ok := respData["error"].(map[string]interface{}); ok {
				fmt.Printf("\n❌ 服务端返回错误:\n")
				fmt.Printf("   type: %v\n", errObj["type"])
				fmt.Printf("   message: %v\n", errObj["message"])
				return
			}
			// 提取 content
			if content, ok := respData["content"].([]interface{}); ok && len(content) > 0 {
				if textBlock, ok := content[0].(map[string]interface{}); ok {
					if text, ok := textBlock["text"].(string); ok {
						collectedResponse.WriteString(text)
					}
				}
			}
		}
	}

	response := collectedResponse.String()
	fmt.Printf("\n=== 验证结果 ===\n")
	fmt.Printf("收集到的回复: %q\n", response)

	if response == "" {
		fmt.Println("❌ 验证失败: 回复为空")
		return
	}

	valid, numbers := validateResponse(response, challenge.ExpectedAnswer)
	fmt.Printf("提取到的数字: %v\n", numbers)
	fmt.Printf("期望答案: %d\n", challenge.ExpectedAnswer)

	if valid {
		fmt.Printf("✅ 验证通过! AI 正确回答了数学题\n")
	} else {
		fmt.Printf("❌ 验证失败: 回复中不包含正确答案 %d\n", challenge.ExpectedAnswer)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
