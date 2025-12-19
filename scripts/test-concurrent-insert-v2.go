package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".code-switch", "app.db")

	fmt.Printf("测试数据库: %s\n\n", dbPath)

	// 直接使用database/sql，设置busy_timeout
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("打开数据库失败: %v\n", err)
		return
	}
	defer db.Close()

	// 设置busy_timeout（10秒）
	_, err = db.Exec("PRAGMA busy_timeout = 10000")
	if err != nil {
		fmt.Printf("设置busy_timeout失败: %v\n", err)
		return
	}

	// 设置WAL模式
	_, err = db.Exec("PRAGMA journal_mode = WAL")
	if err != nil {
		fmt.Printf("设置WAL模式失败: %v\n", err)
		return
	}

	fmt.Println("========== 测试: 并发写入（带重试机制）==========")

	var wg sync.WaitGroup
	concurrency := 10
	errors := 0
	success := 0
	retries := 0
	mu := sync.Mutex{}

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 重试最多3次
			var lastErr error
			for attempt := 0; attempt < 3; attempt++ {
				_, err := db.Exec(`
					INSERT INTO request_log (
						platform, model, provider, http_code,
						input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
						reasoning_tokens, is_stream, duration_sec
					) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`,
					"test",
					"test-model",
					fmt.Sprintf("provider-%d", id),
					200,
					100,
					200,
					0,
					0,
					0,
					0,
					1.5,
				)

				if err == nil {
					mu.Lock()
					success++
					if attempt > 0 {
						retries++
						fmt.Printf("  ✅ [%d] 成功（重试%d次后）\n", id, attempt)
					} else {
						fmt.Printf("  ✅ [%d] 成功\n", id)
					}
					mu.Unlock()
					return
				}

				lastErr = err

				// 如果是SQLITE_BUSY，等待后重试
				if strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked") {
					time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
					continue
				}

				// 其他错误直接失败
				break
			}

			mu.Lock()
			errors++
			fmt.Printf("  ❌ [%d] 失败（重试3次后仍失败）: %v\n", id, lastErr)
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("\n结果统计:\n")
	fmt.Printf("  总请求: %d\n", concurrency)
	fmt.Printf("  成功: %d\n", success)
	fmt.Printf("  失败: %d\n", errors)
	fmt.Printf("  重试成功: %d\n", retries)
	fmt.Printf("  耗时: %v\n", duration)

	if errors == 0 {
		fmt.Printf("\n  ✅ 所有请求成功！重试机制有效。\n")
	} else {
		fmt.Printf("\n  ⚠️  仍有 %d 个请求失败\n", errors)
	}
}
