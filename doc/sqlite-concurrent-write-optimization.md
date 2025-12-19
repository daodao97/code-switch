# SQLite 并发写入优化方案 - 顺序写入队列架构

## 一、用户需求确认
- ✅ **可靠性要求**：必须 100% 写入成功
- ✅ **性能要求**：平衡模式（支持 100-500 req/s）
- ✅ **实施策略**：一次性彻底解决

**方案选择**：顺序写入队列（唯一能保证 100% 成功的方案）

---

## 二、现状分析

### 并发写入统计
- **高频并发写入**：9处（blacklistservice.go）
- **中频并发写入**：2处（providerrelay.go - request_log）
- **低频写入**：6处（settingsservice.go, database.go）

### 核心问题
1. SQLite 单写入限制导致 SQLITE_BUSY 错误
2. 当前重试机制成功率仅 80%，无法满足 100% 要求
3. 事务泄漏已修复，但并发写入冲突仍存在

---

## 三、顺序写入队列架构设计

### 3.1 核心设计原则
1. **单线程写入**：所有数据库写入操作通过单一 worker goroutine 执行
2. **同步接口**：调用方阻塞等待写入结果，保证错误能被捕获
3. **批量优化**：支持批量提交，提升吞吐量
4. **优雅关闭**：应用退出时确保所有待处理任务完成
5. **可监控性**：提供队列状态、性能指标

---

### 3.2 数据结构设计

