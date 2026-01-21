package services

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf16"
)

const (
	networkSettingsFile = "network.json"
)

// ListenMode 监听模式
type ListenMode string

const (
	ListenModeLocalhost ListenMode = "localhost"
	ListenModeWSLAuto   ListenMode = "wsl_auto"
	ListenModeLAN       ListenMode = "lan"
	ListenModeCustom    ListenMode = "custom"
)

// NetworkSettings 网络设置
type NetworkSettings struct {
	ListenMode    ListenMode `json:"listenMode"`
	CustomAddress string     `json:"customAddress,omitempty"`
	CurrentAddress string    `json:"currentAddress,omitempty"`
	WSLAutoConfig bool       `json:"wslAutoConfig"`
	TargetCli     TargetCli  `json:"targetCli"`
}

// TargetCli 目标 CLI 工具配置
type TargetCli struct {
	ClaudeCode bool `json:"claudeCode"`
	Codex      bool `json:"codex"`
	Gemini     bool `json:"gemini"`
}

// WSLDetection WSL 检测结果
type WSLDetection struct {
	Detected bool     `json:"detected"`
	Distros  []string `json:"distros"`
}

// ConfigureResult 配置结果
type ConfigureResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// NetworkService 网络配置服务
type NetworkService struct {
	mu            sync.Mutex
	settingsPath  string
	relayAddr     string
	claudeService *ClaudeSettingsService
	codexService  *CodexSettingsService
	geminiService *GeminiService
}

// NewNetworkService 创建网络服务
func NewNetworkService(
	relayAddr string,
	claudeService *ClaudeSettingsService,
	codexService *CodexSettingsService,
	geminiService *GeminiService,
) *NetworkService {
	home, err := getUserHomeDir()
	if err != nil {
		home = "."
	}

	return &NetworkService{
		settingsPath:  filepath.Join(home, appSettingsDir, networkSettingsFile),
		relayAddr:     relayAddr,
		claudeService: claudeService,
		codexService:  codexService,
		geminiService: geminiService,
	}
}

// defaultSettings 默认网络设置
func (ns *NetworkService) defaultSettings() NetworkSettings {
	return NetworkSettings{
		ListenMode:     ListenModeLocalhost,
		CustomAddress:  "",
		CurrentAddress: "127.0.0.1:18100",
		WSLAutoConfig:  false, // 默认关闭
		TargetCli: TargetCli{
			ClaudeCode: true,
			Codex:      true,
			Gemini:     true,
		},
	}
}

// GetNetworkSettings 获取网络设置
func (ns *NetworkService) GetNetworkSettings() (NetworkSettings, error) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	settings := ns.defaultSettings()
	data, err := os.ReadFile(ns.settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return settings, err
	}

	if len(data) == 0 {
		return settings, nil
	}

	if err := json.Unmarshal(data, &settings); err != nil {
		return ns.defaultSettings(), err
	}

	// 计算当前监听地址
	settings.CurrentAddress = ns.computeListenAddress(settings)

	return settings, nil
}

// SaveNetworkSettings 保存网络设置
func (ns *NetworkService) SaveNetworkSettings(settings NetworkSettings) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	// 计算当前监听地址
	settings.CurrentAddress = ns.computeListenAddress(settings)

	dir := filepath.Dir(ns.settingsPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return AtomicWriteBytes(ns.settingsPath, data)
}

// computeListenAddress 计算监听地址
func (ns *NetworkService) computeListenAddress(settings NetworkSettings) string {
	switch settings.ListenMode {
	case ListenModeLocalhost:
		return "127.0.0.1:18100"
	case ListenModeWSLAuto:
		// WSL 模式下使用宿主机地址
		if addr := ns.getWSLHostAddressInternal(); addr != "" {
			return addr + ":18100"
		}
		return "127.0.0.1:18100"
	case ListenModeLAN:
		return "0.0.0.0:18100"
	case ListenModeCustom:
		if settings.CustomAddress != "" {
			return settings.CustomAddress
		}
		return "0.0.0.0:18100"
	default:
		return "127.0.0.1:18100"
	}
}

