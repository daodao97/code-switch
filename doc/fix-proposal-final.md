# Code-Switch 问题修复方案 - 最终版

> **文档版本**：v1.4 Final (二次审核修正版)
> **创建时间**：2025-11-28
> **最后更新**：2025-11-28 (补充 6 个实现层注意事项)
> **问题总数**：22 个
> **修复阶段**：4 个阶段

---

## ⚠️ v1.4 二次审核补充说明

### v1.3 + v1.4 已修正的问题

| 问题 | 修正内容 | 版本 |
|------|----------|------|
| 数据库初始化重复 | 1.1 仅创建目录，移除 xdb.Inits；集中到 1.2 | v1.3 |
| ProxyConfig 未持久化 | AuthToken 持久化到配置文件 | v1.3 |
| 空 Token 兼容风险 | 添加显式开关 + 告警日志 | v1.3 |
| IP 校验不完整 | 补充 0.0.0.0/::、组播、更多云厂商地址 | v1.3 |
| 流式路径回退风险 | 流式请求不做 Provider 回退 | v1.3 |
| 配置迁移顺序 | 先写新文件 → 校验 → 标记 → 删旧 | v1.3 |
| xrequest API 错误 | 使用 ToHttpResponseWriter 替代不存在的 Body() | v1.3 |
| **_internal 端点死锁** | `/_internal/*` 在中间件开头放行（仅 localhost） | **v1.4** |

### v1.4 实现层注意事项（必读）

| # | 风险点 | 说明 | 落地要求 |
|---|--------|------|----------|
| 1 | **自动更新安全链路** | 文档已定义 PrepareUpdate/ConfirmAndApplyUpdate 接口和备份/回滚机制，但 macOS 明确返回"未实现"错误。 | 落地时需：① Windows 测试完整流程；② macOS 保持禁用状态直到签名验证实现；③ 回滚测试用例 |
| 2 | **SSRF 防护接线** | `buildSafeClient` 已设置 `DialContext: s.safeDialContext`（见 2.3 节），但需确保所有 HTTP 调用都使用此 client | 落地时需：搜索 `http.Client{}` 确保无遗漏 |
| 3 | **initProxyConfig 调用时机** | `proxyConfig` 初始化必须在 `registerRoutes` 之前，否则 nil panic | 落地时需：在 `main.go` 的 `NewProviderRelayService` 构造前调用 `initProxyConfig()` |
| 4 | **流式不回退策略** | 设计决策：流式请求只尝试第一个 Provider，失败直接返回 502 | ⚠️ **需产品确认**：流式无法回滚是技术限制，但可能影响用户体验；发布说明需提及 |
| 5 | **认证默认开启 Breaking Change** | 老用户升级后首次请求会被拒绝（401） | 落地时需：① 升级脚本自动创建 `proxy-config.json` 并设 `AllowUnauthenticated=true`；② 发布说明强调 |
| 6 | **技能仓库安全** | 2.5 节已完整定义限制（100MB ZIP、路径穿越、Zip bomb、文件类型白名单） | 落地时需：验证 `validateZipPath` 和 `isAllowedFileType` 已被调用 |
| 7 | **~~_internal 端点死锁~~** | ~~已修复~~：`/_internal/*` 路径在中间件开头放行（仅限 localhost） | ✅ 已在 2.2 节 securityMiddleware 中修复 |

### 初始化顺序要求（main.go）

```go
func main() {
    // 1. 初始化代理配置（必须最先，否则 proxyConfig 为 nil）
    if err := initProxyConfig(); err != nil {
        log.Fatalf("❌ 初始化代理配置失败: %v", err)
    }

    // 2. 初始化数据库
    if err := services.InitDatabase(); err != nil {
        log.Fatalf("❌ 数据库初始化失败: %v", err)
    }

    // 3. 初始化写入队列
    if err := services.InitGlobalDBQueue(); err != nil {
        log.Fatalf("❌ 初始化数据库队列失败: %v", err)
    }

    // 4. 创建服务（此时 proxyConfig 和数据库都已就绪）
    providerService := services.NewProviderService()
    // ...
    providerRelay := services.NewProviderRelayService(...)
}
```

### 升级兼容性（Breaking Change 处理）

对于 v1.3 之前的用户，首次升级时需要：

```go
// services/migration.go

// MigrateToV14 v1.4 升级迁移
func MigrateToV14() error {
    home, _ := os.UserHomeDir()
    configPath := filepath.Join(home, ".code-switch", "proxy-config.json")

    // 如果配置不存在，创建兼容配置（允许无认证访问）
    if _, err := os.Stat(configPath); os.IsNotExist(err) {
        compatConfig := &ProxyConfig{
            AuthToken:            generateSecureToken(),
            AllowReverseProxy:    false,
            RequireAuth:          true,
            AllowUnauthenticated: true,  // 【关键】老用户默认允许，避免 breaking
        }

        data, _ := json.MarshalIndent(compatConfig, "", "  ")
        os.WriteFile(configPath, data, 0600)

        log.Printf("⚠️  [升级迁移] 已创建兼容配置，AllowUnauthenticated=true")
        log.Printf("⚠️  [安全建议] 生产环境建议设置 AllowUnauthenticated=false")
    }

    return nil
}
```

---

## 目录