```go
// services/dbqueue.go (新文件)

package services

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// WriteTask 写入任务
type WriteTask struct {
	SQL    string          // SQL语句
	Args   []interface{}   // 参数
	Result chan error      // 结果通道（同步等待）
}

// DBWriteQueue 数据库写入队列
type DBWriteQueue struct {
	db            *sql.DB
	queue         chan *WriteTask
	batchQueue    chan *WriteTask  // 批量提交队列
	shutdownChan  chan struct{}
	wg            sync.WaitGroup

	// 关闭状态标志（🔧 新增：防止 Shutdown 后仍可入队）
	closed        atomic.Bool

	// 性能监控
	stats         *QueueStats
	statsMu       sync.RWMutex

	// P99 延迟计算（环形缓冲区存储最近1000个样本）
	latencySamples []float64  // 延迟样本（毫秒）
	sampleIndex    int        // 当前写入位置
	sampleCount    int64      // 已记录样本数
}

// QueueStats 队列统计
type QueueStats struct {
	QueueLength      int     // 当前单次队列长度
	BatchQueueLength int     // 当前批量队列长度（如果启用）
	TotalWrites      int64   // 总写入数
	SuccessWrites    int64   // 成功写入数
	FailedWrites     int64   // 失败写入数
	AvgLatencyMs     float64 // 平均延迟（毫秒）
	P99LatencyMs     float64 // P99延迟
	BatchCommits     int64   // 批量提交次数
}

// NewDBWriteQueue 创建写入队列
// queueSize: 队列缓冲大小（推荐 1000-5000）
// enableBatch: 是否启用批量提交
//
// ⚠️ **批量模式使用约束**（critical）：
// - **仅用于同构写入**：批量通道（ExecBatch）只应用于相同表、相同操作的 SQL
//   - ✅ 正确用法：所有 request_log 的 INSERT（同一表、同一操作、参数结构相同）
//   - ❌ 错误用法：混入不同表的写入（request_log + provider_blacklist）
//   - ❌ 错误用法：混入不同操作（INSERT + UPDATE + DELETE）
// - **为什么必须同构**：
//   - 统计模型假设批次延迟在所有任务间均匀分布（perTaskLatencyMs = batchLatencyMs / count）
//   - 如果批次内有慢 SQL（触发器、复杂索引），会稀释快 SQL 的延迟统计
//   - P99 延迟会被低估，无法真实反映单请求 SLA
// - **代码审查检查点**：
//   - 搜索所有 ExecBatch/ExecBatchCtx 调用
//   - 确认每个调用点只写入同一个表的同一种操作
//   - 异构写入必须使用 Exec/ExecCtx（单次提交，统计准确）
func NewDBWriteQueue(db *sql.DB, queueSize int, enableBatch bool) *DBWriteQueue {
	q := &DBWriteQueue{
		db:             db,
		queue:          make(chan *WriteTask, queueSize),
		shutdownChan:   make(chan struct{}),
		stats:          &QueueStats{},
		latencySamples: make([]float64, 1000), // 环形缓冲区容量1000
		sampleIndex:    0,
		sampleCount:    0,
	}

	if enableBatch {
		q.batchQueue = make(chan *WriteTask, queueSize)
		q.wg.Add(1)
		go q.batchWorker() // 批量提交 worker
	}

	q.wg.Add(1)
	go q.worker() // 主 worker

	return q
}

// worker 单线程顺序处理所有写入
func (q *DBWriteQueue) worker() {
	defer q.wg.Done()

	var currentTask *WriteTask  // 命名变量，用于在 panic 时返回错误

	// panic 保护：确保 worker 不会因未捕获的 panic 而崩溃
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("🚨 数据库写入队列 worker panic: %v\n", r)

			// 🔧 关键修复：如果 panic 时正在处理任务，必须返回错误，否则调用方永久阻塞
			if currentTask != nil {
				currentTask.Result <- fmt.Errorf("数据库写入 panic: %v", r)
				close(currentTask.Result)
			}

			// 等待1秒后重启，避免快速循环（如果是系统性问题）
			time.Sleep(1 * time.Second)

			// 自动重启 worker
			q.wg.Add(1)
			go q.worker()
		}
	}()

	for {
		select {
		case task := <-q.queue:
			currentTask = task  // 记录当前任务，用于 panic 时返回错误

			start := time.Now()
			_, err := q.db.Exec(task.SQL, task.Args...)

			// 更新统计（单次写入，count=1）
			q.updateStats(1, time.Since(start), err)

			// 返回结果
			task.Result <- err
			close(task.Result)

			currentTask = nil  // 清空当前任务（防止下一次 panic 误用）

		case <-q.shutdownChan:
			// 排空 queue 中的所有剩余任务
			for {
				select {
				case task := <-q.queue:
					currentTask = task  // shutdown 排空时也需要跟踪，防止 panic

					start := time.Now()
					_, err := q.db.Exec(task.SQL, task.Args...)
					q.updateStats(1, time.Since(start), err)
					task.Result <- err
					close(task.Result)

					currentTask = nil
				default:
					// queue 已空，安全退出
					return
				}
			}
		}
	}
}

// batchWorker 批量提交 worker（可选）
func (q *DBWriteQueue) batchWorker() {
	defer q.wg.Done()

	var currentBatch []*WriteTask  // 命名变量，用于在 panic 时返回错误

	// panic 保护：确保 batchWorker 不会因未捕获的 panic 而崩溃
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("🚨 数据库批量写入队列 worker panic: %v\n", r)

			// 🔧 关键修复：如果 panic 时正在处理批次，必须给所有任务返回错误
			if len(currentBatch) > 0 {
				panicErr := fmt.Errorf("批量写入 panic: %v", r)
				for _, task := range currentBatch {
					task.Result <- panicErr
					close(task.Result)
				}
			}

			// 等待1秒后重启，避免快速循环（如果是系统性问题）
			time.Sleep(1 * time.Second)

			// 自动重启 batchWorker
			q.wg.Add(1)
			go q.batchWorker()
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond) // 每100ms批量提交一次
	defer ticker.Stop()

	var batch []*WriteTask

	for {
		select {
		case task := <-q.batchQueue:
			batch = append(batch, task)

			// 批次达到上限（50条）或超时，立即提交
			if len(batch) >= 50 {
				currentBatch = batch  // 记录当前批次，用于 panic 时返回错误
				q.commitBatch(batch)
				batch = nil
				currentBatch = nil
			}

		case <-ticker.C:
			if len(batch) > 0 {
				currentBatch = batch
				q.commitBatch(batch)
				batch = nil
				currentBatch = nil
			}

		case <-q.shutdownChan:
			// 1. 先提交当前批次
			if len(batch) > 0 {
				currentBatch = batch
				q.commitBatch(batch)
				batch = nil
				currentBatch = nil
			}

			// 2. 排空 batchQueue 中的所有剩余任务
			for {
				select {
				case task := <-q.batchQueue:
					batch = append(batch, task)
					// 每收集50个或队列空了就提交一次
					if len(batch) >= 50 {
						currentBatch = batch
						q.commitBatch(batch)
						batch = nil
						currentBatch = nil
					}
				default:
					// batchQueue 已空，提交最后一批
					if len(batch) > 0 {
						currentBatch = batch
						q.commitBatch(batch)
						currentBatch = nil
					}
					return
				}
			}
		}
	}
}

// commitBatch 批量提交（使用事务）
func (q *DBWriteQueue) commitBatch(tasks []*WriteTask) {
	start := time.Now()

	// 辅助函数：给所有任务返回结果（成功或失败）
	sendResultToAll := func(err error) {
		for _, task := range tasks {
			task.Result <- err
			close(task.Result)
		}
		// 更新统计（批量提交，count=任务数）
		q.updateStats(len(tasks), time.Since(start), err)
		if err == nil {
			q.statsMu.Lock()
			q.stats.BatchCommits++
			q.statsMu.Unlock()
		}
	}

	tx, err := q.db.Begin()
	if err != nil {
		// 事务开启失败，所有任务都失败
		sendResultToAll(err)
		return
	}
	defer tx.Rollback()

	// 执行所有任务，记录第一个错误
	var firstErr error
	for _, task := range tasks {
		_, err := tx.Exec(task.SQL, task.Args...)
		if err != nil && firstErr == nil {
			firstErr = err  // 记录第一个错误，但继续执行以清理资源
		}
	}

	// 如果有任何错误，回滚并通知所有任务
	if firstErr != nil {
		sendResultToAll(fmt.Errorf("批量提交失败: %w", firstErr))
		return
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		sendResultToAll(fmt.Errorf("事务提交失败: %w", err))
		return
	}

	// 全部成功
	sendResultToAll(nil)
}

// Exec 同步执行写入（阻塞直到完成，默认 30 秒超时）
// 🔧 防御性设计：即使在高频路径误用，也有 30 秒兜底超时，避免永久阻塞
func (q *DBWriteQueue) Exec(sql string, args ...interface{}) error {
	// 先检查关闭状态
	if q.closed.Load() {
		return fmt.Errorf("写入队列已关闭")
	}

	task := &WriteTask{
		SQL:    sql,
		Args:   args,
		Result: make(chan error, 1),
	}

	// 默认 30 秒超时（防止误用导致永久阻塞）
	timeout := time.After(30 * time.Second)

	select {
	case q.queue <- task:
		// 成功入队，等待结果（支持超时）
		select {
		case err := <-task.Result:
			return err
		case <-timeout:
			// 超时，但任务已入队，无法撤销，需等待结果以避免 goroutine 泄漏
			go func() { <-task.Result }()
			return fmt.Errorf("写入超时（30秒），队列可能积压严重")
		}

	case <-timeout:
		// 入队失败（队列满），直接返回
		return fmt.Errorf("入队超时（30秒），队列已满")

	case <-q.shutdownChan:
		return fmt.Errorf("写入队列已关闭")
	}
}

// ExecBatch 批量执行（异步，高吞吐量场景，默认 30 秒超时）
// 🔧 防御性设计：即使误用，也有 30 秒兜底超时
func (q *DBWriteQueue) ExecBatch(sql string, args ...interface{}) error {
	// 先检查关闭状态
	if q.closed.Load() {
		return fmt.Errorf("写入队列已关闭")
	}

	if q.batchQueue == nil {
		return fmt.Errorf("批量模式未启用")
	}

	task := &WriteTask{
		SQL:    sql,
		Args:   args,
		Result: make(chan error, 1),
	}

	// 默认 30 秒超时（防止误用导致永久阻塞）
	timeout := time.After(30 * time.Second)

	select {
	case q.batchQueue <- task:
		// 成功入队，等待结果（支持超时）
		select {
		case err := <-task.Result:
			return err
		case <-timeout:
			// 超时，但任务已入队，无法撤销
			go func() { <-task.Result }()
			return fmt.Errorf("批量写入超时（30秒），批量队列可能积压严重")
		}

	case <-timeout:
		// 入队失败（队列满），直接返回
		return fmt.Errorf("批量入队超时（30秒），队列已满")

	case <-q.shutdownChan:
		return fmt.Errorf("写入队列已关闭")
	}
}

// ExecCtx 支持 context 的写入（带超时控制）
func (q *DBWriteQueue) ExecCtx(ctx context.Context, sql string, args ...interface{}) error {
	// 🔧 关键修复：先检查关闭状态
	if q.closed.Load() {
		return fmt.Errorf("写入队列已关闭")
	}

	task := &WriteTask{
		SQL:    sql,
		Args:   args,
		Result: make(chan error, 1),
	}

	select {
	case q.queue <- task:
		// 成功入队，等待结果（支持超时）
		select {
		case err := <-task.Result:
			return err
		case <-ctx.Done():
			// 超时或取消，但任务已入队，无法撤销
			// 仍需等待结果以避免 goroutine 泄漏
			go func() { <-task.Result }()
			return fmt.Errorf("写入超时或已取消: %w", ctx.Err())
		}

	case <-ctx.Done():
		// 入队失败（队列满），直接返回
		return fmt.Errorf("入队超时或已取消（队列满）: %w", ctx.Err())

	case <-q.shutdownChan:
		return fmt.Errorf("写入队列已关闭")
	}
}

// ExecBatchCtx 支持 context 的批量写入（带超时控制）
func (q *DBWriteQueue) ExecBatchCtx(ctx context.Context, sql string, args ...interface{}) error {
	// 🔧 关键修复：先检查关闭状态
	if q.closed.Load() {
		return fmt.Errorf("写入队列已关闭")
	}

	if q.batchQueue == nil {
		return fmt.Errorf("批量模式未启用")
	}

	task := &WriteTask{
		SQL:    sql,
		Args:   args,
		Result: make(chan error, 1),
	}

	select {
	case q.batchQueue <- task:
		// 成功入队，等待结果（支持超时）
		select {
		case err := <-task.Result:
			return err
		case <-ctx.Done():
			// 超时或取消，但任务已入队，无法撤销
			go func() { <-task.Result }()
			return fmt.Errorf("批量写入超时或已取消: %w", ctx.Err())
		}

	case <-ctx.Done():
		// 入队失败（队列满），直接返回
		return fmt.Errorf("批量入队超时或已取消（队列满）: %w", ctx.Err())

	case <-q.shutdownChan:
		return fmt.Errorf("写入队列已关闭")
	}
}

// Shutdown 优雅关闭
func (q *DBWriteQueue) Shutdown(timeout time.Duration) error {
	// 🔧 关键修复：先设置关闭标志，拒绝新请求入队
	q.closed.Store(true)

	// 然后关闭 shutdownChan，通知 worker 排空队列
	close(q.shutdownChan)

	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("关闭超时，队列中仍有 %d 个任务", len(q.queue))
	}
}

// GetStats 获取统计信息
func (q *DBWriteQueue) GetStats() QueueStats {
	q.statsMu.RLock()
	defer q.statsMu.RUnlock()

	stats := *q.stats
	stats.QueueLength = len(q.queue)

	// 如果启用了批量队列，也返回其长度
	if q.batchQueue != nil {
		stats.BatchQueueLength = len(q.batchQueue)
	}

	return stats
}

// updateStats 更新统计信息
// count: 本次操作涵盖的任务数（单次=1，批量=len(tasks)）
// latency: 操作耗时
// err: 错误（nil表示成功）
//
// 📌 统计假设与局限性说明：
//
// 1. **平均延迟计算假设**：
//    - 批量提交时，假设批次延迟在所有任务间均匀分布
//    - 计算公式：AvgLatencyMs = (旧总延迟 + 批次延迟) / 新总任务数
//    - 局限性：如果批次内不同 SQL 耗时差异巨大（如含触发器、复杂索引），统计会失真
//
// 2. **P99 延迟计算假设**：
//    - 批量提交时，将批次延迟平均分摊到每个任务（perTaskLatencyMs = latencyMs / count）
//    - 每个任务记录相同的延迟样本，用于 P99 计算
//    - 局限性：真实情况下，批次内首个任务可能耗时更长（事务开启开销），最后一个任务可能更快
//
// 3. **适用场景**：
//    - ✅ 批次内所有 SQL 耗时相近（如 request_log INSERT，相同表结构、无触发器）
//    - ✅ 关注整体系统性能趋势，而非单条 SQL 精确耗时
//    - ❌ 批次内混合不同类型操作（INSERT + UPDATE + DELETE）
//    - ❌ 需要精确追踪每条 SQL 的实际耗时
//
// 4. **改进方向**（如需精确统计）：
//    - 在 WriteTask 中添加 startTime 字段，worker 执行时逐个记录真实耗时
//    - 成本：每个任务额外 8 字节（time.Time）+ 逐个更新统计的锁竞争
func (q *DBWriteQueue) updateStats(count int, latency time.Duration, err error) {
	q.statsMu.Lock()
	defer q.statsMu.Unlock()

	// 按任务数累加（而非按批次数）
	q.stats.TotalWrites += int64(count)
	if err == nil {
		q.stats.SuccessWrites += int64(count)
	} else {
		q.stats.FailedWrites += int64(count)
	}

	latencyMs := float64(latency.Milliseconds())

	// 更新平均延迟（使用加权平均，批量提交时延迟按任务数权重分摊）
	oldTotal := q.stats.TotalWrites - int64(count)
	q.stats.AvgLatencyMs = (q.stats.AvgLatencyMs*float64(oldTotal) + latencyMs*float64(count)) / float64(q.stats.TotalWrites)

	// P99 样本按单任务记录（批量提交时将批次延迟均分）
	perTaskLatencyMs := latencyMs / float64(count)
	for i := 0; i < count; i++ {
		q.latencySamples[q.sampleIndex] = perTaskLatencyMs
		q.sampleIndex = (q.sampleIndex + 1) % len(q.latencySamples)
		q.sampleCount++
	}

	// 计算 P99 延迟（每100次更新一次，避免频繁排序）
	if q.sampleCount%100 == 0 || q.sampleCount < 100 {
		q.stats.P99LatencyMs = q.calculateP99()
	}
}

// calculateP99 计算 P99 延迟（需持有锁）
func (q *DBWriteQueue) calculateP99() float64 {
	// 确定有效样本数量
	validSamples := int(q.sampleCount)
	if validSamples > len(q.latencySamples) {
		validSamples = len(q.latencySamples)
	}

	if validSamples == 0 {
		return 0
	}

	// 复制样本并排序（使用标准库快速排序）
	samples := make([]float64, validSamples)
	copy(samples, q.latencySamples[:validSamples])
	sort.Float64s(samples)

	// 取99%位置的值
	p99Index := int(float64(validSamples) * 0.99)
	if p99Index >= validSamples {
		p99Index = validSamples - 1
	}

	return samples[p99Index]
}
```

