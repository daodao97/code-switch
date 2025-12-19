package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite" // SQLite driver
)

func main() {
	// 初始化数据库连接
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".code-switch", "app.db?cache=shared&mode=rwc&_busy_timeout=10000&_journal_mode=WAL")

	fmt.Printf("测试数据库: %s\n\n", filepath.Join(home, ".code-switch", "app.db"))

	if err := xdb.Inits([]xdb.Config{
		{
			Name:   "default",
			Driver: "sqlite",
			DSN:    dbPath,
		},
	}); err != nil {
		fmt.Printf("初始化数据库失败: %v\n", err)
		return
	}

	// 测试1: 使用 xdb.New().Insert() (旧方法 - 会失败)
	fmt.Println("========== 测试1: 使用 xdb.New().Insert() (旧方法) ==========")
	testXdbInsert()

	time.Sleep(1 * time.Second)

	// 测试2: 使用 db.Exec() (新方法 - 应该成功)
	fmt.Println("\n========== 测试2: 使用 db.Exec() (新方法) ==========")
	testDirectExec()

	fmt.Println("\n========== 测试完成 ==========")
}

// testXdbInsert 测试使用 xdb.New().Insert() 的并发写入
func testXdbInsert() {
	var wg sync.WaitGroup
	concurrency := 10
	errors := 0
	success := 0
	mu := sync.Mutex{}

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			_, err := xdb.New("request_log").Insert(xdb.Record{
				"platform":            "test",
				"model":               "test-model",
				"provider":            fmt.Sprintf("provider-%d", id),
				"http_code":           200,
				"input_tokens":        100,
				"output_tokens":       200,
				"cache_create_tokens": 0,
				"cache_read_tokens":   0,
				"reasoning_tokens":    0,
				"is_stream":           0,
				"duration_sec":        1.5,
			})

			mu.Lock()
			if err != nil {
				errors++
				fmt.Printf("  ❌ [%d] 失败: %v\n", id, err)
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

	if errors > 0 {
		fmt.Printf("  ⚠️  检测到 %d 个事务嵌套错误（符合预期）\n", errors)
	}
}

// testDirectExec 测试使用 db.Exec() 的并发写入
func testDirectExec() {
	var wg sync.WaitGroup
	concurrency := 10
	errors := 0
	success := 0
	mu := sync.Mutex{}

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			db, err := xdb.DB("default")
			if err != nil {
				mu.Lock()
				errors++
				fmt.Printf("  ❌ [%d] 获取连接失败: %v\n", id, err)
				mu.Unlock()
				return
			}

			_, err = db.Exec(`
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

			mu.Lock()
			if err != nil {
				errors++
				fmt.Printf("  ❌ [%d] 失败: %v\n", id, err)
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

	if errors == 0 {
		fmt.Printf("  ✅ 所有请求成功，无事务嵌套错误！\n")
	} else {
		fmt.Printf("  ⚠️  仍有 %d 个错误\n", errors)
	}
}