// decodeUTF16LE 将 UTF-16 LE 编码的字节转换为 UTF-8 字符串
// wsl --list 命令在 Windows 上输出 UTF-16 LE 编码
func decodeUTF16LE(b []byte) string {
	// 跳过 BOM（如果存在）
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	}

	// 确保字节数为偶数
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}

	// 将字节转换为 uint16 切片
	u16s := make([]uint16, len(b)/2)
	for i := 0; i < len(u16s); i++ {
		u16s[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8 // Little Endian
	}

	// 解码 UTF-16 到 rune 切片
	runes := utf16.Decode(u16s)
	return string(runes)
}

// DetectWSL 检测 WSL 状态
func (ns *NetworkService) DetectWSL() WSLDetection {
	result := WSLDetection{
		Detected: false,
		Distros:  []string{},
	}

	// 只在 Windows 上检测 WSL
	if runtime.GOOS != "windows" {
		return result
	}

	// 执行 wsl --list --quiet 获取发行版列表
	cmd := hideWindowCmd("wsl", "--list", "--quiet")
	output, err := cmd.Output()
	if err != nil {
		return result
	}

	// wsl 命令输出 UTF-16 LE 编码，需要解码
	decoded := decodeUTF16LE(output)

	// 解析输出，提取发行版名称
	lines := strings.Split(decoded, "\n")
	for _, line := range lines {
		// 清理行内容（去除空白字符、NUL 等）
		distro := strings.TrimSpace(line)
		distro = strings.Trim(distro, "\x00\r")
		if distro != "" && !strings.HasPrefix(distro, "Windows") {
			result.Distros = append(result.Distros, distro)
		}
	}

	result.Detected = len(result.Distros) > 0
	return result
}

// GetWSLHostAddress 获取 WSL 宿主机地址
func (ns *NetworkService) GetWSLHostAddress() string {
	return ns.getWSLHostAddressInternal()
}

// getWSLHostAddressInternal 内部方法：获取 WSL 宿主机地址
func (ns *NetworkService) getWSLHostAddressInternal() string {
	if runtime.GOOS != "windows" {
		return ""
	}

	// 方法 1: 从 /etc/resolv.conf 读取 nameserver（WSL2 中指向宿主机）
	// 这需要在 WSL 内部执行，从 Windows 侧无法直接获取
	// 我们使用 Windows 侧的 IP 地址

	// 方法 2: 获取 Windows 主机的 WSL 虚拟网络适配器 IP
	// 执行 ipconfig 并解析 vEthernet (WSL) 的 IP
	cmd := hideWindowCmd("ipconfig")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(output), "\n")
	inWSLAdapter := false
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// 查找 WSL 虚拟网络适配器
		if strings.Contains(line, "vEthernet (WSL)") ||
			strings.Contains(line, "vEthernet(WSL)") ||
			strings.Contains(line, "Ethernet adapter vEthernet (WSL)") {
			inWSLAdapter = true
			continue
		}

		// 检测到其他适配器时退出
		if inWSLAdapter && strings.Contains(line, "adapter") && !strings.Contains(line, "WSL") {
			break
		}

		// 解析 IPv4 地址
		if inWSLAdapter {
			if strings.Contains(line, "IPv4") || strings.Contains(line, "IP Address") {
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					ip := strings.TrimSpace(parts[len(parts)-1])
					if ip != "" && !strings.Contains(ip, ":") {
						return ip
					}
				}
			}
		}
	}

	// 备用方法：返回本机 IP
	return "127.0.0.1"
}

