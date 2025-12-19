// 黑名单时区问题诊断脚本
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".code-switch", "app.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("打开数据库失败: %v\n", err)
		return
	}
	defer db.Close()

	fmt.Println("=== 黑名单时区诊断 ===\n")

	// 1. 查看当前时间
	now := time.Now()
	fmt.Printf("1. Go 本地时间: %s\n", now.Format("2006-01-02 15:04:05 -07:00"))
	fmt.Printf("   Go UTC 时间:  %s\n", now.UTC().Format("2006-01-02 15:04:05"))

	// 2. 查看 SQLite 的时间
	var sqliteNow string
	db.QueryRow(`SELECT datetime('now')`).Scan(&sqliteNow)
	fmt.Printf("   SQLite now:   %s (UTC)\n\n", sqliteNow)

	// 3. 查看数据库中存储的黑名单时间
	rows, err := db.Query(`
		SELECT
			platform,
			provider_name,
			blacklisted_until,
			auto_recovered,
			failure_count
		FROM provider_blacklist
		WHERE blacklisted_until IS NOT NULL
		ORDER BY blacklisted_until DESC
	`)
	if err != nil {
		fmt.Printf("查询失败: %v\n", err)
		return
	}
	defer rows.Close()

	fmt.Println("2. 数据库中的黑名单记录:\n")
	hasRecords := false
	for rows.Next() {
		hasRecords = true
		var platform, providerName string
		var blacklistedUntil sql.NullTime
		var autoRecovered, failureCount int

		rows.Scan(&platform, &providerName, &blacklistedUntil, &autoRecovered, &failureCount)

		fmt.Printf("   Platform: %s\n", platform)
		fmt.Printf("   Provider: %s\n", providerName)
		if blacklistedUntil.Valid {
			fmt.Printf("   过期时间: %s\n", blacklistedUntil.Time.Format("2006-01-02 15:04:05 -07:00"))
			fmt.Printf("   过期时间 (UTC): %s\n", blacklistedUntil.Time.UTC().Format("2006-01-02 15:04:05"))
		}
		fmt.Printf("   已恢复: %v\n", autoRecovered == 1)
		fmt.Printf("   失败次数: %d\n", failureCount)

		// 4. 手动判断是否过期
		if blacklistedUntil.Valid {
			isExpired := blacklistedUntil.Time.Before(now)
			fmt.Printf("   Go 判断已过期: %v\n", isExpired)
		}

		// 5. SQL 判断是否过期
		var count int
		db.QueryRow(`
			SELECT COUNT(*)
			FROM provider_blacklist
			WHERE platform = ? AND provider_name = ? AND blacklisted_until > datetime('now')
		`, platform, providerName).Scan(&count)
		fmt.Printf("   SQL 判断仍在黑名单: %v\n\n", count > 0)
	}

	if !hasRecords {
		fmt.Println("   ✓ 没有黑名单记录\n")
	}

	// 6. 测试时间存储和比较
	fmt.Println("3. 时间存储测试:\n")
	testTime := time.Now().Add(5 * time.Minute)
	fmt.Printf("   Go 时间: %s\n", testTime.Format("2006-01-02 15:04:05 -07:00"))

	// 创建临时表测试
	db.Exec(`CREATE TEMP TABLE time_test (test_time DATETIME)`)
	db.Exec(`INSERT INTO time_test (test_time) VALUES (?)`, testTime)

	var storedTime sql.NullTime
	db.QueryRow(`SELECT test_time FROM time_test`).Scan(&storedTime)
	if storedTime.Valid {
		fmt.Printf("   存储后读取: %s\n", storedTime.Time.Format("2006-01-02 15:04:05 -07:00"))
		fmt.Printf("   时区是否一致: %v\n", storedTime.Time.Location() == testTime.Location())
	}

	// 7. SQL 比较测试
	var isGreater int
	db.QueryRow(`SELECT test_time > datetime('now') FROM time_test`).Scan(&isGreater)
	fmt.Printf("   SQL 比较 (test_time > now): %v\n", isGreater == 1)

	goCompare := testTime.After(time.Now())
	fmt.Printf("   Go 比较 (test_time > now):  %v\n\n", goCompare)

	// 8. 结论
	fmt.Println("=== 诊断建议 ===\n")
	if hasRecords {
		fmt.Println("⚠️  发现黑名单记录，请检查：")
		fmt.Println("   1. 'SQL 判断仍在黑名单' 是否与 'Go 判断已过期' 矛盾")
		fmt.Println("   2. 如果矛盾，说明存在时区问题")
		fmt.Println("   3. 运行应用时查看控制台是否有 '⛔ Provider xxx 已拉黑' 日志")
	} else {
		fmt.Println("✓ 没有黑名单记录，问题可能在其他过滤条件（模型支持、配置验证）")
	}
}
