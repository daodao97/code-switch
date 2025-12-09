# Code Switch

> 一站式管理你的 AI 编程助手（Claude Code / Codex / Gemini CLI）

## 这是什么？

**Code Switch** 是一个桌面应用，帮你解决以下问题：

- 有多个 AI API 密钥，想灵活切换？
- API 挂了想自动切换到备用服务？
- 想统计每天用了多少 Token、花了多少钱？
- 想集中管理 MCP 服务器配置？

**一句话总结**：装上它，打开开关，Claude Code / Codex / Gemini CLI 的请求就会自动走你配置的供应商，支持自动降级、用量统计、成本追踪。

## 快速开始

### 1. 下载安装

前往 [Releases](https://github.com/Rogers-F/code-switch-R/releases) 下载对应系统的安装包：

| 系统 | 推荐下载 |
|------|---------|
| Windows | `CodeSwitch-amd64-installer.exe` |
| macOS (M1/M2/M3) | `codeswitch-macos-arm64.zip` |
| macOS (Intel) | `codeswitch-macos-amd64.zip` |
| Linux | `CodeSwitch.AppImage` |

### 2. 添加供应商

打开应用后：

1. 点击右上角 **+** 按钮
2. 填写供应商信息：
   - **名称**：随便起，比如 "官方 API"
   - **API URL**：供应商的接口地址
   - **API Key**：你的密钥
3. 点击保存

### 3. 打开代理开关

在供应商列表上方，打开 **代理开关**（蓝色表示开启）。

完成！现在你的 Claude Code / Codex / Gemini CLI 请求会自动走 Code Switch 代理。

## 功能介绍

### 供应商管理

| 功能 | 说明 |
|------|------|
| 多供应商配置 | 可以添加多个 API 供应商 |
| 拖拽排序 | 拖动卡片调整优先级 |
| 一键启用/禁用 | 每个供应商独立开关 |
| 复制供应商 | 快速复制现有配置 |

### 智能降级

当你配置了多个供应商时：

```
请求发起
    ↓
尝试 Level 1 的供应商 A → 失败
    ↓
尝试 Level 1 的供应商 B → 失败
    ↓
尝试 Level 2 的供应商 C → 成功！
    ↓
返回结果
```

**优先级分组（Level）**：
- Level 1：最高优先级（首选）
- Level 2-9：备选
- Level 10：最低优先级（兜底）

### 模型映射

不同供应商可能使用不同的模型名称，比如：
- 官方 API：`claude-sonnet-4`
- OpenRouter：`anthropic/claude-sonnet-4`

配置模型映射后，Code Switch 会自动转换，你不需要改代码。

### 用量统计

- **热力图**：可视化每日使用量
- **请求统计**：请求次数、成功率
- **Token 统计**：输入/输出 Token 数量
- **成本核算**：基于官方定价计算费用

### MCP 服务器管理

集中管理 Claude Code 和 Codex 的 MCP Server：
- 可视化添加/编辑/删除
- 支持 URL 和命令两种类型
- 自动同步到两个平台

### CLI 配置编辑器

可视化编辑 CLI 配置文件：
- 查看当前配置
- 修改可编辑字段（模型、插件等）
- 添加自定义配置
- 支持解锁直接编辑原始配置

### 其他功能

- **技能市场**：一键安装 Claude Skills
- **速度测试**：测试供应商延迟
- **自定义提示词**：管理系统提示词
- **深度链接**：通过 `ccswitch://` 链接导入配置
- **自动更新**：内置更新检查

## 工作原理

```
Claude Code / Codex / Gemini CLI
            ↓
    Code Switch 代理 (:18100)
            ↓
    ┌───────────────────┐
    │  选择供应商        │
    │  (按优先级尝试)    │
    └───────────────────┘
            ↓
      实际 API 服务器
```

**原理简述**：
1. Code Switch 在本地 18100 端口启动代理服务
2. 自动修改 Claude Code / Codex / Gemini CLI 配置，让它们的请求发到本地代理
3. 代理根据你的配置，将请求转发到对应的供应商
4. 如果供应商失败，自动尝试下一个

## 界面预览

| 亮色主题 | 暗色主题 |
|---------|---------|
| ![亮色主界面](resources/images/code-switch.png) | ![暗色主界面](resources/images/code-swtich-dark.png) |
| ![日志亮色](resources/images/code-switch-logs.png) | ![日志暗色](resources/images/code-switch-logs-dark.png) |

## 常见问题

### 打开开关后 CLI 没反应？

1. 确认代理开关已打开（蓝色状态）
2. 重启 Claude Code / Codex / Gemini CLI
3. 检查供应商配置是否正确

### 如何查看代理是否生效？

1. 在 CLI 中发起一次对话
2. 回到 Code Switch，查看"日志"页面
3. 如果有新记录，说明代理生效

### 关闭应用后 CLI 还能用吗？

不能。Code Switch 关闭后代理服务停止，CLI 请求会失败。

**解决方案**：
- 保持 Code Switch 运行
- 或者关闭代理开关（会恢复 CLI 原始配置）

### 如何备份配置？

配置文件位置：
- Windows: `%USERPROFILE%\.code-switch\`
- macOS/Linux: `~/.code-switch/`

主要文件：
- `claude-code.json` - Claude Code 供应商配置
- `codex.json` - Codex 供应商配置
- `mcp.json` - MCP 服务器配置

## 安装详细说明

### Windows

**安装器方式（推荐）**：
1. 下载 `CodeSwitch-amd64-installer.exe`
2. 双击运行，按提示安装
3. 从开始菜单启动

**便携版**：
1. 下载 `CodeSwitch.exe`
2. 放到任意目录，双击运行

### macOS

1. 下载对应芯片的 zip 文件
2. 解压得到 `Code Switch.app`
3. 拖到"应用程序"文件夹
4. 首次打开如提示"无法验证开发者"，在"系统设置 → 隐私与安全性"中允许

### Linux

**AppImage（推荐）**：
```bash
chmod +x CodeSwitch.AppImage
./CodeSwitch.AppImage
```

**DEB 包（Ubuntu/Debian）**：
```bash
sudo dpkg -i codeswitch_*.deb
sudo apt-get install -f  # 如有依赖问题
```

**RPM 包（Fedora/RHEL）**：
```bash
sudo rpm -i codeswitch-*.rpm
```

## 开发者指南

### 环境准备

```bash
# 安装 Go 1.24+
# 安装 Node.js 18+

# 安装 Wails CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

### 开发运行

```bash
wails3 task dev
```

### 构建发布

```bash
# 更新构建资源
wails3 task common:update:build-assets

# 打包当前平台
wails3 task package
```

## 技术栈

| 层级 | 技术 |
|------|------|
| 框架 | [Wails 3](https://v3.wails.io) |
| 后端 | Go 1.24 + Gin + SQLite |
| 前端 | Vue 3 + TypeScript + Tailwind CSS |
| 打包 | NSIS (Windows) / nFPM (Linux) |

## 开源协议

MIT License

---

**有问题？** 欢迎在 [Issues](https://github.com/Rogers-F/code-switch-R/issues) 反馈