// ConfigureWSLClients 配置 WSL 中的 CLI 工具
func (ns *NetworkService) ConfigureWSLClients(targets TargetCli) ConfigureResult {
	if runtime.GOOS != "windows" {
		return ConfigureResult{
			Success: false,
			Message: "WSL configuration is only available on Windows",
		}
	}

	// 检测 WSL
	wslStatus := ns.DetectWSL()
	if !wslStatus.Detected {
		return ConfigureResult{
			Success: false,
			Message: "WSL not detected",
		}
	}

	// 获取宿主机地址
	hostAddr := ns.getWSLHostAddressInternal()
	if hostAddr == "" {
		hostAddr = "127.0.0.1"
	}
	proxyURL := fmt.Sprintf("http://%s:18100", hostAddr)

	var errors []string
	var successes []string

	// 为每个发行版配置 CLI 工具
	for _, distro := range wslStatus.Distros {
		if targets.ClaudeCode {
			if err := ns.configureWSLClaude(distro, proxyURL); err != nil {
				errors = append(errors, fmt.Sprintf("Claude Code in %s: %v", distro, err))
			} else {
				successes = append(successes, fmt.Sprintf("Claude Code in %s", distro))
			}
		}

		if targets.Codex {
			if err := ns.configureWSLCodex(distro, proxyURL); err != nil {
				errors = append(errors, fmt.Sprintf("Codex in %s: %v", distro, err))
			} else {
				successes = append(successes, fmt.Sprintf("Codex in %s", distro))
			}
		}

		if targets.Gemini {
			if err := ns.configureWSLGemini(distro, proxyURL); err != nil {
				errors = append(errors, fmt.Sprintf("Gemini CLI in %s: %v", distro, err))
			} else {
				successes = append(successes, fmt.Sprintf("Gemini CLI in %s", distro))
			}
		}
	}

	if len(errors) > 0 {
		return ConfigureResult{
			Success: len(successes) > 0,
			Message: fmt.Sprintf("Configured: %d, Errors: %d. %s",
				len(successes), len(errors), strings.Join(errors, "; ")),
		}
	}

	return ConfigureResult{
		Success: true,
		Message: fmt.Sprintf("Successfully configured %d CLI tool(s)", len(successes)),
	}
}

// bashSingleQuote safely converts a string to a bash single-quoted literal.
// Example: abc'def -> 'abc'"'"'def'
func bashSingleQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// configureWSLClaude 在 WSL 中配置 Claude Code（字段级合并）
func (ns *NetworkService) configureWSLClaude(distro, proxyURL string) error {
	// 字段级合并：仅更新 env.ANTHROPIC_BASE_URL / env.ANTHROPIC_AUTH_TOKEN，保留其他设置
	script := fmt.Sprintf(`
set -euo pipefail

# 修复 WSL 中 HOME 环境变量指向 Windows 路径的问题
HOME="$(getent passwd "$(whoami)" | cut -d: -f6)"
export HOME

mkdir -p "$HOME/.claude"
config_path="$HOME/.claude/settings.json"

# Symlink protection: refuse to modify symlinks to avoid breaking dotfiles management
if [ -L "$config_path" ]; then
  echo "Refusing to modify: $config_path is a symlink. Please manage it manually or remove the symlink first." >&2
  exit 2
fi

base_url=%s
auth_token='code-switch-r'

ts="$(date +%%s)"
if [ -f "$config_path" ]; then
  cp -a "$config_path" "$config_path.bak.$ts"
fi

tmp_path="$(mktemp "${config_path}.tmp.XXXXXX")"
cleanup() { rm -f "$tmp_path"; }
trap cleanup EXIT

if command -v jq >/dev/null 2>&1; then
  if [ -s "$config_path" ]; then
    if ! jq -e 'type=="object" and (.env==null or (.env|type)=="object")' "$config_path" >/dev/null; then
      echo "Refusing to modify: $config_path must be a JSON object and env must be an object (or null)." >&2
      exit 2
    fi

    jq --arg base_url "$base_url" --arg auth_token "$auth_token" '
      .env = (.env // {})
      | .env.ANTHROPIC_BASE_URL = $base_url
      | .env.ANTHROPIC_AUTH_TOKEN = $auth_token
    ' "$config_path" > "$tmp_path"
  else
    jq -n --arg base_url "$base_url" --arg auth_token "$auth_token" '{env:{ANTHROPIC_BASE_URL:$base_url, ANTHROPIC_AUTH_TOKEN:$auth_token}}' > "$tmp_path"
  fi

  jq -e --arg base_url "$base_url" --arg auth_token "$auth_token" '
    .env.ANTHROPIC_BASE_URL == $base_url and .env.ANTHROPIC_AUTH_TOKEN == $auth_token
  ' "$tmp_path" >/dev/null
elif command -v python3 >/dev/null 2>&1; then
  python3 - "$base_url" "$auth_token" "$config_path" "$tmp_path" <<'PY'
import json
import sys
from pathlib import Path

base_url, auth_token, src, dst = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]

data = {}
try:
    text = Path(src).read_text(encoding="utf-8")
    if text.strip():
        data = json.loads(text)
except FileNotFoundError:
    data = {}
except Exception as e:
    sys.stderr.write(f"Failed to parse existing settings.json: {e}\n")
    sys.exit(2)

if not isinstance(data, dict):
    sys.stderr.write("settings.json must be a JSON object\n")
    sys.exit(2)

env = data.get("env")
if env is None:
    env = {}
if not isinstance(env, dict):
    sys.stderr.write("settings.json env must be a JSON object\n")
    sys.exit(2)

env["ANTHROPIC_BASE_URL"] = base_url
env["ANTHROPIC_AUTH_TOKEN"] = auth_token
data["env"] = env

Path(dst).write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
PY

  python3 - "$base_url" "$auth_token" "$tmp_path" <<'PY'
import json
import sys
from pathlib import Path

base_url, auth_token, path = sys.argv[1], sys.argv[2], sys.argv[3]
payload = json.loads(Path(path).read_text(encoding="utf-8"))
ok = (
    isinstance(payload, dict)
    and isinstance(payload.get("env"), dict)
    and payload["env"].get("ANTHROPIC_BASE_URL") == base_url
    and payload["env"].get("ANTHROPIC_AUTH_TOKEN") == auth_token
)
if not ok:
    sys.stderr.write("Sanity check failed for generated settings.json\n")
    sys.exit(2)
PY
else
  if [ -s "$config_path" ]; then
    echo "Missing jq/python3; cannot safely merge existing $config_path" >&2
    exit 2
  fi

  cat > "$tmp_path" <<EOF
{
  "env": {
    "ANTHROPIC_BASE_URL": "$base_url",
    "ANTHROPIC_AUTH_TOKEN": "$auth_token"
  }
}
EOF
fi

if [ ! -s "$tmp_path" ]; then
  echo "Sanity check failed: generated settings.json is empty" >&2
  exit 2
fi

if [ -f "$config_path" ]; then
  chmod --reference="$config_path" "$tmp_path" 2>/dev/null || true
fi

mv -f "$tmp_path" "$config_path"
trap - EXIT
`, bashSingleQuote(proxyURL))

	return ns.runWSLCommand(distro, script)
}