1. [修复策略总览](#修复策略总览)
2. [阶段一：阻断级问题](#阶段一阻断级问题)
3. [阶段二：安全漏洞](#阶段二安全漏洞)
4. [阶段三：核心逻辑](#阶段三核心逻辑)
5. [阶段四：功能完善](#阶段四功能完善)
6. [测试验证计划](#测试验证计划)
7. [回滚策略](#回滚策略)

---

## 修复策略总览

### 修复原则

1. **最小改动原则**：每个修复尽量只修改必要的代码，避免引入新问题
2. **向后兼容**：修复不应破坏现有功能或用户数据
3. **防御性编程**：在关键路径添加空指针检查和错误处理
4. **可测试性**：每个修复都应有对应的验证方法

### 问题统计

| 阶段 | 问题数 | 关键修复 | 风险等级 |
|------|--------|----------|----------|
| 阶段一 | 4 | 数据库初始化、拼写错误 | 低（不影响现有数据） |
| 阶段二 | 6 | 127.0.0.1 监听、SSRF 防护、自动更新安全、代理认证、技能仓库安全 | 高（需验证远程访问） |
| 阶段三 | 5 | 故障切换、错误分类、Context 隔离 | 中（需回归测试） |
| 阶段四 | 7 | 路径引号、判空保护、配置迁移 | 低（边缘场景） |

### 依赖关系图

```
阶段一（阻断级）
├── [1] 创建 SQLite 父目录 ──────┐
├── [2] 数据库初始化前移 ────────┼─→ 应用能正常启动
├── [3] SuiStore 错误处理 ───────┤
└── [4] 配置目录拼写修正 ────────┘

阶段二（安全漏洞）
├── [5] 代理仅监听 127.0.0.1 ───┐
├── [6] 代理认证与反代防护 ─────┼─→ 阻止 API Key 泄露
├── [7] SSRF + DNS 重绑定防护 ──┤
├── [8] 自动更新完整安全方案 ───┼─→ 防止中间人攻击
└── [9] 技能仓库下载安全 ───────┘

阶段三（核心逻辑）
├── [10] 请求转发故障切换 ──────┐
├── [11] Context 隔离 ──────────┼─→ 核心功能正常
├── [12] 区分可重试错误 ────────┤
├── [13] 黑名单假恢复修复 ──────┤
└── [14] HTTP 连接泄漏 ─────────┘

阶段四（功能完善）
├── [15] Windows 自启动引号
├── [16] 日志写入判空（含 Gemini）
├── [17] 等级拉黑配置持久化
├── [18] 配置迁移完整性
└── [19] 可观测性增强
```

---

## 阶段一：阻断级问题

### 1.1 创建 SQLite 父目录

**问题位置**：`services/providerrelay.go:37-43`

**问题描述**：SQLite 不会自动创建父目录 `~/.code-switch/`，首次启动时 `xdb.Inits()` 会因目录不存在而失败。

**修复方案**：

> ⚠️ **v1.3 修正**：本节仅负责创建目录，`xdb.Inits()` 已移至 1.2 的 `InitDatabase()` 统一处理，避免重复初始化。

```go
// services/providerrelay.go - NewProviderRelayService 函数
// 【v1.3 修正】移除所有数据库初始化代码，仅保留服务构造

func NewProviderRelayService(providerService *ProviderService, geminiService *GeminiService, blacklistService *BlacklistService, addr string) *ProviderRelayService {
	if addr == "" {
		addr = "127.0.0.1:18100"  // 【安全修复】仅监听本地
	}

	// 【v1.3 修正】数据库初始化已移至 main.go 的 InitDatabase()
	// 此处不再调用 xdb.Inits()、ensureRequestLogTable()、ensureBlacklistTables()

	return &ProviderRelayService{
		providerService:  providerService,
		geminiService:    geminiService,
		blacklistService: blacklistService,
		addr:             addr,
	}
}
```

**验证方法**：
```bash
# 删除配置目录后启动应用
rm -rf ~/.code-switch
./CodeSwitch
# 预期：应用正常启动，目录由 InitDatabase() 自动创建
```

---

### 1.2 数据库初始化时序重构

**问题位置**：`main.go:72-74` 和 `services/dbqueue.go:26-30`

**问题描述**：
- `InitGlobalDBQueue()` 在第 72 行调用 `xdb.DB("default")`
- 但 `xdb.Inits()` 在 `NewProviderRelayService()` 内部（第 81 行）才执行
- 结果：`xdb.DB("default")` 返回错误，`log.Fatalf` 终止进程

**修复方案**：将数据库初始化提取为独立函数

```go
// services/database.go（新建文件）

package services

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"
)

// InitDatabase 初始化数据库连接（必须在所有服务构造之前调用）
func InitDatabase() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}

	// 1. 确保配置目录存在
	configDir := filepath.Join(home, ".code-switch")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	// 2. 初始化 xdb 连接池
	dbPath := filepath.Join(configDir, "app.db?cache=shared&mode=rwc&_busy_timeout=10000&_journal_mode=WAL")
	if err := xdb.Inits([]xdb.Config{
		{
			Name:   "default",
			Driver: "sqlite",
			DSN:    dbPath,
		},
	}); err != nil {
		return fmt.Errorf("初始化数据库失败: %w", err)
	}

	// 3. 确保表结构存在
	if err := ensureRequestLogTable(); err != nil {
		return fmt.Errorf("初始化 request_log 表失败: %w", err)
	}
	if err := ensureBlacklistTables(); err != nil {
		return fmt.Errorf("初始化黑名单表失败: %w", err)
	}

	// 4. 预热连接池
	db, err := xdb.DB("default")
	if err == nil && db != nil {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM request_log").Scan(&count); err != nil {
			fmt.Printf("⚠️  连接池预热查询失败: %v\n", err)
		} else {
			fmt.Printf("✅ 数据库连接已预热（request_log 记录数: %d）\n", count)
		}
	}

	return nil
}
```

**修改 main.go**：

```go
// main.go

func main() {
	appservice := &AppService{}

	// 【修复】第一步：初始化数据库（必须最先执行）
	if err := services.InitDatabase(); err != nil {
		log.Fatalf("❌ 数据库初始化失败: %v", err)
	}
	log.Println("✅ 数据库已初始化")

	// 【修复】第二步：初始化写入队列（依赖数据库连接）
	if err := services.InitGlobalDBQueue(); err != nil {
		log.Fatalf("❌ 初始化数据库队列失败: %v", err)
	}
	log.Println("✅ 数据库写入队列已启动")

	// 第三步：创建服务（现在可以安全使用数据库了）
	suiService, errt := services.NewSuiStore()
	if errt != nil {
		log.Fatalf("❌ SuiStore 初始化失败: %v", errt)  // 【修复】不再忽略错误
	}

	providerService := services.NewProviderService()
	settingsService := services.NewSettingsService()
	// ... 其余服务构造保持不变

	// 【修复】从 NewProviderRelayService 中移除数据库初始化代码
	providerRelay := services.NewProviderRelayService(providerService, geminiService, blacklistService, ":18100")
	// ...
}
```

**同步修改 providerrelay.go**：

```go
// services/providerrelay.go - 移除数据库初始化代码

func NewProviderRelayService(providerService *ProviderService, geminiService *GeminiService, blacklistService *BlacklistService, addr string) *ProviderRelayService {
	if addr == "" {
		addr = "127.0.0.1:18100"  // 【安全修复】同时修改为仅监听本地
	}

	// 【修复】移除所有数据库初始化代码，已移至 InitDatabase()
	// 原有的 xdb.Inits()、ensureRequestLogTable()、ensureBlacklistTables() 全部删除

	return &ProviderRelayService{
		providerService:  providerService,
		geminiService:    geminiService,
		blacklistService: blacklistService,
		addr:             addr,
	}
}
```

**验证方法**：
```bash
# 删除数据库后启动
rm ~/.code-switch/app.db
./CodeSwitch
# 预期：数据库和表结构自动创建，无 panic
```

---

### 1.3 SuiStore 错误处理

**问题位置**：`main.go:66-70`

**问题描述**：`NewSuiStore()` 返回的错误被完全忽略，`suiService` 可能为 nil 但仍被注册到 Wails。

**修复方案**（已在 1.2 中包含）：

```go
suiService, errt := services.NewSuiStore()
if errt != nil {
	log.Fatalf("❌ SuiStore 初始化失败: %v", errt)
}
```

---

### 1.4 配置目录拼写错误修正

**问题位置**：`services/appsettings.go:10-11`

**问题描述**：
```go
const (
	appSettingsDir  = ".codex-swtich"  // ← 拼写错误！应为 .code-switch
	appSettingsFile = "app.json"
)
```

用户设置保存到错误路径，导致每次启动都读不到配置。

**修复方案**：

```go
// services/appsettings.go

const (
	appSettingsDir     = ".code-switch"  // 【修复】修正拼写
	appSettingsFile    = "app.json"
	oldSettingsDir     = ".codex-swtich"  // 旧的错误拼写
	migrationMarkerFile = ".migrated-from-codex-swtich"
)
```

**数据迁移**（完整版，含标记和清理）：

```go
// services/appsettings.go - NewAppSettingsService 函数

func NewAppSettingsService(autoStartService *AutoStartService) *AppSettingsService {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	newDir := filepath.Join(home, appSettingsDir)
	newPath := filepath.Join(newDir, appSettingsFile)
	oldDir := filepath.Join(home, oldSettingsDir)
	oldPath := filepath.Join(oldDir, appSettingsFile)
	markerPath := filepath.Join(newDir, migrationMarkerFile)

	// 检查是否已经迁移过
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		// 尚未迁移，检查旧目录
		if _, err := os.Stat(oldPath); err == nil {
			// 旧文件存在，执行迁移
			if err := migrateSettings(oldPath, newPath, oldDir, markerPath); err != nil {
				fmt.Printf("[AppSettings] ⚠️  迁移配置失败: %v\n", err)
			}
		}
	}

	return &AppSettingsService{
		path:             newPath,
		autoStartService: autoStartService,
	}
}

// migrateSettings 完整的配置迁移
// 【v1.3 修正】迁移顺序：写新文件 → 校验 → 标记 → 删旧
func migrateSettings(oldPath, newPath, oldDir, markerPath string) error {
	// 1. 确保新目录存在
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return fmt.Errorf("创建新目录失败: %w", err)
	}

	// 2. 检查新文件是否已存在
	if _, err := os.Stat(newPath); err == nil {
		// 新文件已存在，不覆盖，但仍创建迁移标记
		fmt.Printf("[AppSettings] 新配置文件已存在，跳过迁移\n")
	} else {
		// 3. 读取旧配置
		data, err := os.ReadFile(oldPath)
		if err != nil {
			return fmt.Errorf("读取旧配置失败: %w", err)
		}

		// 4. 写入新配置
		if err := os.WriteFile(newPath, data, 0644); err != nil {
			return fmt.Errorf("写入新配置失败: %w", err)
		}

		// 5. 【v1.3 新增】校验新文件
		verifyData, err := os.ReadFile(newPath)
		if err != nil {
			// 写入成功但读取失败，回滚
			os.Remove(newPath)
			return fmt.Errorf("校验新配置失败（已回滚）: %w", err)
		}

		// 校验内容一致性
		if !bytes.Equal(data, verifyData) {
			os.Remove(newPath)
			return fmt.Errorf("配置内容校验失败（已回滚）: 写入内容与读取内容不一致")
		}

		// 如果是 JSON 文件，额外校验 JSON 格式有效性
		if strings.HasSuffix(newPath, ".json") {
			var jsonTest interface{}
			if err := json.Unmarshal(verifyData, &jsonTest); err != nil {
				os.Remove(newPath)
				return fmt.Errorf("JSON 格式校验失败（已回滚）: %w", err)
			}
		}

		fmt.Printf("[AppSettings] ✅ 已迁移并校验配置: %s → %s\n", oldPath, newPath)
	}

	// 6. 创建迁移标记文件
	markerContent := fmt.Sprintf("迁移时间: %s\n旧路径: %s\n", time.Now().Format(time.RFC3339), oldDir)
	if err := os.WriteFile(markerPath, []byte(markerContent), 0644); err != nil {
		return fmt.Errorf("创建迁移标记失败: %w", err)
	}

	// 7. 【v1.3 修正】只有在新文件校验通过后才删除旧目录
	if err := os.RemoveAll(oldDir); err != nil {
		// 删除失败不是致命错误，只记录警告
		fmt.Printf("[AppSettings] ⚠️  删除旧目录失败: %v（可手动删除 %s）\n", err, oldDir)
	} else {
		fmt.Printf("[AppSettings] ✅ 已删除旧目录: %s\n", oldDir)
	}

	return nil
}
```

**验证方法**：
```bash
# 检查是否读取正确路径
cat ~/.code-switch/app.json
# 预期：设置能正确保存和读取
```

---

## 阶段二：安全漏洞

### 2.1 代理服务仅监听 127.0.0.1

**问题位置**：`services/providerrelay.go:32-35, 89-91`

**问题描述**：
- 默认监听 `:18100` = `0.0.0.0:18100`（所有网卡）
- 同网段任何设备可访问，直接借用本地 API Key

**安全等级**：🔴 **灾难级** - API Key 全网暴露

**修复方案**：

```go
// services/providerrelay.go

func NewProviderRelayService(providerService *ProviderService, geminiService *GeminiService, blacklistService *BlacklistService, addr string) *ProviderRelayService {
	if addr == "" {
		addr = "127.0.0.1:18100"  // 【安全修复】仅监听本地回环地址
	}
	// ...
}
```

---

### 2.2 代理认证与反代防护

**问题分析**：即使监听 `127.0.0.1`，用户可能通过 Nginx/FRP 将端口暴露到公网。

**修复方案**：

```go
// services/providerrelay.go

import (
	"crypto/rand"
	"encoding/base64"
)

// ProxyConfig 代理安全配置
type ProxyConfig struct {
	// 认证 Token（从配置文件读取，首次启动时生成）
	AuthToken string `json:"auth_token"`
	// 是否允许被反向代理（默认 false）
	AllowReverseProxy bool `json:"allow_reverse_proxy"`
	// 是否要求认证（默认 true）
	RequireAuth bool `json:"require_auth"`
	// 【v1.3 新增】是否允许无认证访问（必须显式设置，默认 false）
	// 与 RequireAuth=false 不同，此字段要求用户明确知晓风险
	AllowUnauthenticated bool `json:"allow_unauthenticated"`
}

// 全局代理配置（延迟初始化，从配置文件加载）
var proxyConfig *ProxyConfig

// initProxyConfig 初始化代理配置（【v1.3 修正】从配置文件加载，保证 Token 持久化）
func initProxyConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}

	configPath := filepath.Join(home, ".code-switch", "proxy-config.json")

	// 尝试加载现有配置
	if data, err := os.ReadFile(configPath); err == nil {
		config := &ProxyConfig{}
		if err := json.Unmarshal(data, config); err == nil {
			proxyConfig = config
			log.Printf("✅ 已加载代理配置（Token: %s...）", proxyConfig.AuthToken[:8])
			return nil
		}
	}

	// 配置不存在或无效，生成新配置
	proxyConfig = &ProxyConfig{
		AuthToken:            generateSecureToken(),
		AllowReverseProxy:    false,
		RequireAuth:          true,
		AllowUnauthenticated: false,
	}

	// 【v1.3 修正】持久化配置到文件
	if err := saveProxyConfig(configPath); err != nil {
		return fmt.Errorf("保存代理配置失败: %w", err)
	}

	log.Printf("✅ 已生成新的代理配置（Token: %s...）", proxyConfig.AuthToken[:8])
	return nil
}

// saveProxyConfig 保存代理配置到文件
func saveProxyConfig(configPath string) error {
	data, err := json.MarshalIndent(proxyConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0600) // 限制权限，仅所有者可读写
}

// generateSecureToken 生成安全 Token
func generateSecureToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// securityMiddleware 安全中间件
func securityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 【v1.4 修正】内部端点放行（仅限本地回环 IP）
		// 解决"鸡生蛋"死锁：用户需要先获取 Token 才能通过认证
		if strings.HasPrefix(c.Request.URL.Path, "/_internal/") {
			clientIP := c.ClientIP()
			if clientIP == "127.0.0.1" || clientIP == "::1" {
				c.Next()
				return
			}
			// 非本地 IP 访问内部端点，拒绝
			c.JSON(http.StatusForbidden, gin.H{"error": "internal endpoints are localhost only"})
			c.Abort()
			return
		}

		// 1. 检查反向代理头
		if !proxyConfig.AllowReverseProxy {
			// 检查常见的反代头
			reverseProxyHeaders := []string{
				"X-Forwarded-For",
				"X-Real-IP",
				"X-Forwarded-Host",
				"X-Forwarded-Proto",
				"Via",
				"Forwarded",
			}
			for _, header := range reverseProxyHeaders {
				if c.GetHeader(header) != "" {
					log.Printf("⚠️  检测到反向代理头 %s，拒绝请求", header)
					c.JSON(http.StatusForbidden, gin.H{
						"error": "Reverse proxy detected. Direct connections only.",
					})
					c.Abort()
					return
				}
			}
		}

		// 2. 检查来源 IP
		clientIP := c.ClientIP()
		if clientIP != "127.0.0.1" && clientIP != "::1" && clientIP != "" {
			log.Printf("⚠️  非本地 IP 访问: %s", clientIP)
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Access denied: only localhost connections allowed",
			})
			c.Abort()
			return
		}

		// 3. 检查认证 Token（如果启用）
		if proxyConfig.RequireAuth {
			authHeader := c.GetHeader("X-CodeSwitch-Auth")
			if authHeader == "" {
				// 也检查 Bearer Token
				authHeader = strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
			}

			if authHeader == "" {
				// 【v1.3 修正】Token 为空时的处理
				if proxyConfig.AllowUnauthenticated {
					// 显式允许无认证访问（用户已知晓风险）
					log.Printf("⚠️  [安全警告] 收到无认证请求，已通过（AllowUnauthenticated=true）")
				} else {
					// 默认拒绝无认证请求
					log.Printf("⚠️  拒绝无认证请求（如需允许，请设置 AllowUnauthenticated=true）")
					c.JSON(http.StatusUnauthorized, gin.H{
						"error": "Authentication required. Set AllowUnauthenticated=true to disable.",
					})
					c.Abort()
					return
				}
			} else if authHeader != proxyConfig.AuthToken {
				// Token 不匹配，说明是伪造的
				log.Printf("⚠️  无效的认证 Token")
				c.JSON(http.StatusUnauthorized, gin.H{
					"error": "Invalid authentication token",
				})
				c.Abort()
				return
			}
			// Token 匹配，继续处理
		}

		c.Next()
	}
}

// registerRoutes 注册路由（修改版）
func (prs *ProviderRelayService) registerRoutes(router gin.IRouter) {
	// 【安全修复】添加安全中间件
	router.Use(securityMiddleware())

	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))
	router.POST("/responses", prs.proxyHandler("codex", "/responses"))
	router.POST("/gemini/v1beta/*any", prs.geminiProxyHandler("/v1beta"))
	router.POST("/gemini/v1/*any", prs.geminiProxyHandler("/v1"))

	// 【新增】健康检查端点（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 【新增】获取认证 Token（仅限本地调用，用于配置 Claude Code）
	router.GET("/_internal/auth-token", func(c *gin.Context) {
		// 此端点只有在本地直接访问时才返回 Token
		if c.ClientIP() != "127.0.0.1" && c.ClientIP() != "::1" {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": proxyConfig.AuthToken})
	})
}

// GetAuthToken 获取认证 Token（供前端使用）
func (prs *ProviderRelayService) GetAuthToken() string {
	return proxyConfig.AuthToken
}

// SetAllowReverseProxy 设置是否允许反向代理（高级选项）
func (prs *ProviderRelayService) SetAllowReverseProxy(allow bool) {
	proxyConfig.AllowReverseProxy = allow
	if allow {
		log.Printf("⚠️  已启用反向代理支持，请确保已配置额外的安全措施")
	}
}
```

**验证方法**：
```bash
# 从本地访问
curl http://127.0.0.1:18100/v1/messages
# 预期：正常响应

# 模拟反向代理
curl -H "X-Forwarded-For: 1.2.3.4" http://127.0.0.1:18100/v1/messages
# 预期：403 Forbidden
```

---

### 2.3 SSRF + DNS 重绑定防护

**问题位置**：`services/speedtestservice.go:45-120`

**问题描述**：
- `TestEndpoints` 接受任意 URL 并发起请求
- 无协议、IP、端口限制
- DNS 重绑定攻击可绕过初始验证

**修复方案**：

```go
// services/speedtestservice.go

import (
	"context"
	"net"
	"time"
)

// SafeDialContext 安全的 DialContext（在连接时检查 IP）
func (s *SpeedTestService) safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// 解析主机名
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("无效的地址: %s", addr)
	}

	// 解析 IP（在连接前再次解析，防止 DNS 重绑定）
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("DNS 解析失败: %w", err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("DNS 解析无结果")
	}

	// 检查所有解析到的 IP
	for _, ip := range ips {
		if s.isUnsafeIP(ip) {
			return nil, fmt.Errorf("检测到不安全的 IP 地址: %s（可能是 DNS 重绑定攻击）", ip.String())
		}
	}

	// 使用第一个安全的 IP 连接
	targetAddr := net.JoinHostPort(ips[0].String(), port)

	// 创建连接
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	return dialer.DialContext(ctx, network, targetAddr)
}

// isUnsafeIP 检查是否为不安全的 IP
// 【v1.3 修正】补充 0.0.0.0/::、组播、更多云厂商元数据地址
func (s *SpeedTestService) isUnsafeIP(ip net.IP) bool {
	// 检查是否为 nil 或 unspecified (0.0.0.0, ::)
	if ip == nil || ip.IsUnspecified() {
		return true
	}

	// 检查特殊地址
	if ip.IsLoopback() ||           // 127.0.0.0/8, ::1
		ip.IsPrivate() ||           // 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
		ip.IsLinkLocalUnicast() ||  // 169.254.0.0/16, fe80::/10
		ip.IsLinkLocalMulticast() ||// 224.0.0.0/24, ff02::/16
		ip.IsMulticast() {          // 【v1.3 新增】224.0.0.0/4, ff00::/8 (全部组播地址)
		return true
	}

	// 【v1.3 修正】更完整的云服务元数据地址
	metadataBlocks := []string{
		// AWS
		"169.254.169.254/32",  // AWS EC2 IMDS IPv4
		"fd00:ec2::254/128",   // AWS EC2 IMDS IPv6
		"169.254.170.2/32",    // AWS ECS Task Metadata

		// GCP
		"metadata.google.internal/32", // GCP (通常解析为 169.254.169.254)

		// Azure
		"169.254.169.254/32",  // Azure IMDS (与 AWS 共用)
		"168.63.129.16/32",    // Azure Wire Server

		// Alibaba Cloud
		"100.100.100.200/32",  // Alibaba Cloud Metadata

		// Oracle Cloud
		"169.254.169.254/32",  // Oracle Cloud IMDS (与 AWS 共用)

		// DigitalOcean
		"169.254.169.254/32",  // DigitalOcean Droplet Metadata

		// Hetzner
		"169.254.169.254/32",  // Hetzner Cloud Metadata

		// OpenStack
		"169.254.169.254/32",  // OpenStack Metadata Service

		// Kubernetes
		"10.0.0.1/32",         // 默认 Kubernetes API Server (可配置)

		// Docker
		"172.17.0.1/32",       // Docker bridge 网关 (默认)
	}

	for _, block := range metadataBlocks {
		_, cidr, _ := net.ParseCIDR(block)
		if cidr != nil && cidr.Contains(ip) {
			return true
		}
	}

	// 【v1.3 新增】保留地址段
	reservedBlocks := []string{
		"0.0.0.0/8",           // 当前网络（仅作为源地址有效）
		"100.64.0.0/10",       // 运营商级 NAT (CGN)
		"192.0.0.0/24",        // IETF 协议分配
		"192.0.2.0/24",        // TEST-NET-1 (文档用)
		"198.51.100.0/24",     // TEST-NET-2 (文档用)
		"203.0.113.0/24",      // TEST-NET-3 (文档用)
		"240.0.0.0/4",         // 保留（未来使用）
		"255.255.255.255/32",  // 广播地址
	}

	for _, block := range reservedBlocks {
		_, cidr, _ := net.ParseCIDR(block)
		if cidr != nil && cidr.Contains(ip) {
			return true
		}
	}

	return false
}

// validateURL 增强版 URL 验证
func (s *SpeedTestService) validateURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL 格式无效: %v", err)
	}

	// 1. 协议白名单
	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("不支持的协议: %s", scheme)
	}

	// 2. 检查 Host
	host := parsedURL.Hostname()
	if host == "" {
		return fmt.Errorf("URL 缺少主机名")
	}

	// 3. 拒绝 IP literal（直接使用 IP 地址）
	if ip := net.ParseIP(host); ip != nil {
		if s.isUnsafeIP(ip) {
			return fmt.Errorf("禁止直接访问内网 IP: %s", host)
		}
	}

	// 4. 拒绝特殊主机名
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" ||
		strings.HasSuffix(lowerHost, ".local") ||
		strings.HasSuffix(lowerHost, ".internal") ||
		strings.HasSuffix(lowerHost, ".localhost") {
		return fmt.Errorf("禁止访问本地主机名: %s", host)
	}

	// 5. 端口白名单
	port := parsedURL.Port()
	if port != "" && port != "80" && port != "443" && port != "8080" && port != "8443" {
		return fmt.Errorf("不支持的端口: %s", port)
	}

	return nil
}

// buildSafeClient 构建安全的 HTTP 客户端
func (s *SpeedTestService) buildSafeClient(timeoutSecs int) *http.Client {
	transport := &http.Transport{
		DialContext: s.safeDialContext,  // 【关键】使用安全的 DialContext
		// 禁用代理（防止通过代理绕过）
		Proxy: nil,
		// 限制重定向
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return &http.Client{
		Timeout:   time.Duration(timeoutSecs) * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 限制重定向次数
			if len(via) >= 3 {
				return fmt.Errorf("重定向次数过多")
			}

			// 【关键】检查重定向目标
			if err := s.validateURL(req.URL.String()); err != nil {
				return fmt.Errorf("重定向目标不安全: %w", err)
			}

			return nil
		},
	}
}

// testSingleEndpoint 测试单个端点（修改版）
func (s *SpeedTestService) testSingleEndpoint(client *http.Client, rawURL string) EndpointLatency {
	trimmed := trimSpace(rawURL)
	if trimmed == "" {
		errMsg := "URL 不能为空"
		return EndpointLatency{URL: rawURL, Error: &errMsg}
	}

	// 【安全修复】验证 URL
	if err := s.validateURL(trimmed); err != nil {
		errMsg := err.Error()
		return EndpointLatency{URL: trimmed, Error: &errMsg}
	}

	// 【修复】热身请求响应体必须关闭
	if resp, _ := s.makeRequest(client, trimmed); resp != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// ... 后续代码不变
}

// TestEndpoints 测试端点（修改版）
func (s *SpeedTestService) TestEndpoints(urls []string, timeoutSecs *int) []EndpointLatency {
	if len(urls) == 0 {
		return []EndpointLatency{}
	}

	timeout := s.sanitizeTimeout(timeoutSecs)
	client := s.buildSafeClient(timeout)  // 【修改】使用安全客户端

	// ... 后续代码不变
}
```

**验证方法**：
```bash
# 合法 URL
curl -X POST 'http://localhost:18100/speedtest' \
  -d '{"urls": ["https://api.anthropic.com"]}'
# 预期：正常测试

# 非法 URL（内网）
curl -X POST 'http://localhost:18100/speedtest' \
  -d '{"urls": ["http://192.168.1.1/admin"]}'
# 预期：返回错误 "禁止访问内网地址"

# DNS 重绑定攻击模拟（需要搭建测试环境）
# 预期：在连接时二次解析 IP，发现是内网地址后拒绝
```

---

### 2.4 自动更新完整安全方案

**问题位置**：`services/updateservice.go`

**问题描述**：
- `calculateSHA256()` 函数存在但从未被调用
- 下载后直接执行/替换，无完整性校验
- 静默执行，用户无感知
- 无回滚机制

**修复方案**：

```go
// services/updateservice.go

// UpdateConfirmation 更新确认信息（供前端展示）
type UpdateConfirmation struct {
	Version        string `json:"version"`
	CurrentVersion string `json:"current_version"`
	FilePath       string `json:"file_path"`
	FileSize       int64  `json:"file_size"`
	FileSizeHuman  string `json:"file_size_human"`
	SHA256         string `json:"sha256"`
	ExpectedSHA256 string `json:"expected_sha256"`
	HashVerified   bool   `json:"hash_verified"`
	ReleaseNotes   string `json:"release_notes"`
}

// PrepareUpdate 准备更新（修改版）
func (us *UpdateService) PrepareUpdate() (*UpdateConfirmation, error) {
	us.mu.Lock()
	defer us.mu.Unlock()

	if us.updateFilePath == "" {
		return nil, fmt.Errorf("更新文件路径为空")
	}

	// 1. 计算实际 SHA256
	actualSHA256, err := calculateSHA256(us.updateFilePath)
	if err != nil {
		return nil, fmt.Errorf("计算文件哈希失败: %w", err)
	}

	// 2. 获取文件大小
	fileInfo, err := os.Stat(us.updateFilePath)
	if err != nil {
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}

	// 3. 返回确认信息（供前端展示）
	return &UpdateConfirmation{
		Version:        us.latestVersion,
		CurrentVersion: us.currentVersion,
		FilePath:       us.updateFilePath,
		FileSize:       fileInfo.Size(),
		FileSizeHuman:  formatFileSize(fileInfo.Size()),
		SHA256:         actualSHA256,
		ExpectedSHA256: us.expectedSHA256,
		HashVerified:   us.expectedSHA256 == "" || strings.EqualFold(actualSHA256, us.expectedSHA256),
		ReleaseNotes:   us.releaseNotes,
	}, nil
}

// ConfirmAndApplyUpdate 用户确认后执行更新
func (us *UpdateService) ConfirmAndApplyUpdate(userConfirmed bool) error {
	if !userConfirmed {
		return fmt.Errorf("用户取消更新")
	}

	us.mu.Lock()
	updatePath := us.updateFilePath
	expectedSHA256 := us.expectedSHA256
	us.mu.Unlock()

	// 1. 【关键】再次校验 SHA256（防止 TOCTOU 攻击）
	if expectedSHA256 != "" {
		actualSHA256, err := calculateSHA256(updatePath)
		if err != nil {
			return fmt.Errorf("校验文件失败: %w", err)
		}
		if !strings.EqualFold(actualSHA256, expectedSHA256) {
			os.Remove(updatePath)
			return fmt.Errorf("文件完整性校验失败，已删除可疑文件")
		}
	}

	// 2. 备份当前版本
	if err := us.backupCurrentVersion(); err != nil {
		log.Printf("[UpdateService] ⚠️  备份当前版本失败: %v（继续更新）", err)
		// 不阻止更新，但记录警告
	}

	// 3. 根据平台执行更新
	switch runtime.GOOS {
	case "windows":
		return us.applyUpdateWindowsWithConfirm(updatePath)
	case "darwin":
		return us.applyUpdateDarwinWithConfirm(updatePath)
	case "linux":
		return us.applyUpdateLinuxWithConfirm(updatePath)
	default:
		return fmt.Errorf("不支持的平台: %s", runtime.GOOS)
	}
}

// backupCurrentVersion 备份当前版本
func (us *UpdateService) backupCurrentVersion() error {
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}

	// 备份目录
	backupDir := filepath.Join(us.updateDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return err
	}

	// 备份文件名：CodeSwitch_v1.1.16_20251128_150405.exe
	backupName := fmt.Sprintf("%s_%s_%s%s",
		strings.TrimSuffix(filepath.Base(currentExe), filepath.Ext(currentExe)),
		us.currentVersion,
		time.Now().Format("20060102_150405"),
		filepath.Ext(currentExe),
	)
	backupPath := filepath.Join(backupDir, backupName)

	// 复制文件
	if err := copyUpdateFile(currentExe, backupPath); err != nil {
		return err
	}

	log.Printf("[UpdateService] ✅ 已备份当前版本: %s", backupPath)

	// 清理旧备份（保留最近 3 个）
	us.cleanOldBackups(backupDir, 3)

	return nil
}

// cleanOldBackups 清理旧备份
func (us *UpdateService) cleanOldBackups(backupDir string, keepCount int) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	// 按修改时间排序
	type backupFile struct {
		path    string
		modTime time.Time
	}
	var backups []backupFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupFile{
			path:    filepath.Join(backupDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	// 按时间降序
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.After(backups[j].modTime)
	})

	// 删除多余的备份
	for i := keepCount; i < len(backups); i++ {
		os.Remove(backups[i].path)
		log.Printf("[UpdateService] 已清理旧备份: %s", backups[i].path)
	}
}

// RollbackUpdate 回滚到上一个备份
func (us *UpdateService) RollbackUpdate() error {
	backupDir := filepath.Join(us.updateDir, "backups")

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("无可用备份")
	}

	// 找到最新的备份
	var latestBackup string
	var latestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestBackup = filepath.Join(backupDir, entry.Name())
		}
	}

	if latestBackup == "" {
		return fmt.Errorf("无可用备份")
	}

	currentExe, err := os.Executable()
	if err != nil {
		return err
	}

	// 替换当前可执行文件
	if err := copyUpdateFile(latestBackup, currentExe); err != nil {
		return fmt.Errorf("回滚失败: %w", err)
	}

	log.Printf("[UpdateService] ✅ 已回滚到: %s", latestBackup)
	return nil
}

// applyUpdateWindowsWithConfirm Windows 更新（带确认）
func (us *UpdateService) applyUpdateWindowsWithConfirm(updatePath string) error {
	if us.isPortable {
		return us.applyPortableUpdateWithConfirm(updatePath)
	}

	// 安装版：启动安装器（【修改】移除 /SILENT，让用户看到安装界面）
	cmd := exec.Command(updatePath)  // 不再使用 /SILENT
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动安装器失败: %w", err)
	}

	// 清理 pending 标记
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	_ = os.Remove(pendingFile)

	// 退出当前应用
	os.Exit(0)
	return nil
}

// applyUpdateDarwinWithConfirm macOS 更新（实现或禁用）
func (us *UpdateService) applyUpdateDarwinWithConfirm(zipPath string) error {
	// 【修复】明确告知用户 macOS 自动更新未实现
	return fmt.Errorf("macOS 自动更新暂未实现，请手动下载安装: %s", us.downloadURL)

	// 未来实现时的完整逻辑：
	// 1. 解压 zip 到临时目录
	// 2. 验证 .app 包签名（codesign --verify）
	// 3. 备份当前 .app
	// 4. 替换 /Applications/CodeSwitch.app
	// 5. 使用 open 命令重启
}

// formatFileSize 格式化文件大小
func formatFileSize(size int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case size >= GB:
		return fmt.Sprintf("%.2f GB", float64(size)/GB)
	case size >= MB:
		return fmt.Sprintf("%.2f MB", float64(size)/MB)
	case size >= KB:
		return fmt.Sprintf("%.2f KB", float64(size)/KB)
	default:
		return fmt.Sprintf("%d B", size)
	}
}
```

**前端确认对话框设计**：

```typescript
// 前端：显示更新确认对话框
async function showUpdateConfirmation() {
  const confirmation = await Call.ByName('codeswitch/services.UpdateService.PrepareUpdate')

  const confirmed = await showDialog({
    title: '确认更新',
    content: `
      当前版本: ${confirmation.current_version}
      新版本: ${confirmation.version}
      文件大小: ${confirmation.file_size_human}
      SHA256: ${confirmation.sha256.substring(0, 16)}...
      哈希校验: ${confirmation.hash_verified ? '✅ 通过' : '⚠️ 未校验'}
    `,
    buttons: ['取消', '确认更新']
  })

  if (confirmed) {
    await Call.ByName('codeswitch/services.UpdateService.ConfirmAndApplyUpdate', true)
  }
}
```

---

### 2.5 技能仓库下载安全

**问题位置**：`services/skillservice.go`

**问题描述**：
- 无下载大小限制
- 无路径穿越检查
- 无文件类型检查

**修复方案**：

```go
// services/skillservice.go

const (
	maxZipSize      = 100 * 1024 * 1024  // 100MB 最大下载大小
	maxFileSize     = 10 * 1024 * 1024   // 10MB 单文件最大大小
	maxFileCount    = 1000               // 最大文件数量
	maxPathDepth    = 10                 // 最大路径深度
)

// downloadFile 安全的文件下载（限制大小）
func (ss *SkillService) downloadFile(rawURL, dest string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ai-code-studio")

	resp, err := ss.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: %s", resp.Status)
	}

	// 【安全修复】检查 Content-Length
	if resp.ContentLength > maxZipSize {
		return fmt.Errorf("文件过大: %d bytes（最大允许 %d bytes）", resp.ContentLength, maxZipSize)
	}

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	// 【安全修复】使用 LimitReader 限制读取大小
	limitedReader := io.LimitReader(resp.Body, maxZipSize+1)
	written, err := io.Copy(out, limitedReader)
	if err != nil {
		return err
	}

	if written > maxZipSize {
		os.Remove(dest)
		return fmt.Errorf("文件过大: 超过 %d bytes 限制", maxZipSize)
	}

	return nil
}

// unzipArchive 安全的解压（路径穿越检查）
func unzipArchive(zipPath, dest string) (string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	// 【安全修复】检查文件数量
	if len(reader.File) > maxFileCount {
		return "", fmt.Errorf("压缩包文件过多: %d（最大允许 %d）", len(reader.File), maxFileCount)
	}

	var root string
	var totalSize int64

	for _, file := range reader.File {
		name := file.Name
		if name == "" {
			continue
		}

		// 【安全修复 1】检查路径穿越
		if err := validateZipPath(name, dest); err != nil {
			return "", err
		}

		if root == "" {
			root = strings.Split(name, "/")[0]
		}

		targetPath := filepath.Join(dest, name)

		// 【安全修复 2】再次验证最终路径
		absTarget, err := filepath.Abs(targetPath)
		if err != nil {
			return "", fmt.Errorf("无法解析路径: %s", name)
		}
		absDest, err := filepath.Abs(dest)
		if err != nil {
			return "", fmt.Errorf("无法解析目标目录")
		}
		if !strings.HasPrefix(absTarget, absDest+string(filepath.Separator)) && absTarget != absDest {
			return "", fmt.Errorf("检测到路径穿越攻击: %s", name)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return "", err
			}
			continue
		}

		// 【安全修复 3】检查单文件大小
		if file.UncompressedSize64 > uint64(maxFileSize) {
			return "", fmt.Errorf("文件过大: %s (%d bytes)", name, file.UncompressedSize64)
		}

		// 【安全修复 4】累计大小检查（防止 zip bomb）
		totalSize += int64(file.UncompressedSize64)
		if totalSize > maxZipSize*10 { // 允许 10 倍压缩率
			return "", fmt.Errorf("解压后总大小过大，可能是 zip bomb 攻击")
		}

		// 【安全修复 5】检查文件类型（只允许文本文件和特定类型）
		if !isAllowedFileType(name) {
			log.Printf("[SkillService] 跳过不允许的文件类型: %s", name)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return "", err
		}

		if err := extractFile(file, targetPath); err != nil {
			return "", err
		}
	}

	if root == "" {
		return "", errors.New("压缩包内容为空")
	}
	return filepath.Join(dest, root), nil
}

// validateZipPath 验证 zip 文件路径
func validateZipPath(name, dest string) error {
	// 检查绝对路径
	if filepath.IsAbs(name) {
		return fmt.Errorf("禁止绝对路径: %s", name)
	}

	// 检查路径穿越
	cleanName := filepath.Clean(name)
	if strings.HasPrefix(cleanName, "..") || strings.Contains(cleanName, ".."+string(filepath.Separator)) {
		return fmt.Errorf("检测到路径穿越: %s", name)
	}

	// 检查路径深度
	parts := strings.Split(cleanName, string(filepath.Separator))
	if len(parts) > maxPathDepth {
		return fmt.Errorf("路径深度过大: %s", name)
	}

	return nil
}

// isAllowedFileType 检查是否为允许的文件类型
func isAllowedFileType(name string) bool {
	// 允许的扩展名
	allowedExts := map[string]bool{
		".md":    true,
		".txt":   true,
		".json":  true,
		".yaml":  true,
		".yml":   true,
		".toml":  true,
		".py":    true,
		".js":    true,
		".ts":    true,
		".sh":    true,  // 脚本需要用户自行审查
		".go":    true,
		".rs":    true,
		".css":   true,
		".html":  true,
		".svg":   true,
		".png":   true,
		".jpg":   true,
		".jpeg":  true,
		".gif":   true,
	}

	ext := strings.ToLower(filepath.Ext(name))
	return allowedExts[ext]
}

// extractFile 安全提取单个文件
func extractFile(file *zip.File, targetPath string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode()&0o644) // 去掉执行权限
	if err != nil {
		return err
	}
	defer dst.Close()

	// 使用 LimitReader 防止解压炸弹
	if _, err := io.Copy(dst, io.LimitReader(src, maxFileSize+1)); err != nil {
		return err
	}

	return nil
}
```

---

## 阶段三：核心逻辑

### 3.1 请求转发故障切换

**问题位置**：`services/providerrelay.go:262-323`

**问题描述**：
- 当前只取第一个 Level 的第一个 provider
- 失败直接返回 502，不尝试其他 provider
- 与文档描述的"Level 内按顺序尝试，失败后降级到下一 Level"不符

**修复方案**：

```go
// services/providerrelay.go - proxyHandler 函数

func (prs *ProviderRelayService) proxyHandler(kind string, endpoint string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ... 前置代码（读取 body、加载 providers、过滤）保持不变 ...

		// 按 Level 分组
		levelGroups := make(map[int][]Provider)
		for _, provider := range active {
			level := provider.Level
			if level <= 0 {
				level = 1
			}
			levelGroups[level] = append(levelGroups[level], provider)
		}

		// 获取所有 level 并升序排序
		levels := make([]int, 0, len(levelGroups))
		for level := range levelGroups {
			levels = append(levels, level)
		}
		sort.Ints(levels)

		fmt.Printf("[INFO] 共 %d 个 Level 分组：%v\n", len(levels), levels)

		// 【v1.3 修正】流式请求不做 Provider 回退，只尝试第一个
		// 原因：流式请求一旦开始写入 c.Writer 就无法回滚
		if isStream {
			firstLevel := levels[0]
			firstProvider := levelGroups[firstLevel][0]
			effectiveModel := firstProvider.GetEffectiveModel(requestedModel)

			currentBodyBytes := bodyBytes
			if effectiveModel != requestedModel && requestedModel != "" {
				modifiedBody, err := ReplaceModelInRequestBody(bodyBytes, effectiveModel)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("模型映射失败: %v", err)})
					return
				}
				currentBodyBytes = modifiedBody
			}

			fmt.Printf("[INFO] 流式请求，仅尝试 Level %d 的 %s\n", firstLevel, firstProvider.Name)

			result, err := prs.forwardRequestIsolated(c, kind, firstProvider, endpoint, query, clientHeaders, currentBodyBytes, true, effectiveModel)
			if result == ForwardSuccess {
				prs.blacklistService.RecordSuccess(kind, firstProvider.Name)
			} else if result == ForwardRetryable {
				prs.blacklistService.RecordFailure(kind, firstProvider.Name)
				if err != nil {
					c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
				}
			}
			// ForwardNonRetryable: 已在 forwardRequestStream 中处理
			return
		}

		// 【修复】非流式请求：遍历所有 Level 和 Provider，实现真正的故障切换
		var lastError error
		var lastProvider Provider
		totalAttempts := 0
		maxAttempts := 10  // 总尝试次数上限，防止无限循环

		for _, level := range levels {
			providersInLevel := levelGroups[level]
			fmt.Printf("[INFO] === 尝试 Level %d（%d 个 provider）===\n", level, len(providersInLevel))

			for i, provider := range providersInLevel {
				if totalAttempts >= maxAttempts {
					fmt.Printf("[WARN] 达到最大尝试次数 %d，停止尝试\n", maxAttempts)
					break
				}
				totalAttempts++

				// 获取映射后的模型名
				effectiveModel := provider.GetEffectiveModel(requestedModel)

				// 如果需要映射，修改请求体
				currentBodyBytes := bodyBytes
				if effectiveModel != requestedModel && requestedModel != "" {
					fmt.Printf("[INFO] Provider %s 映射模型: %s -> %s\n", provider.Name, requestedModel, effectiveModel)
					modifiedBody, err := ReplaceModelInRequestBody(bodyBytes, effectiveModel)
					if err != nil {
						fmt.Printf("[ERROR] 替换模型名失败: %v\n", err)
						continue
					}
					currentBodyBytes = modifiedBody
				}

				// 【关键】重置请求体（使用缓存的 bodyBytes）
				c.Request.Body = io.NopCloser(bytes.NewReader(currentBodyBytes))

				fmt.Printf("[INFO]   [%d/%d] Provider: %s | Model: %s\n", i+1, len(providersInLevel), provider.Name, effectiveModel)

				startTime := time.Now()
				result, err := prs.forwardRequestIsolated(c, kind, provider, endpoint, query, clientHeaders, currentBodyBytes, isStream, effectiveModel)
				duration := time.Since(startTime)

				switch result {
				case ForwardSuccess:
					fmt.Printf("[INFO]   ✓ Level %d 成功: %s | 耗时: %.2fs\n", level, provider.Name, duration.Seconds())

					// 成功：清零连续失败计数
					if err := prs.blacklistService.RecordSuccess(kind, provider.Name); err != nil {
						fmt.Printf("[WARN] 清零失败计数失败: %v\n", err)
					}

					return  // 成功，立即返回

				case ForwardNonRetryable:
					// 不可重试错误（4xx），直接返回给用户，不尝试其他 provider
					// 也不记录失败，因为这不是 provider 的问题
					fmt.Printf("[INFO]   ↩ 不可重试错误: %s | 耗时: %.2fs\n", provider.Name, duration.Seconds())
					return

				case ForwardRetryable:
					// 失败：记录错误并继续尝试
					lastError = err
					lastProvider = provider
					errorMsg := "未知错误"
					if err != nil {
						errorMsg = err.Error()
					}
					fmt.Printf("[WARN]   ✗ Level %d 失败: %s | 错误: %s | 耗时: %.2fs\n",
						level, provider.Name, errorMsg, duration.Seconds())

					// 记录失败到黑名单系统
					if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
						fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
					}
				}
			}

			fmt.Printf("[WARN] Level %d 的所有 %d 个 provider 均失败，尝试下一 Level\n", level, len(providersInLevel))
		}

		// 所有 Level 全部失败
		errorMsg := "未知错误"
		if lastError != nil {
			errorMsg = lastError.Error()
		}
		fmt.Printf("[ERROR] 所有 %d 个 provider（%d 次尝试）均失败\n", len(active), totalAttempts)

		c.JSON(http.StatusBadGateway, gin.H{
			"error":         fmt.Sprintf("所有 provider 均失败，最后错误: %s", errorMsg),
			"last_provider": lastProvider.Name,
			"total_attempts": totalAttempts,
		})
	}
}
```

---

### 3.2 Context 隔离（ResponseRecorder 模式）

**问题分析**：原方案在循环中多次调用 `forwardRequest`，可能导致 "headers already sent" 错误。

**修复方案**：

```go
// services/providerrelay.go

import (
	"net/http/httptest"
)

// ForwardResult 转发结果
type ForwardResult int

const (
	ForwardSuccess       ForwardResult = iota  // 成功（2xx）
	ForwardRetryable                           // 可重试错误（5xx、超时、网络错误）
	ForwardNonRetryable                        // 不可重试错误（4xx）
)

// ForwardResponse 转发响应
type ForwardResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// 【v1.3 新增】captureResponseWriter 用于捕获响应体
type captureResponseWriter struct {
	buf        *bytes.Buffer
	statusCode int
	header     http.Header
}

func (w *captureResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *captureResponseWriter) Write(data []byte) (int, error) {
	return w.buf.Write(data)
}

func (w *captureResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

// forwardRequestIsolated 隔离的转发请求（使用 ResponseRecorder）
func (prs *ProviderRelayService) forwardRequestIsolated(
	c *gin.Context,
	kind string,
	provider Provider,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
	isStream bool,
	model string,
) (ForwardResult, error) {
	// 流式请求必须直接写入，无法使用 ResponseRecorder
	if isStream {
		return prs.forwardRequestStream(c, kind, provider, endpoint, query, clientHeaders, bodyBytes, model)
	}

	// 非流式请求：使用 ResponseRecorder 隔离
	recorder := httptest.NewRecorder()

	targetURL := joinURL(provider.APIURL, endpoint)
	headers := cloneMap(clientHeaders)
	headers["Authorization"] = fmt.Sprintf("Bearer %s", provider.APIKey)
	if _, ok := headers["Accept"]; !ok {
		headers["Accept"] = "application/json"
	}

	requestLog := &ReqeustLog{
		Platform: kind,
		Provider: provider.Name,
		Model:    model,
		IsStream: false,
	}
	start := time.Now()
	defer func() {
		requestLog.DurationSec = time.Since(start).Seconds()
		prs.writeRequestLog(requestLog)
	}()

	req := xrequest.New().
		SetHeaders(headers).
		SetQueryParams(query).
		SetRetry(1, 500*time.Millisecond).
		SetTimeout(3 * time.Hour)

	reqBody := bytes.NewReader(bodyBytes)
	req = req.SetBody(reqBody)

	resp, err := req.Post(targetURL)
	if err != nil {
		return ForwardRetryable, err
	}

	if resp == nil {
		return ForwardRetryable, fmt.Errorf("empty response")
	}

	status := resp.StatusCode()
	requestLog.HttpCode = status

	if resp.Error() != nil {
		return ForwardRetryable, resp.Error()
	}

	// 【v1.3 修正】读取响应体
	// xrequest 没有 Body() 方法，使用 ToHttpResponseWriter 配合 bytes.Buffer
	// 或者直接从 RawResponse 读取
	var respBody []byte

	// 方案一：使用 bytes.Buffer 捕获响应
	buf := &bytes.Buffer{}
	bufWriter := &captureResponseWriter{buf: buf}
	_, copyErr := resp.ToHttpResponseWriter(bufWriter, nil)
	if copyErr != nil {
		return ForwardRetryable, fmt.Errorf("读取响应体失败: %w", copyErr)
	}
	respBody = buf.Bytes()

	// 解析 token usage
	parseTokenUsage(respBody, kind, requestLog)

	// 根据状态码判断结果
	switch {
	case status == 0:
		// 特殊处理：状态码 0 当作成功
		prs.writeResponseToClient(c, status, resp.Header(), respBody)
		return ForwardSuccess, nil

	case status >= 200 && status < 300:
		// 成功
		prs.writeResponseToClient(c, status, resp.Header(), respBody)
		return ForwardSuccess, nil

	case status == 429:
		// 限流：可重试
		return ForwardRetryable, fmt.Errorf("限流 (429 Too Many Requests)")

	case status >= 500:
		// 服务端错误：可重试
		return ForwardRetryable, fmt.Errorf("服务端错误 (%d)", status)

	case status >= 400 && status < 500:
		// 客户端错误（400、401、403 等）：不可重试，直接返回给用户
		prs.writeResponseToClient(c, status, resp.Header(), respBody)
		return ForwardNonRetryable, fmt.Errorf("客户端错误 (%d)", status)

	default:
		return ForwardRetryable, fmt.Errorf("未知状态码 (%d)", status)
	}
}

// writeResponseToClient 写入响应到客户端
func (prs *ProviderRelayService) writeResponseToClient(c *gin.Context, status int, headers http.Header, body []byte) {
	// 复制响应头
	for key, values := range headers {
		for _, value := range values {
			c.Header(key, value)
		}
	}
	// 写入状态码和响应体
	contentType := headers.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(status, contentType, body)
}

// forwardRequestStream 流式请求处理（必须直接写入）
func (prs *ProviderRelayService) forwardRequestStream(
	c *gin.Context,
	kind string,
	provider Provider,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
	model string,
) (ForwardResult, error) {
	// 流式请求无法使用 ResponseRecorder，直接调用原有逻辑
	// 但流式请求一旦开始写入就无法回滚，所以只尝试一次
	targetURL := joinURL(provider.APIURL, endpoint)
	headers := cloneMap(clientHeaders)
	headers["Authorization"] = fmt.Sprintf("Bearer %s", provider.APIKey)
	if _, ok := headers["Accept"]; !ok {
		headers["Accept"] = "application/json"
	}

	requestLog := &ReqeustLog{
		Platform: kind,
		Provider: provider.Name,
		Model:    model,
		IsStream: true,
	}
	start := time.Now()
	defer func() {
		requestLog.DurationSec = time.Since(start).Seconds()
		prs.writeRequestLog(requestLog)
	}()

	req := xrequest.New().
		SetHeaders(headers).
		SetQueryParams(query).
		SetRetry(1, 500*time.Millisecond).
		SetTimeout(3 * time.Hour)

	reqBody := bytes.NewReader(bodyBytes)
	req = req.SetBody(reqBody)

	resp, err := req.Post(targetURL)
	if err != nil {
		return ForwardRetryable, err
	}

	if resp == nil {
		return ForwardRetryable, fmt.Errorf("empty response")
	}

	status := resp.StatusCode()
	requestLog.HttpCode = status

	if resp.Error() != nil {
		return ForwardRetryable, resp.Error()
	}

	// 流式响应：判断状态码后直接写入
	if status >= 200 && status < 300 || status == 0 {
		_, copyErr := resp.ToHttpResponseWriter(c.Writer, ReqeustLogHook(c, kind, requestLog))
		if copyErr != nil {
			fmt.Printf("[WARN] 流式复制响应失败: %v\n", copyErr)
		}
		return ForwardSuccess, nil
	}

	if status >= 400 && status < 500 {
		_, copyErr := resp.ToHttpResponseWriter(c.Writer, ReqeustLogHook(c, kind, requestLog))
		if copyErr != nil {
			fmt.Printf("[WARN] 流式复制响应失败: %v\n", copyErr)
		}
		return ForwardNonRetryable, fmt.Errorf("客户端错误 (%d)", status)
	}

	return ForwardRetryable, fmt.Errorf("upstream status %d", status)
}

// writeRequestLog 写入请求日志
func (prs *ProviderRelayService) writeRequestLog(requestLog *ReqeustLog) {
	// 【修复】防御性判空
	if GlobalDBQueueLogs == nil {
		fmt.Printf("[WARN] 数据库队列未初始化，跳过日志写入\n")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := GlobalDBQueueLogs.ExecBatchCtx(ctx, `
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
		fmt.Printf("写入 request_log 失败: %v\n", err)
	}
}
```

---

### 3.3 区分可重试错误

**问题位置**：`services/providerrelay.go:422-431`

**问题描述**：
- 所有非 2xx 响应都返回 `false`，触发 `RecordFailure`
- 400（参数错误）、401（密钥错误）、429（限流）不应拉黑 provider

**修复方案**：已在 3.2 中通过 `ForwardResult` 类型和 `switch` 语句实现。

---

### 3.4 黑名单"假恢复"修复

**问题位置**：`services/blacklistservice.go:617-620`

**问题描述**：
- `AutoRecoverExpired()` 只重置 `auto_recovered=1, failure_count=0`
- 未重置 `blacklist_level`，导致下次失败时等级累加
- Provider 会永久陷入 L5（24 小时拉黑）

**修复方案**：

```go
// services/blacklistservice.go - AutoRecoverExpired 函数

func (bs *BlacklistService) AutoRecoverExpired() error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// ... 查询代码不变 ...

	// 【修复】批量更新时同时清零 blacklist_level 和记录恢复时间
	for _, item := range toRecover {
		err := GlobalDBQueue.Exec(`
			UPDATE provider_blacklist
			SET auto_recovered = 1,
				failure_count = 0,
				blacklist_level = 0,                    -- 【修复】清零等级
				last_recovered_at = ?,                  -- 【修复】记录恢复时间
				last_degrade_hour = 0                   -- 【修复】重置降级计时
			WHERE platform = ? AND provider_name = ?
		`, time.Now(), item.Platform, item.ProviderName)

		if err != nil {
			failed = append(failed, fmt.Sprintf("%s/%s", item.Platform, item.ProviderName))
			log.Printf("⚠️  标记恢复状态失败: %s/%s - %v", item.Platform, item.ProviderName, err)
		} else {
			recovered = append(recovered, fmt.Sprintf("%s/%s", item.Platform, item.ProviderName))
		}
	}

	if len(recovered) > 0 {
		log.Printf("✅ 自动恢复 %d 个过期拉黑（等级已清零）: %v", len(recovered), recovered)
	}

	// ...
}
```

**验证方法**：
```sql
-- 模拟一个 L3 拉黑的 provider
UPDATE provider_blacklist
SET blacklist_level = 3,
    blacklisted_until = datetime('now', '-1 minute')
WHERE provider_name = 'test-provider';

-- 触发自动恢复
-- 等待 1 分钟或手动调用 AutoRecoverExpired()

-- 检查结果
SELECT blacklist_level, auto_recovered, last_recovered_at
FROM provider_blacklist
WHERE provider_name = 'test-provider';

-- 预期：blacklist_level = 0, auto_recovered = 1, last_recovered_at 有值
```

---

### 3.5 HTTP 连接泄漏修复

**问题位置**：`services/speedtestservice.go:94-95`

**问题描述**：热身请求的响应体未关闭，批量测速时会耗尽连接池。

**修复方案**（已在 2.3 节包含）：

```go
// 热身请求（必须关闭响应体！）
if resp, _ := s.makeRequest(client, parsedURL.String()); resp != nil {
	io.Copy(io.Discard, resp.Body)  // 消费响应体
	resp.Body.Close()                // 关闭连接
}
```

---

## 阶段四：功能完善

### 4.1 Windows 自启动路径引号

**问题位置**：`services/autostartservice.go:67-78`

**问题描述**：路径未加引号，`C:\Program Files\` 等带空格的路径会被截断。

**修复方案**：

```go
// services/autostartservice.go

func (as *AutoStartService) enableWindows() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

	// 【修复】路径需要加引号，处理空格
	quotedPath := fmt.Sprintf(`"%s"`, exePath)

	cmd := exec.Command("reg", "add", key, "/v", "CodeSwitch", "/t", "REG_SZ", "/d", quotedPath, "/f")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to add registry key: %w", err)
	}
	return nil
}
```

**验证方法**：
```powershell
# 启用自启动后检查注册表
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v CodeSwitch
# 预期：路径带有引号，如 "C:\Program Files\CodeSwitch\CodeSwitch.exe"
```

---

### 4.2 日志写入判空保护（含 Gemini）

**问题位置**：`services/providerrelay.go:352-381` 和 `684-706`

**问题描述**：如果 `GlobalDBQueueLogs` 为 nil（数据库初始化失败），调用 `ExecBatchCtx` 会 panic。

**修复方案**（已在 3.2 中通过 `writeRequestLog` 函数统一处理）：

```go
// forwardRequest 的 defer 函数
defer func() {
	requestLog.DurationSec = time.Since(start).Seconds()

	// 【修复】防御性判空
	if GlobalDBQueueLogs == nil {
		fmt.Printf("[WARN] 数据库队列未初始化，跳过日志写入\n")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := GlobalDBQueueLogs.ExecBatchCtx(ctx, `
		INSERT INTO request_log (...)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, ...)

	if err != nil {
		fmt.Printf("写入 request_log 失败: %v\n", err)
	}
}()
```

**Gemini 路径同步修复**（`services/providerrelay.go:684-706`）：

```go
// geminiProxyHandler 中的 defer 函数
defer func() {
	requestLog.DurationSec = time.Since(start).Seconds()

	// 【修复】同步添加判空保护
	if GlobalDBQueueLogs == nil {
		fmt.Printf("[WARN] 数据库队列未初始化，跳过 Gemini 日志写入\n")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := GlobalDBQueueLogs.ExecBatchCtx(ctx, `
		INSERT INTO request_log (...)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, ...)

	if err != nil {
		fmt.Printf("[Gemini] 写入 request_log 失败: %v\n", err)
	}
}()
```

---

### 4.3 等级拉黑配置持久化

**问题位置**：`services/blacklist_level_config.go:33-35` 和 `services/settingsservice.go:47-49`

**问题描述**：
- 配置文件不存在时返回默认配置（`EnableLevelBlacklist=false`）
- 前端切换开关后未调用 `SaveBlacklistLevelConfig` 落盘
- 每次启动都回退到固定模式

**修复方案**：

```go
// services/blacklist_level_config.go

func (ss *SettingsService) GetBlacklistLevelConfig() (*BlacklistLevelConfig, error) {
	configPath, err := GetBlacklistLevelConfigPath()
	if err != nil {
		return nil, err
	}

	// 如果文件不存在，创建默认配置并保存
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := DefaultBlacklistLevelConfig()

		// 【修复】自动创建默认配置文件
		if err := ss.SaveBlacklistLevelConfig(defaultConfig); err != nil {
			fmt.Printf("[WARN] 创建默认配置文件失败: %v\n", err)
		} else {
			fmt.Printf("✅ 已创建默认等级拉黑配置: %s\n", configPath)
		}

		return defaultConfig, nil
	}

	// ... 原有读取逻辑
}
```

---

### 4.4 可观测性增强

**修复方案**：

```go
// main.go - 启动时打印配置状态

func main() {
	// ... 初始化代码 ...

	// 【增强】打印启动配置摘要
	printStartupSummary(settingsService, blacklistService)

	// ...
}

func printStartupSummary(ss *services.SettingsService, bs *services.BlacklistService) {
	fmt.Println("========== Code-Switch 启动配置 ==========")

	// 1. 拉黑功能状态
	if ss.IsBlacklistEnabled() {
		fmt.Println("🔒 拉黑功能: 启用")
	} else {
		fmt.Println("🔓 拉黑功能: 禁用")
	}

	// 2. 等级拉黑配置
	levelConfig, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		fmt.Printf("⚠️  等级拉黑配置: 读取失败 (%v)\n", err)
	} else if levelConfig.EnableLevelBlacklist {
		fmt.Printf("📊 等级拉黑: 启用 (L1=%dm, L2=%dm, L3=%dm, L4=%dm, L5=%dm)\n",
			levelConfig.L1DurationMinutes, levelConfig.L2DurationMinutes,
			levelConfig.L3DurationMinutes, levelConfig.L4DurationMinutes,
			levelConfig.L5DurationMinutes)
	} else {
		fmt.Printf("📊 等级拉黑: 禁用 (使用固定模式: %dm)\n", levelConfig.FallbackDurationMinutes)
	}

	// 3. 数据库队列状态
	stats1 := services.GetGlobalDBQueueStats()
	stats2 := services.GetGlobalDBQueueLogsStats()
	fmt.Printf("📝 写入队列: 单次队列长度=%d, 批量队列长度=%d\n", stats1.QueueLength, stats2.BatchQueueLength)

	fmt.Println("==========================================")
}

// services/dbqueue.go - 定期打印队列健康状态（可选）

func startQueueHealthMonitor() {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for range ticker.C {
			stats1 := GetGlobalDBQueueStats()
			stats2 := GetGlobalDBQueueLogsStats()

			// 只在队列积压时打印警告
			if stats1.QueueLength > 100 || stats2.BatchQueueLength > 100 {
				log.Printf("⚠️  队列积压警告: 单次=%d, 批量=%d", stats1.QueueLength, stats2.BatchQueueLength)
			}
		}
	}()
}
```

---

## 测试验证计划

### 阻断级测试（阶段一）

| 测试项 | 测试步骤 | 预期结果 |
|--------|----------|----------|
| 首次启动 | 删除 `~/.code-switch/` 后启动 | 自动创建目录和数据库 |
| 数据库初始化 | 启动并检查日志 | 无 "xdb.DB" 相关错误 |
| 配置路径 | 修改设置后检查文件路径 | 保存到 `~/.code-switch/app.json` |
| 配置迁移 | 创建旧目录后启动 | 自动迁移、创建标记、删除旧目录 |

### 安全测试（阶段二）

| 测试项 | 测试步骤 | 预期结果 |
|--------|----------|----------|
| 本地访问 | `curl http://127.0.0.1:18100/v1/messages` | 正常响应 |
| 远程访问 | 从其他设备访问 | 连接拒绝或 403 |
| 反代检测 | 带 X-Forwarded-For 头访问 | 403 Forbidden |
| SSRF - 内网 | 测速 `http://192.168.1.1` | 返回错误 |
| SSRF - file:// | 测速 `file:///etc/passwd` | 返回错误 |
| SSRF - DNS重绑定 | 使用重绑定测试域名 | 在连接时拒绝 |
| 更新确认 | 下载更新后检查确认界面 | 显示版本、大小、SHA256 |
| 更新回滚 | 更新后调用回滚 | 恢复到上一版本 |