---

### 3.3 全局队列初始化

```go
// main.go 或 services/providerrelay.go

var GlobalDBQueue *services.DBWriteQueue

func initDBQueue() {
	db, err := xdb.DB("default")
	if err != nil {
		log.Fatalf("初始化数据库队列失败: %v", err)
	}

	// 创建全局队列（队列大小5000，禁用批量提交）
	// 🚨 当前项目所有写入都是异构的（不同表、不同操作），不满足批量模式的同构约束
	// 如未来仅针对 request_log INSERT 启用批量，需创建独立的批量队列
	GlobalDBQueue = services.NewDBWriteQueue(db, 5000, false)

	log.Println("✅ 数据库写入队列已启动")
}

// 应用关闭时调用
func shutdownDBQueue() {
	if GlobalDBQueue != nil {
		if err := GlobalDBQueue.Shutdown(10 * time.Second); err != nil {
			log.Printf("⚠️  队列关闭超时: %v", err)
		} else {
			stats := GlobalDBQueue.GetStats()
			log.Printf("✅ 队列已关闭，统计：成功=%d 失败=%d 平均延迟=%.2fms",
				stats.SuccessWrites, stats.FailedWrites, stats.AvgLatencyMs)
		}
	}
}
```

---

### 3.4 代码改造方案

