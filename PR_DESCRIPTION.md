# 供应商失败自动拉黑 + 多项功能增强

## 概述

本 PR 为 Code Switch 项目带来了多项重要功能增强和改进，主要包括：
- ✅ 供应商失败自动拉黑机制
- ✅ 拉黑配置界面
- ✅ 自动更新功能修复
- ✅ 实时状态刷新优化

---

## 核心功能

### 1. 供应商失败自动拉黑功能 (v0.3.0)

**功能描述**：
- 当供应商连续失败达到阈值（可配置，默认 3 次）时，自动拉黑指定时长（可配置：15/30/60 分钟）
- 拉黑期间该供应商不会被选择，避免浪费请求
- 拉黑时长到期后自动解禁并重置失败计数
- 支持手动立即解禁

**实现细节**：
- 新增 `BlacklistService` 核心服务
- 新增 `SettingsService` 配置管理服务
- 数据持久化到 SQLite（`~/.code-switch/app.db`）
- 每分钟自动检查并恢复到期的拉黑记录

**数据库表结构**：
```sql
CREATE TABLE provider_blacklist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    platform TEXT NOT NULL,
    provider_name TEXT NOT NULL,
    failure_count INTEGER DEFAULT 1,
    blacklisted_at DATETIME,
    blacklisted_until DATETIME,
    last_failure_at DATETIME,
    auto_recovered BOOLEAN DEFAULT 0,
    UNIQUE(platform, provider_name)
);

CREATE TABLE app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

**前端 UI**：
- 在供应商卡片上显示拉黑状态横幅
- 实时倒计时显示剩余拉黑时间
- 支持一键立即解禁

---

### 2. 拉黑配置界面 (v0.3.1)

**功能描述**：
在应用设置中新增「拉黑配置」部分，用户可自定义拉黑策略：
- **失败阈值**：1-10 次（默认 3 次）
- **拉黑时长**：15/30/60 分钟（默认 30 分钟）
- 配置即时生效并持久化到数据库

**界面优化**：
- 实时状态刷新优化，拉黑后条幅立即显示（无延迟）
- 窗口焦点恢复监听（最小化后恢复时立即刷新）
- 定期轮询机制（每 10 秒）
- Tab 切换时立即刷新对应平台的黑名单状态

---

### 3. 自动更新功能修复 (v0.3.2)

**修复问题**：
1. ✅ 自动更新开关状态持久化（重启后不再丢失）
2. ✅ 手动点击「立即检查」后自动引导下载和安装
3. ✅ 兼容老版本升级（首次运行时自动保存默认配置）

**用户体验改进**：
- 发现新版本后自动弹窗询问是否下载
- 下载完成后弹窗询问是否重启
- 一键完成：检查 → 下载 → 安装

---

## 技术改进

### 后端

**新增服务**：
- `services/blacklistservice.go` - 核心拉黑逻辑
- `services/settingsservice.go` - 全局配置管理
- `services/database.go` - 数据库表初始化

**核心逻辑**：
```go
// 拉黑检查（在 providerrelay.go 中集成）
if isBlacklisted, until := prs.blacklistService.IsBlacklisted(kind, provider.Name); isBlacklisted {
    fmt.Printf("⛔ Provider %s 已拉黑，过期时间: %v\n", provider.Name, until.Format("15:04:05"))
    skippedCount++
    continue
}

// 失败记录（请求失败时调用）
if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
    fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
}
```

**更新服务改进**：
- 在 `UpdateState` 中新增 `auto_check_enabled` 字段
- 使用 `strings.Contains()` 检查 JSON 字段存在性确保兼容性
- 首次运行时自动保存默认配置

### 前端

**新增文件**：
- `frontend/src/services/blacklist.ts` - 拉黑 API 封装
- `frontend/src/services/settings.ts` - 配置 API 封装

**状态管理优化**：
- 使用 Vue 3 `reactive()` 管理黑名单状态映射
- 实现倒计时客户端递减（减少 API 调用）
- 添加窗口焦点事件和定期轮询

**国际化支持**：
- 完整的中英文翻译（`zh.json` / `en.json`）

---

## 测试建议

### 拉黑功能测试
1. 手动禁用一个供应商的 API Key（制造失败）
2. 连续请求 3 次触发拉黑
3. 观察拉黑条幅是否立即显示
4. 验证倒计时是否正常递减
5. 测试手动解禁功能

### 配置界面测试
1. 打开应用设置 → 拉黑配置
2. 修改失败阈值和拉黑时长
3. 重启应用验证配置是否保存

### 自动更新测试
1. 勾选自动更新
2. 彻底关闭应用并重启
3. 验证设置是否保留
4. 点击「立即检查」测试下载流程

---

## 版本历史

- **v0.3.2** (最新): 修复自动更新功能
- **v0.3.1**: 新增拉黑配置界面 + 实时性优化
- **v0.3.0**: 核心拉黑功能
- **v0.2.6**: HTTP 状态码记录修复
- **v0.2.5**: UI 按钮对齐修复

---

## 兼容性

- ✅ 向后兼容旧版本配置文件
- ✅ 数据库自动迁移（新表自动创建）
- ✅ Windows / macOS / Linux 全平台支持

---

## 相关 Issue

本 PR 解决了以下需求：
- 供应商降级失败率过高导致用户体验差
- 需要手动管理供应商可用性
- 自动更新配置丢失问题
- 拉黑状态更新延迟

---

## 截图

（建议添加以下截图）：
1. 拉黑条幅显示效果
2. 拉黑配置界面
3. 自动更新设置界面

---

感谢你的审核！如有任何问题或建议，请随时告知。🚀
