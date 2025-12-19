package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	// 使用临时测试数据库
	dbPath := "./test_concurrent.db"

	// 删除旧的测试数据库
	os.Remove(dbPath)

	fmt.Printf("测试数据库: %s\n\n", dbPath)

	// 直接使用database/sql，设置busy_timeout
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("打开数据库失败: %v\n", err)
		return
	}
	defer db.Close()
	defer os.Remove(dbPath) // 清理测试数据库

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

	// 创建测试表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS request_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			platform TEXT,
			model TEXT,
			provider TEXT,
			http_code INTEGER,
			input_tokens INTEGER,
			output_tokens INTEGER,
			cache_create_tokens INTEGER,
			cache_read_tokens INTEGER,
			reasoning_tokens INTEGER,
			is_stream INTEGER,
			duration_sec REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		fmt.Printf("创建表失败: %v\n", err)
		return
	}

	fmt.Println("✅ 数据库初始化成功（WAL模式 + 10秒超时）\n")

	// 测试1: 不带重试的并发写入
	fmt.Println("========== 测试1: 并发写入（无重试）==========")
	testWithoutRetry(db, 10)

	time.Sleep(100 * time.Millisecond)

	// 测试2: 带重试的并发写入
	fmt.Println("\n========== 测试2: 并发写入（带重试）==========")
	testWithRetry(db, 10)
}

func testWithoutRetry(db *sql.DB, concurrency int) {
	var wg sync.WaitGroup
	errors := 0
	success := 0
	mu := sync.Mutex{}

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			_, err := db.Exec(`
				INSERT INTO request_log (
					platform, model, provider, http_code,
					input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
					reasoning_tokens, is_stream, duration_sec
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`,
				"test1",
				"test-model",
				fmt.Sprintf("provider-%d", id),
				200, 100, 200, 0, 0, 0, 0, 1.5,
			)

			mu.Lock()
			if err != nil {
				errors++
				if strings.Contains(err.Error(), "SQLITE_BUSY") {
					fmt.Printf("  ⚠️  [%d] SQLITE_BUSY（busy_timeout可能未生效）\n", id)
				} else {
					fmt.Printf("  ❌ [%d] 失败: %v\n", id, err)
				}
			} else {
				success++
				fmt.Printf("  ✅ [%d] 成功\n", id)
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("\n结果统计:\n")
	fmt.Printf("  总请求: %d\n", concurrency)
	fmt.Printf("  成功: %d\n", success)
	fmt.Printf("  失败: %d\n", errors)
	fmt.Printf("  耗时: %v\n", duration)
}

func testWithRetry(db *sql.DB, concurrency int) {
	var wg sync.WaitGroup
	errors := 0
	success := 0
	retries := 0
	mu := sync.Mutex{}

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			var lastErr error
			for attempt := 0; attempt < 3; attempt++ {
				_, err := db.Exec(`
					INSERT INTO request_log (
						platform, model, provider, http_code,
						input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
						reasoning_tokens, is_stream, duration_sec
					) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`,
					"test2",
					"test-model",
					fmt.Sprintf("provider-%d", id),
					200, 100, 200, 0, 0, 0, 0, 1.5,
				)

				if err == nil {
					mu.Lock()
					success++
					if attempt > 0 {
						retries++
						fmt.Printf("  ✅ [%d] 成功（第%d次重试）\n", id, attempt+1)
					} else {
						fmt.Printf("  ✅ [%d] 成功\n", id)
					}
					mu.Unlock()
					return
				}

				lastErr = err

				if strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked") {
					time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
					continue
				}

				break
			}

			mu.Lock()
			errors++
			fmt.Printf("  ❌ [%d] 失败（3次重试后）: %v\n", id, lastErr)
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
		fmt.Printf("\n  ✅ 所有请求成功！带重试的方案有效。\n")
	}
}