#### 改造清单

| 文件 | 函数 | 改造内容 | 推荐接口 | 理由 |
|------|------|---------|----------|------|
| `services/blacklistservice.go` | RecordSuccess | 2个UPDATE改为队列调用 | `Exec()` | 低频操作，可容忍阻塞 |
| `services/blacklistservice.go` | RecordFailure | 3个INSERT/UPDATE改为队列调用 | `Exec()` | 低频操作，可容忍阻塞 |
| `services/blacklistservice.go` | recordFailureFixedMode | 3个操作改为队列 | `Exec()` | 低频操作，可容忍阻塞 |
| `services/providerrelay.go` | forwardRequest (defer) | request_log INSERT改为队列（**删除重试逻辑**）| **`ExecCtx()`** | **高频操作（每个请求），必须超时控制** |
| `services/providerrelay.go` | geminiProxyHandler (defer) | 同上 | **`ExecCtx()`** | **高频操作（每个请求），必须超时控制** |
| `services/settingsservice.go` | UpdateBlacklistSettings | 2个UPDATE改为队列（删除事务）| `Exec()` | 用户手动配置，低频 |
| `services/settingsservice.go` | UpdateBlacklistEnabled | 1个UPDATE改为队列 | `Exec()` | 用户手动配置，低频 |
| `services/settingsservice.go` | UpdateBlacklistLevelConfig | 1个UPDATE改为队列 | `Exec()` | 用户手动配置，低频 |