// configureWSLCodex 在 WSL 中配置 Codex（字段级合并）
func (ns *NetworkService) configureWSLCodex(distro, proxyURL string) error {
	// 字段级合并：
	// - config.toml: 更新 preferred_auth_method、model_provider；移除旧的 [model_providers.code-switch-r]；追加新段到文件末尾
	// - auth.json: 仅更新 OPENAI_API_KEY
	// 写入采用 tmp + mv，并对双文件写入做失败回滚
	script := fmt.Sprintf(`
set -euo pipefail

# 修复 WSL 中 HOME 环境变量指向 Windows 路径的问题
HOME="$(getent passwd "$(whoami)" | cut -d: -f6)"
export HOME

mkdir -p "$HOME/.codex"
config_path="$HOME/.codex/config.toml"
auth_path="$HOME/.codex/auth.json"

# Symlink protection: refuse to modify symlinks to avoid breaking dotfiles management
if [ -L "$config_path" ]; then
  echo "Refusing to modify: $config_path is a symlink. Please manage it manually or remove the symlink first." >&2
  exit 2
fi
if [ -L "$auth_path" ]; then
  echo "Refusing to modify: $auth_path is a symlink. Please manage it manually or remove the symlink first." >&2
  exit 2
fi

base_url=%s
provider_key='code-switch-r'
api_key='code-switch-r'

ts="$(date +%%s)"
[ -f "$config_path" ] && cp -a "$config_path" "$config_path.bak.$ts"
[ -f "$auth_path" ] && cp -a "$auth_path" "$auth_path.bak.$ts"

tmp_config="$(mktemp "${config_path}.tmp.XXXXXX")"
tmp_auth="$(mktemp "${auth_path}.tmp.XXXXXX")"
cleanup() { rm -f "$tmp_config" "$tmp_auth"; }
trap cleanup EXIT

if [ -s "$config_path" ]; then
  awk -v provider_key="$provider_key" -v base_url="$base_url" '
    BEGIN { in_root=1; seen_pref=0; seen_model=0; skipping=0 }
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function rtrim(s) { sub(/[[:space:]]+$/, "", s); return s }
    function extract_header(s) {
      # Extract pure header from line, removing inline comments
      # e.g., "[foo.bar] # comment" -> "[foo.bar]"
      if (match(s, /^\[[^\]]+\]/)) {
        return substr(s, RSTART, RLENGTH)
      }
      return s
    }
    function is_target_section(h, pk) {
      # Match: [model_providers.code-switch-r], [model_providers."code-switch-r"], [model_providers.'"'"'code-switch-r'"'"']
      # Also match subtables: [model_providers.code-switch-r.xxx]
      header = extract_header(h)
      base1 = "[model_providers." pk "]"
      base2 = "[model_providers.\"" pk "\"]"
      base3 = "[model_providers.'"'"'" pk "'"'"']"
      prefix1 = "[model_providers." pk "."
      prefix2 = "[model_providers.\"" pk "\"."
      prefix3 = "[model_providers.'"'"'" pk "'"'"'."
      return (header == base1 || header == base2 || header == base3 || index(header, prefix1) == 1 || index(header, prefix2) == 1 || index(header, prefix3) == 1)
    }
    {
      line=$0
      trimmed=rtrim(ltrim(line))

      # skipping check BEFORE comment check to delete comments inside skipped section
      if (skipping) {
        if (substr(trimmed, 1, 1) == "[") {
          if (is_target_section(trimmed, provider_key)) {
            next
          }
          skipping=0
        } else {
          next
        }
      }

      if (trimmed ~ /^#/) { print line; next }

      if (in_root && substr(trimmed, 1, 1) == "[") {
        inserted=0
        if (!seen_pref) { print "preferred_auth_method = \"apikey\""; seen_pref=1; inserted=1 }
        if (!seen_model) { print "model_provider = \"" provider_key "\""; seen_model=1; inserted=1 }
        if (inserted) print ""
        in_root=0
      }

      if (is_target_section(trimmed, provider_key)) {
        skipping=1
        next
      }

      if (in_root && trimmed ~ /^preferred_auth_method[[:space:]]*=/) {
        if (!seen_pref) { print "preferred_auth_method = \"apikey\""; seen_pref=1 }
        next
      }
      if (in_root && trimmed ~ /^model_provider[[:space:]]*=/) {
        if (!seen_model) { print "model_provider = \"" provider_key "\""; seen_model=1 }
        next
      }

      print line
    }
    END {
      if (in_root) {
        if (!seen_pref) print "preferred_auth_method = \"apikey\""
        if (!seen_model) print "model_provider = \"" provider_key "\""
      }
      print ""
      print "[model_providers." provider_key "]"
      print "name = \"" provider_key "\""
      print "base_url = \"" base_url "\""
      print "wire_api = \"responses\""
      print "requires_openai_auth = false"
    }
  ' "$config_path" > "$tmp_config"
else
  cat > "$tmp_config" <<EOF
preferred_auth_method = "apikey"
model_provider = "code-switch-r"

[model_providers.code-switch-r]
name = "code-switch-r"
base_url = "$base_url"
wire_api = "responses"
requires_openai_auth = false
EOF
fi

if [ ! -s "$tmp_config" ]; then
  echo "Sanity check failed: generated config.toml is empty" >&2
  exit 2
fi
grep -qF 'preferred_auth_method = "apikey"' "$tmp_config" || { echo "Sanity check failed: missing preferred_auth_method" >&2; exit 2; }
grep -qF 'model_provider = "code-switch-r"' "$tmp_config" || { echo "Sanity check failed: missing model_provider" >&2; exit 2; }
grep -qF "base_url = \"$base_url\"" "$tmp_config" || { echo "Sanity check failed: missing provider base_url" >&2; exit 2; }

# Use AWK to count section headers (ignoring comments), matching all header variants
# Also handles inline comments like "[model_providers.code-switch-r] # comment"
count_section="$(awk -v pk="$provider_key" '
  BEGIN { c=0 }
  function extract_header(s) {
    if (match(s, /^\[[^\]]+\]/)) {
      return substr(s, RSTART, RLENGTH)
    }
    return s
  }
  {
    line=$0
    sub(/^[[:space:]]+/, "", line)
    sub(/[[:space:]]+$/, "", line)
    if (line ~ /^#/) next
    if (substr(line, 1, 1) != "[") next
    header = extract_header(line)
    base1 = "[model_providers." pk "]"
    base2 = "[model_providers.\"" pk "\"]"
    base3 = "[model_providers.'"'"'" pk "'"'"']"
    if (header == base1 || header == base2 || header == base3) c++
  }
  END { print c }
' "$tmp_config")"
if [ "$count_section" -ne 1 ]; then
  echo "Sanity check failed: expected exactly one [model_providers.code-switch-r] section, got $count_section" >&2
  exit 2
fi

if command -v jq >/dev/null 2>&1; then
  if [ -s "$auth_path" ]; then
    if ! jq -e 'type=="object"' "$auth_path" >/dev/null; then
      echo "Refusing to modify: $auth_path must be a JSON object." >&2
      exit 2
    fi
    jq --arg api_key "$api_key" '.OPENAI_API_KEY = $api_key' "$auth_path" > "$tmp_auth"
  else
    jq -n --arg api_key "$api_key" '{OPENAI_API_KEY:$api_key}' > "$tmp_auth"
  fi
  jq -e --arg api_key "$api_key" '.OPENAI_API_KEY == $api_key' "$tmp_auth" >/dev/null
elif command -v python3 >/dev/null 2>&1; then
  python3 - "$api_key" "$auth_path" "$tmp_auth" <<'PY'
import json
import sys
from pathlib import Path

api_key, src, dst = sys.argv[1], sys.argv[2], sys.argv[3]
data = {}
try:
    text = Path(src).read_text(encoding="utf-8")
    if text.strip():
        data = json.loads(text)
except FileNotFoundError:
    data = {}
except Exception as e:
    sys.stderr.write(f"Failed to parse existing auth.json: {e}\n")
    sys.exit(2)

if not isinstance(data, dict):
    sys.stderr.write("auth.json must be a JSON object\n")
    sys.exit(2)

data["OPENAI_API_KEY"] = api_key
Path(dst).write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
PY

  python3 - "$api_key" "$tmp_auth" <<'PY'
import json
import sys
from pathlib import Path

api_key, path = sys.argv[1], sys.argv[2]
payload = json.loads(Path(path).read_text(encoding="utf-8"))
if not (isinstance(payload, dict) and payload.get("OPENAI_API_KEY") == api_key):
    sys.stderr.write("Sanity check failed for generated auth.json\n")
    sys.exit(2)
PY
else
  if [ -s "$auth_path" ]; then
    echo "Missing jq/python3; cannot safely merge existing $auth_path" >&2
    exit 2
  fi
  cat > "$tmp_auth" <<EOF
{"OPENAI_API_KEY":"$api_key"}
EOF
fi

if [ ! -s "$tmp_auth" ]; then
  echo "Sanity check failed: generated auth.json is empty" >&2
  exit 2
fi
grep -qF '"OPENAI_API_KEY"' "$tmp_auth" || { echo "Sanity check failed: missing OPENAI_API_KEY" >&2; exit 2; }

if [ -f "$config_path" ]; then
  chmod --reference="$config_path" "$tmp_config" 2>/dev/null || true
fi
if [ -f "$auth_path" ]; then
  chmod --reference="$auth_path" "$tmp_auth" 2>/dev/null || true
fi

if mv -f "$tmp_config" "$config_path"; then
  if mv -f "$tmp_auth" "$auth_path"; then
    trap - EXIT
    exit 0
  fi

  echo "Failed to write $auth_path; attempting to rollback $config_path" >&2
  if [ -f "$config_path.bak.$ts" ]; then
    if cp -a "$config_path.bak.$ts" "$config_path"; then
      echo "Rollback successful: restored $config_path from backup" >&2
    else
      echo "CRITICAL: Rollback failed! Manual recovery needed: cp $config_path.bak.$ts $config_path" >&2
    fi
  else
    echo "WARNING: No backup found for $config_path, moving to $config_path.failed.$ts" >&2
    mv -f "$config_path" "$config_path.failed.$ts" 2>/dev/null || echo "CRITICAL: Failed to move config to .failed" >&2
  fi
  exit 1
fi

echo "Failed to write $config_path" >&2
exit 1
`, bashSingleQuote(proxyURL))

	return ns.runWSLCommand(distro, script)
}