### 核心逻辑测试（阶段三）

| 测试项 | 测试步骤 | 预期结果 |
|--------|----------|----------|
| 故障切换 | 禁用 Level 1 provider | 自动降级到 Level 2 |
| 4xx 不拉黑 | 使用错误 API Key | 返回 401，不触发拉黑 |
| 5xx 拉黑 | 触发服务端错误 | 记录失败，尝试下一个 |
| Context 隔离 | 连续失败多个 provider | 无 "headers already sent" 错误 |
| 恢复等级清零 | 等待拉黑过期 | `blacklist_level = 0` |

---

## 回滚策略

### 配置文件备份

所有配置文件修改前自动备份：

```go
func backupConfig(path string) error {
	backupPath := path + ".bak." + time.Now().Format("20060102150405")
	return copyFile(path, backupPath)
}
```

### 数据库回滚

```sql
-- 回滚 provider_blacklist 表结构变更
ALTER TABLE provider_blacklist DROP COLUMN last_degrade_hour;
-- （如果有新增字段的话）
```

### 代码回滚

每个阶段完成后创建 Git tag：

```bash
git tag -a v1.1.17-fix-stage1 -m "阶段一：阻断级问题修复"
git tag -a v1.1.17-fix-stage2 -m "阶段二：安全漏洞修复"
git tag -a v1.1.17-fix-stage3 -m "阶段三：核心逻辑修复"
git tag -a v1.1.17-fix-stage4 -m "阶段四：功能完善修复"
```