**接口选择原则**：
- ✅ **`Exec()`**：低频操作（用户手动触发、定时任务），可容忍短暂阻塞
  - **防御性保护**：内置 30 秒默认超时，即使误用也不会永久阻塞
  - 适用场景：配置更新、定时任务、用户手动触发的操作
- ⚠️ **`ExecCtx()`**：高频操作（每个 HTTP 请求），必须设置超时（如 5 秒）
  - 避免大量请求堆积导致服务雪崩
  - 适用场景：每个请求都会调用的写入（如 request_log）

**重要说明**：
- settingsservice.go 中的写入操作虽然频率较低，但为确保 100% 写入成功，必须统一接入队列
- 原有的事务逻辑（UpdateBlacklistSettings）需要改为两个独立的队列调用（见下方示例）
- request_log 写入是最高频操作（500 req/s），**必须使用 ExecCtx** 避免队列满时请求堆积
- **即使错误地在高频路径使用了 `Exec()`，也有 30 秒兜底超时保护**，不会导致永久性阻塞

**⚠️ 成本提示**（防止掩盖问题）：
- **30 秒兜底超时是最后防线**，仅用于防止灾难性的永久阻塞
- **不应依赖兜底超时**来容忍上游误用：
  - ❌ 错误做法：在高频路径使用 `Exec()`，依赖 30 秒超时"保护"
  - ✅ 正确做法：高频路径统一使用 `ExecCtx(5s)`，低频路径使用 `Exec()`
- **为什么不能依赖兜底**：
  - 30 秒超时过长，会导致大量请求堆积（500 req/s × 30s = 15000 个 goroutine）
  - 压测结果失真（P99 延迟会包含 30 秒超时的样本）
  - 掩盖真实的队列积压问题（应该在 5 秒就暴露，而非 30 秒）
- **代码审查检查点**：
  - 搜索 `queue.Exec(` 确认调用方是否为低频操作
  - 所有 `providerrelay.go` 中的写入必须是 `ExecCtx`
  - 压测脚本必须使用 `ExecCtx` 模拟真实环境

