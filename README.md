# Code Switch

集中管理 Claude Code & Codex 供应商

- 无需重启 cc & codex, 平滑切换不同供应商
- 支持多供应商自动降级, 保证使用体验
- 支持请求级别的用量统计, 花费多少清晰可见
- 支持 cc & codex Mcp Server 双平台管理
- 支持 Claude Skill 自动下载与安装, 内置 2 个流行的 skill 仓库
- 支持添加自定义 Skill 仓库

基于 [Wails 3](https://v3.wails.io)

## 实现原理

应用启动时会初始化 在本地 18100 端口创建一个 HTTP 代理服务器, 默认绑定 :18100

并自动更新 Claude Code、Codex 配置, 指向 http://127.0.0.1:18100 服务

代理内部只暴露兼容的关键端点：

- /v1/messages 转发到配置的 Claude 供应商
- /responses 转发到 Codex 供应商；

请求由 proxyHandler 动态挑选符合当前优先级与启用状态的 provider，并在失败时自动回退。

以上流程让 cli 看到的是一个固定的本地地址，而真实请求会被 Code Switch 透明地路由到你在应用里维护的供应商列表

## 下载

[macOS](https://github.com/daodao97/code-swtich/releases) | [Windows](https://github.com/daodao97/code-swtich/releases) | [Linux (amd64)](https://github.com/daodao97/code-swtich/releases)


## 预览
![亮色主界面](resources/images/code-switch.png)
![暗色主界面](resources/images/code-swtich-dark.png)
![日志亮色](resources/images/code-switch-logs.png)
![日志暗色](resources/images/code-switch-logs-dark.png)

## 开发准备
- Go 1.24+
- Node.js 18+
- npm / pnpm / yarn
- Wails 3 CLI：`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`

## 开发运行
```bash
wails3 task dev
```

## 构建流程
1. 同步 build metadata：
   ```bash
   wails3 task common:update:build-assets
   ```
2. 打包 macOS `.app`：
   ```bash
   wails3 task package
   ```

### 交叉编译 Windows (macOS 环境)
1. 安装 `mingw-w64`：
   ```bash
   brew install mingw-w64
   ```
2. 运行 Windows 任务：
   ```bash
   env ARCH=amd64 wails3 task windows:build
   # 生成安装器
   env ARCH=amd64 wails3 task windows:package
   ```

## 发布
脚本 `scripts/publish_release.sh v0.1.0` 将自动打包并上传以下资产（macOS 会分别构建 arm64 与 amd64）：
- `codeswitch-macos-arm64.zip`
- `codeswitch-macos-amd64.zip`
- `codeswitch-arm64-installer.exe`
- `codeswitch.exe`

若要手动发布，可执行：
```bash
wails3 task package
env ARCH=amd64 wails3 task windows:package
scripts/publish_release.sh
```

## 常见问题
- 若 `.app` 无法打开，先执行 `wails3 task common:update:build-assets` 后再构建。
- macOS 交叉编译需要终端拥有完全磁盘访问权限，否则 `~/Library/Caches/go-build` 会报 *operation not permitted*。