---

## 风险降级总结

| 问题 | 修复前风险 | 修复后风险 |
|------|-----------|-----------|
| 数据库初始化时序 | 阻断级 | 无 |
| SQLite 父目录 | 阻断级 | 无 |
| SuiStore 错误处理 | 阻断级 | 无 |
| 配置目录拼写 | 高 | 无 |
| 代理监听 0.0.0.0 | 灾难级 | 无 |
| 代理反代绕过 | 严重 | 低 |
| SSRF 攻击 | 严重 | 低 |
| DNS 重绑定 | 高 | 低 |
| 自动更新安全 | 严重 | 低 |
| 技能仓库投毒 | 中 | 低 |
| 故障切换缺失 | 高 | 无 |
| Context 重复写 | 高 | 无 |
| 错误分类缺失 | 高 | 无 |
| 黑名单假恢复 | 高 | 无 |
| HTTP 连接泄漏 | 中 | 无 |
| Windows 自启动引号 | 中 | 无 |
| 日志写入判空 | 中 | 无 |
| 等级拉黑持久化 | 中 | 无 |
| 配置迁移不完整 | 低 | 无 |
| 可观测性不足 | 低 | 无 |

---

## 实施建议

**建议执行顺序**：阶段一 → 阶段二 → 阶段三 → 阶段四