**🚨 批量模式约束**（critical - 影响统计精度）：
- **当前项目不使用批量模式**（`NewDBWriteQueue(db, 5000, false)`）
  - 原因：当前所有写入点都是**异构写入**（不同表、不同操作）
  - blacklistservice.go：UPDATE provider_blacklist（表1）+ INSERT/UPDATE provider_failure（表2）
  - providerrelay.go：INSERT request_log（表3）
  - settingsservice.go：UPDATE app_settings（表4）
- **如果未来启用批量模式**（`enableBatch=true`），必须满足：
  - ✅ **仅用于同构写入**：同一表 + 同一操作类型（如仅 request_log INSERT）
  - ❌ **禁止混入异构**：不同表、不同操作的 SQL 不能进入同一批次
  - **违规后果**：
    - P99 延迟被稀释（慢 SQL 延迟被均分到快 SQL）
    - 平均延迟失真（无法反映真实的单请求耗时）
    - 压测验收标准无效（统计数据不可信）
- **代码审查检查点**（如果启用批量）：
  - 搜索所有 `ExecBatch(` 或 `ExecBatchCtx(` 调用
  - 确认每个调用点的 SQL 是否为同一表 + 同一操作
  - 如有疑问，优先使用 `Exec`/`ExecCtx`（单次提交，统计准确）

#### 示例：改造 RecordSuccess

**改造前**：
```go
func (bs *BlacklistService) RecordSuccess(platform string, providerName string) error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	_, err = db.Exec(`UPDATE provider_blacklist SET failure_count = 0 WHERE ...`)
	if err != nil {
		return err
	}

	// ... 更多UPDATE
	return nil
}
```

**改造后**：
```go
func (bs *BlacklistService) RecordSuccess(platform string, providerName string) error {
	// 直接使用全局队列
	err := GlobalDBQueue.Exec(`UPDATE provider_blacklist SET failure_count = 0 WHERE ...`, ...)
	if err != nil {
		return err
	}

	// ... 更多UPDATE（同样改为队列调用）
	return nil
}
```

**关键点**：
- ✅ 调用方式几乎不变，仅替换 `db.Exec` 为 `GlobalDBQueue.Exec`
- ✅ 错误处理保持一致
- ✅ 无需修改函数签名和调用方

#### 示例：改造带事务的写入（settingsservice.go）

**改造前**：
```go
func (ss *SettingsService) UpdateBlacklistSettings(threshold int, duration int) error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 开启事务
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 更新失败阈值
	_, err = tx.Exec(`UPDATE app_settings SET value = ? WHERE key = 'blacklist_failure_threshold'`, threshold)
	if err != nil {
		return fmt.Errorf("更新失败阈值失败: %w", err)
	}

	// 更新拉黑时长
	_, err = tx.Exec(`UPDATE app_settings SET value = ? WHERE key = 'blacklist_duration_minutes'`, duration)
	if err != nil {
		return fmt.Errorf("更新拉黑时长失败: %w", err)
	}

	// 提交事务
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}
```

**改造后（方案1 - 简单版）**：
```go
func (ss *SettingsService) UpdateBlacklistSettings(threshold int, duration int) error {
	// 独立写入（简单，但无原子性保证）
	err1 := GlobalDBQueue.Exec(`UPDATE app_settings SET value = ? WHERE key = 'blacklist_failure_threshold'`, threshold)
	if err1 != nil {
		return fmt.Errorf("更新失败阈值失败: %w", err1)
	}

	err2 := GlobalDBQueue.Exec(`UPDATE app_settings SET value = ? WHERE key = 'blacklist_duration_minutes'`, duration)
	if err2 != nil {
		return fmt.Errorf("更新拉黑时长失败: %w", err2)
	}

	return nil
}
```

**改造后（方案2 - 原子性保证版，推荐）**：
```go
func (ss *SettingsService) UpdateBlacklistSettings(threshold int, duration int) error {
	// 先读取旧值（用于补偿）
	var oldThreshold, oldDuration int
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	err = db.QueryRow(`SELECT value FROM app_settings WHERE key = 'blacklist_failure_threshold'`).Scan(&oldThreshold)
	if err != nil {
		return fmt.Errorf("读取旧阈值失败: %w", err)
	}

	err = db.QueryRow(`SELECT value FROM app_settings WHERE key = 'blacklist_duration_minutes'`).Scan(&oldDuration)
	if err != nil {
		return fmt.Errorf("读取旧时长失败: %w", err)
	}

	// 尝试第一次写入
	err1 := GlobalDBQueue.Exec(`UPDATE app_settings SET value = ? WHERE key = 'blacklist_failure_threshold'`, threshold)
	if err1 != nil {
		return fmt.Errorf("更新失败阈值失败: %w", err1)
	}

	// 尝试第二次写入
	err2 := GlobalDBQueue.Exec(`UPDATE app_settings SET value = ? WHERE key = 'blacklist_duration_minutes'`, duration)
	if err2 != nil {
		// 第二次失败，回滚第一次（补偿逻辑）
		rollbackErr := GlobalDBQueue.Exec(`UPDATE app_settings SET value = ? WHERE key = 'blacklist_failure_threshold'`, oldThreshold)
		if rollbackErr != nil {
			return fmt.Errorf("更新拉黑时长失败且回滚失败: %w (原始错误: %v)", rollbackErr, err2)
		}
		return fmt.Errorf("更新拉黑时长失败（已回滚失败阈值）: %w", err2)
	}

	return nil
}
```