// configureWSLGemini 在 WSL 中配置 Gemini CLI（字段级合并）
func (ns *NetworkService) configureWSLGemini(distro, proxyURL string) error {
	// Gemini CLI 配置路径: ~/.gemini/.env
	// 注意：Gemini 代理路由带 /gemini 前缀
	geminiURL := strings.TrimRight(proxyURL, "/") + "/gemini"

	// 字段级合并：仅更新 GOOGLE_GEMINI_BASE_URL / GEMINI_API_KEY；更新首次出现并删除后续重复项
	script := fmt.Sprintf(`
set -euo pipefail

# 修复 WSL 中 HOME 环境变量指向 Windows 路径的问题
HOME="$(getent passwd "$(whoami)" | cut -d: -f6)"
export HOME

mkdir -p "$HOME/.gemini"
env_path="$HOME/.gemini/.env"

# Symlink protection: refuse to modify symlinks to avoid breaking dotfiles management
if [ -L "$env_path" ]; then
  echo "Refusing to modify: $env_path is a symlink. Please manage it manually or remove the symlink first." >&2
  exit 2
fi

gemini_base_url=%s
api_key='code-switch-r'

ts="$(date +%%s)"
[ -f "$env_path" ] && cp -a "$env_path" "$env_path.bak.$ts"

tmp_path="$(mktemp "${env_path}.tmp.XXXXXX")"
cleanup() { rm -f "$tmp_path"; }
trap cleanup EXIT

if [ -f "$env_path" ]; then
  awk -v gemini_base_url="$gemini_base_url" -v api_key="$api_key" '
    BEGIN { seen_base=0; seen_key=0 }
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    {
      line=$0
      trimmed=ltrim(line)
      if (trimmed ~ /^#/) { print line; next }

      prefix=""
      rest=trimmed
      if (rest ~ /^export[[:space:]]+/) {
        prefix="export "
        sub(/^export[[:space:]]+/, "", rest)
      }

      if (rest ~ /^GOOGLE_GEMINI_BASE_URL[[:space:]]*=/) {
        if (!seen_base) {
          print prefix "GOOGLE_GEMINI_BASE_URL=" gemini_base_url
          seen_base=1
        }
        next
      }
      if (rest ~ /^GEMINI_API_KEY[[:space:]]*=/) {
        if (!seen_key) {
          print prefix "GEMINI_API_KEY=" api_key
          seen_key=1
        }
        next
      }

      print line
    }
    END {
      if (!seen_base) print "GOOGLE_GEMINI_BASE_URL=" gemini_base_url
      if (!seen_key) print "GEMINI_API_KEY=" api_key
    }
  ' "$env_path" > "$tmp_path"
else
  cat > "$tmp_path" <<EOF
GOOGLE_GEMINI_BASE_URL=$gemini_base_url
GEMINI_API_KEY=$api_key
EOF
fi

if [ ! -s "$tmp_path" ]; then
  echo "Sanity check failed: generated .env is empty" >&2
  exit 2
fi

count_base="$(awk '
  BEGIN{c=0}
  {
    line=$0
    sub(/^[[:space:]]+/, "", line)
    if (line ~ /^#/) next
    if (line ~ /^export[[:space:]]+/) sub(/^export[[:space:]]+/, "", line)
    if (line ~ /^GOOGLE_GEMINI_BASE_URL[[:space:]]*=/) c++
  }
  END{print c}
' "$tmp_path")"
if [ "$count_base" -ne 1 ]; then
  echo "Sanity check failed: expected exactly one GOOGLE_GEMINI_BASE_URL, got $count_base" >&2
  exit 2
fi

count_key="$(awk '
  BEGIN{c=0}
  {
    line=$0
    sub(/^[[:space:]]+/, "", line)
    if (line ~ /^#/) next
    if (line ~ /^export[[:space:]]+/) sub(/^export[[:space:]]+/, "", line)
    if (line ~ /^GEMINI_API_KEY[[:space:]]*=/) c++
  }
  END{print c}
' "$tmp_path")"
if [ "$count_key" -ne 1 ]; then
  echo "Sanity check failed: expected exactly one GEMINI_API_KEY, got $count_key" >&2
  exit 2
fi

actual_base="$(awk '
  {
    line=$0
    sub(/^[[:space:]]+/, "", line)
    if (line ~ /^#/) next
    if (line ~ /^export[[:space:]]+/) sub(/^export[[:space:]]+/, "", line)
    if (line ~ /^GOOGLE_GEMINI_BASE_URL[[:space:]]*=/) {
      sub(/^GOOGLE_GEMINI_BASE_URL[[:space:]]*=/, "", line)
      sub(/[[:space:]]+$/, "", line)
      print line
      exit
    }
  }
' "$tmp_path")"
if [ "$actual_base" != "$gemini_base_url" ]; then
  echo "Sanity check failed: GOOGLE_GEMINI_BASE_URL mismatch" >&2
  exit 2
fi

actual_key="$(awk '
  {
    line=$0
    sub(/^[[:space:]]+/, "", line)
    if (line ~ /^#/) next
    if (line ~ /^export[[:space:]]+/) sub(/^export[[:space:]]+/, "", line)
    if (line ~ /^GEMINI_API_KEY[[:space:]]*=/) {
      sub(/^GEMINI_API_KEY[[:space:]]*=/, "", line)
      sub(/[[:space:]]+$/, "", line)
      print line
      exit
    }
  }
' "$tmp_path")"
if [ "$actual_key" != "$api_key" ]; then
  echo "Sanity check failed: GEMINI_API_KEY mismatch" >&2
  exit 2
fi

if [ -f "$env_path" ]; then
  chmod --reference="$env_path" "$tmp_path" 2>/dev/null || true
fi

mv -f "$tmp_path" "$env_path"
trap - EXIT
`, bashSingleQuote(geminiURL))

	return ns.runWSLCommand(distro, script)
}