**每阶段完成后**：
1. 运行对应的测试用例
2. 创建 Git tag
3. 部署到测试环境验证
4. 确认无回归后进入下一阶段

---

## 落地检查清单（发布前必检）

### 自动更新链路验证

- [ ] **Windows 端到端测试**
  - [ ] 下载新版本 → SHA256 校验通过
  - [ ] `backupCurrentVersion()` 成功备份到 `~/.code-switch/backups/`
  - [ ] 确认对话框正确显示版本/大小/哈希信息
  - [ ] `ConfirmAndApplyUpdate(true)` 启动安装器
  - [ ] 安装失败时 `RollbackUpdate()` 能恢复旧版本
  - [ ] `.pending-update` 标记正确清理

- [ ] **macOS 禁用验证**
  - [ ] `applyUpdateDarwinWithConfirm()` 返回错误
  - [ ] 前端收到错误后显示"请手动下载"提示
  - [ ] 提示中包含正确的下载链接

- [ ] **Linux 验证**（如适用）
  - [ ] 便携版替换流程正常
  - [ ] 备份/回滚机制生效

### 代理配置初始化验证

- [ ] **main.go 初始化顺序**
  ```go
  // 确认顺序：
  initProxyConfig()           // ① 必须最先
  services.InitDatabase()     // ②
  services.InitGlobalDBQueue()// ③
  // ... 然后才能创建服务
  ```

- [ ] **MigrateToV14 执行时机**
  - [ ] 在 `initProxyConfig()` 内部或之前调用
  - [ ] 老用户首次升级时自动创建 `proxy-config.json`
  - [ ] 默认 `AllowUnauthenticated=true`（避免 breaking）
  - [ ] 日志输出迁移提示

- [ ] **_internal 端点测试**
  - [ ] `curl http://127.0.0.1:18100/_internal/auth-token` 返回 Token
  - [ ] 外部 IP 访问 `/_internal/*` 返回 403

### 发布说明必须包含

- [ ] 流式请求不再支持 Provider 回退（技术限制）
- [ ] 新增代理认证机制，老用户默认兼容模式
- [ ] 建议生产环境设置 `AllowUnauthenticated=false`
- [ ] macOS 自动更新暂不可用，需手动下载

---

（文档结束）