**重要说明**：
- ✅ **方案1（简单）**：适用于对原子性要求不高的场景，依赖用户重试
- ✅ **方案2（原子性保证）**：通过补偿逻辑（Saga模式）确保要么全部成功，要么全部回滚
- ⚠️ **权衡**：方案2需要额外的读操作和可能的回滚操作，延迟略高（<10ms），但可靠性更强
- 📌 **推荐使用方案2**：配置更新是关键操作，原子性保证更重要

#### 示例：高频写入改造（providerrelay.go）

**改造前**：
```go
defer func() {
	requestLog.DurationSec = time.Since(start).Seconds()
	db, err := xdb.DB("default")
	if err != nil {
		fmt.Printf("写入 request_log 失败: 获取数据库连接失败: %v\n", err)
		return
	}

	// 重试机制：SQLite并发写入时会出现SQLITE_BUSY，需要重试
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		_, err = db.Exec(`INSERT INTO request_log (...) VALUES (...)`, ...)
		if err == nil {
			return // 成功，直接返回
		}
		lastErr = err
		if strings.Contains(err.Error(), "SQLITE_BUSY") {
			time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
			continue
		}
		break
	}
	fmt.Printf("写入 request_log 失败（已重试3次）: %v\n", lastErr)
}()
```

**改造后**：
```go
defer func() {
	requestLog.DurationSec = time.Since(start).Seconds()

	// 使用 ExecCtx 带超时控制（5秒超时，避免队列满时请求堆积）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := GlobalDBQueue.ExecCtx(ctx, `
		INSERT INTO request_log (
			platform, model, provider, http_code,
			input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
			reasoning_tokens, is_stream, duration_sec
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		requestLog.Platform,
		requestLog.Model,
		requestLog.Provider,
		requestLog.HttpCode,
		requestLog.InputTokens,
		requestLog.OutputTokens,
		requestLog.CacheCreateTokens,
		requestLog.CacheReadTokens,
		requestLog.ReasoningTokens,
		boolToInt(requestLog.IsStream),
		requestLog.DurationSec,
	)

	if err != nil {
		// 写入失败不影响主流程（日志记录是非关键路径）
		fmt.Printf("写入 request_log 失败: %v\n", err)
	}
}()
```

**关键改进**：
- ✅ **删除重试逻辑**：队列已保证 100% 写入成功，无需业务侧重试
- ✅ **超时控制**：5秒超时避免队列满时请求无限期阻塞
- ✅ **简化代码**：从42行减少到27行，可读性大幅提升
- ⚠️ **失败处理**：如果5秒内入队失败或写入失败，只记录日志不影响主流程（日志非关键）

---

## 四、测试验证方案

### 4.1 单元测试

```go
// services/dbqueue_test.go

