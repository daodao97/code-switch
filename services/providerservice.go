package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Provider struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	APIURL  string `json:"apiUrl"`
	APIKey  string `json:"apiKey"`
	Site    string `json:"officialSite"`
	Icon    string `json:"icon"`
	Tint    string `json:"tint"`
	Accent  string `json:"accent"`
	Enabled bool   `json:"enabled"`

	// 模型白名单 - Provider 原生支持的模型名
	// 使用 map 实现 O(1) 查找，向后兼容（omitempty）
	SupportedModels map[string]bool `json:"supportedModels,omitempty"`

	// 模型映射 - 外部模型名 -> Provider 内部模型名
	// 支持精确匹配和通配符（如 "claude-*" -> "anthropic/claude-*"）
	ModelMapping map[string]string `json:"modelMapping,omitempty"`

	// 优先级分组 - 数字越小优先级越高（1-10，默认 1）
	// 使用 omitempty 确保零值不序列化，向后兼容
	Level int `json:"level,omitempty"`

	// 内部字段：配置验证错误（不持久化）
	configErrors []string `json:"-"`
}

type providerEnvelope struct {
	Providers []Provider `json:"providers"`
}

type ProviderService struct {
	mu sync.Mutex
}

func NewProviderService() *ProviderService {
	return &ProviderService{}
}

func (ps *ProviderService) Start() error { return nil }
func (ps *ProviderService) Stop() error  { return nil }

func providerFilePath(kind string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".code-switch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var filename string
	switch strings.ToLower(kind) {
	case "claude", "claude-code", "claude_code":
		filename = "claude-code.json"
	case "codex":
		filename = "codex.json"
	default:
		return "", fmt.Errorf("unknown provider type: %s", kind)
	}
	return filepath.Join(dir, filename), nil
}