// runWSLCommand 在指定的 WSL 发行版中执行命令
func (ns *NetworkService) runWSLCommand(distro, script string) error {
	// 通过 stdin 传递脚本，避免 Windows 命令行参数转义问题
	cmd := hideWindowCmd("wsl", "-d", distro, "bash")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(output))
	}
	return nil
}

// GetWSLConfigStatus 获取 WSL 配置状态
func (ns *NetworkService) GetWSLConfigStatus() map[string]map[string]bool {
	result := make(map[string]map[string]bool)

	if runtime.GOOS != "windows" {
		return result
	}

	wslStatus := ns.DetectWSL()
	if !wslStatus.Detected {
		return result
	}

	for _, distro := range wslStatus.Distros {
		status := make(map[string]bool)
		status["claudeCode"] = ns.checkWSLClaudeConfigured(distro)
		status["codex"] = ns.checkWSLCodexConfigured(distro)
		status["gemini"] = ns.checkWSLGeminiConfigured(distro)
		result[distro] = status
	}

	return result
}

// checkWSLClaudeConfigured 检查 WSL 中 Claude Code 是否已配置
func (ns *NetworkService) checkWSLClaudeConfigured(distro string) bool {
	cmd := hideWindowCmd("wsl", "-d", distro, "bash", "-lc", "test -f ~/.claude/settings.json")
	return cmd.Run() == nil
}

// checkWSLCodexConfigured 检查 WSL 中 Codex 是否已配置
func (ns *NetworkService) checkWSLCodexConfigured(distro string) bool {
	cmd := hideWindowCmd("wsl", "-d", distro, "bash", "-lc", "test -f ~/.codex/config.toml")
	return cmd.Run() == nil
}

// checkWSLGeminiConfigured 检查 WSL 中 Gemini CLI 是否已配置
func (ns *NetworkService) checkWSLGeminiConfigured(distro string) bool {
	cmd := hideWindowCmd("wsl", "-d", distro, "bash", "-lc", "test -f ~/.gemini/.env")
	return cmd.Run() == nil
}

// ReadWSLResolveConf 从 WSL 中读取 /etc/resolv.conf 获取宿主机 IP
// 这是更准确的方法，因为 WSL2 的 nameserver 指向宿主机
func (ns *NetworkService) ReadWSLResolveConf(distro string) string {
	cmd := hideWindowCmd("wsl", "-d", distro, "cat", "/etc/resolv.conf")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "nameserver") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}

	return ""
}
