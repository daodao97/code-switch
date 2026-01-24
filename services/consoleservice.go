package services

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// ConsoleLog 控制台日志条目
type ConsoleLog struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // INFO, WARN, ERROR
	Message   string    `json:"message"`
}

// ConsoleService 控制台日志服务
type ConsoleService struct {
	logs         []ConsoleLog
	mutex        sync.RWMutex
	maxLogs      int
	writer       *consoleWriter
	oldStdout    *os.File
	oldStderr    *os.File
	pauseLogging bool // 暂停日志捕获标志
}

// consoleWriter 自定义 writer，同时写入控制台和缓存
type consoleWriter struct {
	service *ConsoleService
	level   string
	output  io.Writer
}

func (w *consoleWriter) Write(p []byte) (n int, err error) {
	// 写入原始输出
	n, err = w.output.Write(p)

	// 添加到日志缓存
	w.service.addLog(w.level, string(p))

	return n, err
}

func NewConsoleService() *ConsoleService {
	cs := &ConsoleService{
		logs:    make([]ConsoleLog, 0, 1000),
		maxLogs: 1000, // 最多保留 1000 条日志
	}

	// 捕获标准输出和标准错误
	cs.captureStdout()

	return cs
}

// captureStdout 捕获标准输出和标准错误
func (cs *ConsoleService) captureStdout() {
	// 保存原始输出
	cs.oldStdout = os.Stdout
	cs.oldStderr = os.Stderr

	// 创建管道
	stdoutReader, stdoutWriter, _ := os.Pipe()
	stderrReader, stderrWriter, _ := os.Pipe()

	// 替换标准输出
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	log.SetOutput(stdoutWriter)

	// 启动 goroutine 读取管道内容
	go cs.readPipe(stdoutReader, "INFO", cs.oldStdout)
	go cs.readPipe(stderrReader, "ERROR", cs.oldStderr)
}

// readPipe 读取管道内容
func (cs *ConsoleService) readPipe(reader *os.File, level string, output *os.File) {
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(output, "读取管道失败: %v\n", err)
			}
			return
		}

		if n > 0 {
			msg := string(buf[:n])
			// 写入原始输出
			output.Write(buf[:n])
			// 添加到日志缓存
			cs.addLog(level, msg)
		}
	}
}

// addLog 添加日志到缓存
func (cs *ConsoleService) addLog(level, message string) {
	// 如果暂停日志捕获，直接返回
	if cs.pauseLogging {
		return
	}

	// 过滤 Wails 框架的调试日志，避免日志递归
	if shouldFilterLog(message) {
		return
	}

	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	log := ConsoleLog{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	}

	cs.logs = append(cs.logs, log)

	// 限制日志数量
	if len(cs.logs) > cs.maxLogs {
		cs.logs = cs.logs[len(cs.logs)-cs.maxLogs:]
	}

	// 清理3天前的日志
	cs.cleanOldLogs()
}

// cleanOldLogs 清理3天前的日志
func (cs *ConsoleService) cleanOldLogs() {
	// 无需加锁，因为调用者 addLog 已经加锁
	threeDaysAgo := time.Now().Add(-72 * time.Hour)

	// 找到第一个在3天内的日志索引
	cutoffIndex := 0
	for i, log := range cs.logs {
		if log.Timestamp.After(threeDaysAgo) {
			cutoffIndex = i
			break
		}
	}

	// 如果有旧日志需要清理
	if cutoffIndex > 0 {
		cs.logs = cs.logs[cutoffIndex:]
		fmt.Printf("[ConsoleService] 清理了 %d 条超过3天的日志\n", cutoffIndex)
	}
}

// GetLogs 获取所有日志
func (cs *ConsoleService) GetLogs() []ConsoleLog {
	// 暂停日志捕获，避免 GetLogs 本身产生的日志被记录（导致递归）
	cs.pauseLogging = true
	defer func() { cs.pauseLogging = false }()

	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	// 返回副本
	result := make([]ConsoleLog, len(cs.logs))
	copy(result, cs.logs)
	return result
}

// GetRecentLogs 获取最近 N 条日志
func (cs *ConsoleService) GetRecentLogs(count int) []ConsoleLog {
	// 暂停日志捕获，避免递归
	cs.pauseLogging = true
	defer func() { cs.pauseLogging = false }()

	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	if count <= 0 {
		count = 100
	}

	if count > len(cs.logs) {
		count = len(cs.logs)
	}

	// 返回最后 N 条
	result := make([]ConsoleLog, count)
	copy(result, cs.logs[len(cs.logs)-count:])
	return result
}

// ClearLogs 清空日志
func (cs *ConsoleService) ClearLogs() {
	// 暂停日志捕获，避免递归
	cs.pauseLogging = true
	defer func() { cs.pauseLogging = false }()

	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	cs.logs = make([]ConsoleLog, 0, 1000)
}

// shouldFilterLog 判断是否应该过滤这条日志
// 过滤掉 Wails 框架的调试日志和 JSON 序列化日志，避免日志递归爆炸
func shouldFilterLog(message string) bool {
	// 1. 过滤掉包含大量反斜杠的日志（JSON 序列化递归）
	// 正常日志不应该有超过 10 个连续的反斜杠
	if strings.Contains(message, "\\\\\\\\\\\\\\\\\\\\") {
		return true
	}

	// 2. 过滤掉包含 JSON 结构的日志（GetLogs 的返回值被序列化）
	// 检测是否包含日志的 JSON 结构特征
	if strings.Contains(message, `"timestamp":`) &&
	   strings.Contains(message, `"level":`) &&
	   strings.Contains(message, `"message":`) {
		return true
	}

	// 3. 过滤 Wails 框架的内部日志
	filterKeywords := []string{
		"Binding call started",
		"Binding call complete",
		"Asset Request",
		"INF Binding call",
		"INF Asset Request",
		"/wails/runtime",
		"ConsoleService.GetLogs",
		"ConsoleService.GetRecentLogs",
		"ConsoleService.ClearLogs",
	}

	for _, keyword := range filterKeywords {
		if strings.Contains(message, keyword) {
			return true
		}
	}

	return false
}