func (ps *ProviderService) SaveProviders(kind string, providers []Provider) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	path, err := providerFilePath(kind)
	if err != nil {
		return err
	}

	existingProviders, err := ps.LoadProviders(kind)
	if err != nil {
		return err
	}
	nameByID := make(map[int]string, len(existingProviders))
	for _, p := range existingProviders {
		nameByID[p.ID] = p.Name
	}

	// 验证每个 provider 的配置
	validationErrors := make([]string, 0)
	for _, p := range providers {
		// 规则 1：name 不可修改
		if oldName, ok := nameByID[p.ID]; ok && oldName != p.Name {
			return fmt.Errorf("provider id %d 的 name 不可修改", p.ID)
		}

		// 规则 2：验证模型配置
		if errs := p.ValidateConfiguration(); len(errs) > 0 {
			for _, errMsg := range errs {
				validationErrors = append(validationErrors, fmt.Sprintf("[%s] %s", p.Name, errMsg))
			}
		}
	}

	// 如果有验证错误，返回汇总错误
	if len(validationErrors) > 0 {
		return fmt.Errorf("配置验证失败：\n  - %s", strings.Join(validationErrors, "\n  - "))
	}

	data, err := json.MarshalIndent(providerEnvelope{Providers: providers}, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (ps *ProviderService) LoadProviders(kind string) ([]Provider, error) {
	path, err := providerFilePath(kind)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var envelope providerEnvelope
	if len(data) == 0 {
		return []Provider{}, nil
	}

	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	return envelope.Providers, nil
}

// IsModelSupported 检查 provider 是否支持指定的模型
// 支持条件：1) 模型在 SupportedModels 中（精确或通配符匹配）
//          2) 模型在 ModelMapping 的 key 中（精确或通配符匹配）
func (p *Provider) IsModelSupported(modelName string) bool {
	// 向后兼容：如果未配置白名单和映射，假设支持所有模型
	if (p.SupportedModels == nil || len(p.SupportedModels) == 0) &&
		(p.ModelMapping == nil || len(p.ModelMapping) == 0) {
		return true
	}

	// 场景 A：Provider 原生支持该模型（精确匹配）
	if p.SupportedModels != nil && p.SupportedModels[modelName] {
		return true
	}

	// 场景 A+：Provider 原生支持该模型（通配符匹配）
	if p.SupportedModels != nil {
		for supportedModel := range p.SupportedModels {
			if matchWildcard(supportedModel, modelName) {
				return true
			}
		}
	}

	// 场景 B：Provider 通过映射支持该模型（精确匹配）
	if p.ModelMapping != nil {
		if _, exists := p.ModelMapping[modelName]; exists {
			return true
		}

		// 场景 B+：通过通配符映射支持
		for pattern := range p.ModelMapping {
			if matchWildcard(pattern, modelName) {
				return true
			}
		}
	}

	// 场景 C：不支持
	return false
}

// GetEffectiveModel 获取实际应该使用的模型名
// 如果存在映射（精确或通配符），返回映射后的模型名；否则返回原模型名
func (p *Provider) GetEffectiveModel(requestedModel string) string {
	if p.ModelMapping == nil || len(p.ModelMapping) == 0 {
		return requestedModel
	}

	// 优先查找精确映射
	if mappedModel, exists := p.ModelMapping[requestedModel]; exists {
		return mappedModel
	}

	// 查找通配符映射
	for pattern, replacement := range p.ModelMapping {
		if matchWildcard(pattern, requestedModel) {
			return applyWildcardMapping(pattern, replacement, requestedModel)
		}
	}

	// 无映射，返回原模型名
	return requestedModel
}

// ValidateConfiguration 验证 provider 的模型配置
// 返回验证错误列表（空则表示验证通过）
func (p *Provider) ValidateConfiguration() []string {
	errors := make([]string, 0)

	// 规则 1：ModelMapping 的 value 必须在 SupportedModels 中
	if p.ModelMapping != nil && p.SupportedModels != nil {
		for externalModel, internalModel := range p.ModelMapping {
			// 检查是否为通配符映射
			if strings.Contains(internalModel, "*") {
				// 通配符映射暂不验证（需要具体请求才能展开）
				continue
			}

			// 精确映射需要验证
			supported := false
			if p.SupportedModels[internalModel] {
				supported = true
			} else {
				// 检查通配符白名单
				for supportedPattern := range p.SupportedModels {
					if matchWildcard(supportedPattern, internalModel) {
						supported = true
						break
					}
				}
			}

			if !supported {
				errors = append(errors, fmt.Sprintf(
					"模型映射无效：'%s' -> '%s'，目标模型 '%s' 不在 supportedModels 中",
					externalModel, internalModel, internalModel,
				))
			}
		}
	}

	// 规则 2：如果配置了 ModelMapping 但未配置 SupportedModels，给出警告
	if p.ModelMapping != nil && len(p.ModelMapping) > 0 &&
		(p.SupportedModels == nil || len(p.SupportedModels) == 0) {
		errors = append(errors,
			"警告：配置了 modelMapping 但未配置 supportedModels，映射的目标模型无法验证",
		)
	}

	// 规则 3：检测自映射（通常无意义，但不是错误）
	if p.ModelMapping != nil {
		for external, internal := range p.ModelMapping {
			if external == internal {
				errors = append(errors, fmt.Sprintf(
					"警告：模型 '%s' 映射到自身，这通常无意义",
					external,
				))
			}
		}
	}

	p.configErrors = errors
	return errors
}

// matchWildcard 通配符匹配函数
// 支持 * 通配符，如 "claude-*" 匹配 "claude-sonnet-4"
func matchWildcard(pattern, text string) bool {
	// 如果没有通配符，使用精确匹配
	if !strings.Contains(pattern, "*") {
		return pattern == text
	}

	// 简化实现：只支持单个 * 通配符
	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		// 前缀 + * 或 * + 后缀
		prefix, suffix := parts[0], parts[1]
		return strings.HasPrefix(text, prefix) && strings.HasSuffix(text, suffix)
	}

	// 多个 * 的情况（更复杂，暂不支持）
	return false
}

// applyWildcardMapping 应用通配符映射
// 将 pattern 中的 * 匹配部分替换到 replacement 的 * 位置
// 示例: pattern="claude-*", replacement="anthropic/claude-*", input="claude-sonnet-4"
//      输出: "anthropic/claude-sonnet-4"
func applyWildcardMapping(pattern, replacement, input string) string {
	// 如果 pattern 或 replacement 没有通配符，直接返回 replacement
	if !strings.Contains(pattern, "*") || !strings.Contains(replacement, "*") {
		return replacement
	}

	// 提取通配符匹配的部分
	parts := strings.Split(pattern, "*")
	if len(parts) != 2 {
		return replacement // 不支持多个通配符
	}

	prefix, suffix := parts[0], parts[1]

	// 验证 input 确实匹配 pattern
	if !strings.HasPrefix(input, prefix) || !strings.HasSuffix(input, suffix) {
		return replacement
	}

	// 提取中间部分
	wildcardPart := input[len(prefix) : len(input)-len(suffix)]

	// 替换 replacement 中的 *
	return strings.Replace(replacement, "*", wildcardPart, 1)
}