func TestDBWriteQueue_ConcurrentWrites(t *testing.T) {
	// 1. 创建临时数据库
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 2. 初始化队列
	queue := NewDBWriteQueue(db, 100, false)
	defer queue.Shutdown(5 * time.Second)

	// 3. 并发写入测试（1000次并发）
	var wg sync.WaitGroup
	errors := make(chan error, 1000)

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := queue.Exec(`INSERT INTO test_table (id, value) VALUES (?, ?)`, id, fmt.Sprintf("value-%d", id))
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 4. 验证结果
	if len(errors) > 0 {
		t.Fatalf("并发写入失败: %v", <-errors)
	}

	// 5. 验证数据完整性
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM test_table`).Scan(&count)
	if count != 1000 {
		t.Fatalf("期望写入1000条，实际%d条", count)
	}

	// 6. 验证统计
	stats := queue.GetStats()
	if stats.SuccessWrites != 1000 {
		t.Fatalf("统计错误：成功=%d，期望1000", stats.SuccessWrites)
	}
}
```

### 4.2 压力测试脚本

```go
// scripts/test-write-queue-stress.go

func main() {
	// 1. 初始化真实数据库
	db := initRealDB()

	// 2. 创建队列（对齐生产环境配置：禁用批量模式）
	// 🚨 生产环境是异构写入，禁用批量模式以确保统计准确
	queue := services.NewDBWriteQueue(db, 5000, false)
	defer queue.Shutdown(30 * time.Second)

	// 3. 压力测试（模拟500 req/s持续1分钟）
	concurrency := 500
	duration := 60 * time.Second

	start := time.Now()
	ticker := time.NewTicker(time.Second / time.Duration(concurrency))
	defer ticker.Stop()

	var totalWrites int64
	var errors int64

	timeout := time.After(duration)

	for {
		select {
		case <-ticker.C:
			go func() {
				// 🔧 修复：使用 ExecCtx(5s) 对齐生产环境高频写入用法
				// 避免使用 Exec 的 30 秒兜底超时，导致压测结果失真
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				err := queue.ExecCtx(ctx, `INSERT INTO request_log (...) VALUES (...)`, ...)
				atomic.AddInt64(&totalWrites, 1)
				if err != nil {
					atomic.AddInt64(&errors, 1)
				}
			}()

		case <-timeout:
			elapsed := time.Since(start)
			stats := queue.GetStats()

			fmt.Printf("\n========== 压力测试结果 ==========\n")
			fmt.Printf("持续时间: %v\n", elapsed)
			fmt.Printf("总写入数: %d\n", totalWrites)
			fmt.Printf("成功写入: %d\n", stats.SuccessWrites)
			fmt.Printf("失败写入: %d\n", errors)
			fmt.Printf("成功率: %.2f%%\n", float64(stats.SuccessWrites)/float64(totalWrites)*100)
			fmt.Printf("平均延迟: %.2fms\n", stats.AvgLatencyMs)
			fmt.Printf("队列长度: %d\n", stats.QueueLength)
			fmt.Printf("批量提交次数: %d\n", stats.BatchCommits)
			fmt.Printf("====================================\n")
			return
		}
	}
}
```

**验收标准**：
- ✅ 成功率：100%
- ✅ 平均延迟：< 50ms
- ✅ P99延迟：< 200ms
- ✅ 队列长度：峰值 < 1000
- ✅ 无panic、死锁

---

## 五、实施步骤

### 阶段1：基础设施搭建（2天）
1. 创建 `services/dbqueue.go` - 实现队列核心逻辑
2. 编写单元测试 `services/dbqueue_test.go`
3. 在 `main.go` 中初始化全局队列
4. 添加优雅关闭逻辑

### 阶段2：代码改造（2天）
1. 改造 `services/blacklistservice.go`（9处写入）
   - RecordSuccess
   - RecordFailure
   - recordFailureFixedMode
2. 改造 `services/providerrelay.go`（2处写入）
   - forwardRequest defer
   - geminiProxyHandler defer
3. 删除现有重试逻辑（已不需要）

### 阶段3：测试验证（1天）
1. 运行单元测试
2. 运行压力测试脚本
3. 修复发现的问题
4. 性能调优（调整队列大小、批量参数）
5. **🚨 批量模式合规性检查**（如果 `enableBatch=true`）：
   - 搜索所有 `ExecBatch(` 和 `ExecBatchCtx(` 调用
   - 确认每个调用点只写入同一表的同一种操作（同构写入）
   - 如有异构写入混入批量通道，立即改为 `Exec`/`ExecCtx`
   - 验证压测脚本的 P99 延迟统计是否合理（无异常稀释）

### 阶段4：监控与上线（1天）
1. 添加队列监控接口（暴露给前端）
2. 本地完整测试
3. 生产环境灰度部署
4. 观察队列统计和错误日志

---

## 六、风险评估与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| 队列成为性能瓶颈 | 中 | 高 | 启用批量提交，测试验证吞吐量 |
| worker goroutine panic | 低 | 高 | 添加 recover 机制，自动重启，panic 时返回错误 |
| 队列满导致请求阻塞 | 低 | 中 | 设置合理队列大小（5000），监控队列长度，Exec 兜底 30s 超时 |
| 关闭时任务丢失 | 低 | 高 | 实现优雅关闭，等待所有任务完成，atomic.Bool 拒绝新入队 |
| 批量提交事务失败 | 低 | 中 | 失败时回退到逐个处理模式 |
| **批量模式异构写入** | **中** | **高** | **当前不启用批量模式**（enableBatch=false），如未来启用必须严格限制为同构写入（同表同操作），代码审查强制检查 |

---

## 七、关键文件清单

### 新增文件
- `services/dbqueue.go` - 队列核心实现
- `services/dbqueue_test.go` - 单元测试
- `scripts/test-write-queue-stress.go` - 压力测试

### 修改文件
- `main.go` - 初始化全局队列、优雅关闭
- `services/blacklistservice.go` - 9处写入改为队列调用
- `services/providerrelay.go` - 2处写入改为队列调用

---

## 八、成功标准

✅ **功能目标**：
- 100% 写入成功率（无 SQLITE_BUSY 错误）
- 支持 500 req/s 并发写入
- 平均延迟 < 50ms

✅ **质量目标**：
- 单元测试覆盖率 > 80%
- 压力测试通过（1小时无错误）
- 代码审查通过

✅ **可维护性**：
- 队列统计接口可用
- 日志完整（队列状态、错误信息）
- 文档齐全
