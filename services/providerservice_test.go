package services

import (
	"encoding/json"
	"sort"
	"testing"
)

// ==================== 通配符匹配测试 ====================

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		text     string
		expected bool
	}{
		// 精确匹配
		{
			name:     "精确匹配-成功",
			pattern:  "claude-sonnet-4",
			text:     "claude-sonnet-4",
			expected: true,
		},
		{
			name:     "精确匹配-失败",
			pattern:  "claude-sonnet-4",
			text:     "claude-opus-4",
			expected: false,
		},

		// 前缀通配符
		{
			name:     "前缀通配符-成功",
			pattern:  "claude-*",
			text:     "claude-sonnet-4",
			expected: true,
		},
		{
			name:     "前缀通配符-多段匹配",
			pattern:  "claude-*",
			text:     "claude-sonnet-4-latest",
			expected: true,
		},
		{
			name:     "前缀通配符-失败",
			pattern:  "claude-*",
			text:     "gpt-4",
			expected: false,
		},

		// 后缀通配符
		{
			name:     "后缀通配符-成功",
			pattern:  "*-4",
			text:     "claude-sonnet-4",
			expected: true,
		},
		{
			name:     "后缀通配符-失败",
			pattern:  "*-4",
			text:     "claude-sonnet-3.5",
			expected: false,
		},

		// 中间通配符
		{
			name:     "中间通配符-成功",
			pattern:  "claude-*-4",
			text:     "claude-sonnet-4",
			expected: true,
		},
		{
			name:     "中间通配符-多段匹配",
			pattern:  "claude-*-4",
			text:     "claude-opus-mini-4",
			expected: true,
		},
		{
			name:     "中间通配符-失败前缀",
			pattern:  "claude-*-4",
			text:     "gpt-sonnet-4",
			expected: false,
		},
		{
			name:     "中间通配符-失败后缀",
			pattern:  "claude-*-4",
			text:     "claude-sonnet-3",
			expected: false,
		},

		// 边界情况
		{
			name:     "空前缀",
			pattern:  "*-sonnet",
			text:     "claude-sonnet",
			expected: true,
		},
		{
			name:     "空后缀",
			pattern:  "claude-*",
			text:     "claude-",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWildcard(tt.pattern, tt.text)
			if result != tt.expected {
				t.Errorf("matchWildcard(%q, %q) = %v, 期望 %v",
					tt.pattern, tt.text, result, tt.expected)
			}
		})
	}
}

// ==================== 通配符映射应用测试 ====================

func TestApplyWildcardMapping(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		replacement string
		input       string
		expected    string
	}{
		// 前缀通配符映射
		{
			name:        "前缀通配符映射",
			pattern:     "claude-*",
			replacement: "anthropic/claude-*",
			input:       "claude-sonnet-4",
			expected:    "anthropic/claude-sonnet-4",
		},
		{
			name:        "前缀通配符映射-多段",
			pattern:     "claude-*",
			replacement: "anthropic/claude-*",
			input:       "claude-opus-4-latest",
			expected:    "anthropic/claude-opus-4-latest",
		},

		// 中间通配符映射
		{
			name:        "中间通配符映射",
			pattern:     "claude-*-4",
			replacement: "anthropic/claude-*-v4",
			input:       "claude-sonnet-4",
			expected:    "anthropic/claude-sonnet-v4",
		},

		// 无通配符（直接返回 replacement）
		{
			name:        "无通配符-pattern",
			pattern:     "claude-sonnet-4",
			replacement: "anthropic/claude-sonnet-4",
			input:       "claude-sonnet-4",
			expected:    "anthropic/claude-sonnet-4",
		},
		{
			name:        "无通配符-replacement",
			pattern:     "claude-*",
			replacement: "fixed-model",
			input:       "claude-sonnet-4",
			expected:    "fixed-model",
		},

		// 边界情况
		{
			name:        "空匹配部分",
			pattern:     "claude-*",
			replacement: "anthropic/claude-*",
			input:       "claude-",
			expected:    "anthropic/claude-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyWildcardMapping(tt.pattern, tt.replacement, tt.input)
			if result != tt.expected {
				t.Errorf("applyWildcardMapping(%q, %q, %q) = %q, 期望 %q",
					tt.pattern, tt.replacement, tt.input, result, tt.expected)
			}
		})
	}
}

// ==================== IsModelSupported 测试 ====================

func TestProvider_IsModelSupported(t *testing.T) {
	tests := []struct {
		name      string
		provider  Provider
		modelName string
		expected  bool
	}{
		// 向后兼容：未配置白名单和映射
		{
			name:      "向后兼容-未配置",
			provider:  Provider{},
			modelName: "any-model",
			expected:  true,
		},

		// 场景 A：原生支持（精确匹配）
		{
			name: "原生支持-精确匹配-成功",
			provider: Provider{
				SupportedModels: map[string]bool{
					"claude-sonnet-4": true,
					"claude-opus-4":   true,
				},
			},
			modelName: "claude-sonnet-4",
			expected:  true,
		},
		{
			name: "原生支持-精确匹配-失败",
			provider: Provider{
				SupportedModels: map[string]bool{
					"claude-sonnet-4": true,
				},
			},
			modelName: "gpt-4",
			expected:  false,
		},

		// 场景 A+：原生支持（通配符匹配）
		{
			name: "原生支持-通配符匹配-成功",
			provider: Provider{
				SupportedModels: map[string]bool{
					"claude-*": true,
				},
			},
			modelName: "claude-sonnet-4",
			expected:  true,
		},
		{
			name: "原生支持-通配符匹配-失败",
			provider: Provider{
				SupportedModels: map[string]bool{
					"claude-*": true,
				},
			},
			modelName: "gpt-4",
			expected:  false,
		},

		// 场景 B：映射支持（精确匹配）
		{
			name: "映射支持-精确匹配-成功",
			provider: Provider{
				SupportedModels: map[string]bool{
					"anthropic/claude-sonnet-4": true,
				},
				ModelMapping: map[string]string{
					"claude-sonnet-4": "anthropic/claude-sonnet-4",
				},
			},
			modelName: "claude-sonnet-4",
			expected:  true,
		},

		// 场景 B+：映射支持（通配符匹配）
		{
			name: "映射支持-通配符匹配-成功",
			provider: Provider{
				SupportedModels: map[string]bool{
					"anthropic/claude-*": true,
				},
				ModelMapping: map[string]string{
					"claude-*": "anthropic/claude-*",
				},
			},
			modelName: "claude-sonnet-4",
			expected:  true,
		},

		// 混合模式
		{
			name: "混合模式-原生+映射",
			provider: Provider{
				SupportedModels: map[string]bool{
					"native-model":    true,
					"vendor/external": true,
				},
				ModelMapping: map[string]string{
					"external": "vendor/external",
				},
			},
			modelName: "external",
			expected:  true,
		},
		{
			name: "混合模式-只在原生",
			provider: Provider{
				SupportedModels: map[string]bool{
					"native-model": true,
				},
				ModelMapping: map[string]string{
					"external": "vendor/external",
				},
			},
			modelName: "native-model",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.provider.IsModelSupported(tt.modelName)
			if result != tt.expected {
				t.Errorf("IsModelSupported(%q) = %v, 期望 %v",
					tt.modelName, result, tt.expected)
			}
		})
	}
}

// ==================== GetEffectiveModel 测试 ====================

func TestProvider_GetEffectiveModel(t *testing.T) {
	tests := []struct {
		name           string
		provider       Provider
		requestedModel string
		expected       string
	}{
		// 无映射
		{
			name:           "无映射-返回原名",
			provider:       Provider{},
			requestedModel: "claude-sonnet-4",
			expected:       "claude-sonnet-4",
		},

		// 精确映射
		{
			name: "精确映射-成功",
			provider: Provider{
				ModelMapping: map[string]string{
					"claude-sonnet-4": "anthropic/claude-sonnet-4",
				},
			},
			requestedModel: "claude-sonnet-4",
			expected:       "anthropic/claude-sonnet-4",
		},
		{
			name: "精确映射-无匹配",
			provider: Provider{
				ModelMapping: map[string]string{
					"claude-sonnet-4": "anthropic/claude-sonnet-4",
				},
			},
			requestedModel: "gpt-4",
			expected:       "gpt-4",
		},

		// 通配符映射
		{
			name: "通配符映射-前缀",
			provider: Provider{
				ModelMapping: map[string]string{
					"claude-*": "anthropic/claude-*",
				},
			},
			requestedModel: "claude-sonnet-4",
			expected:       "anthropic/claude-sonnet-4",
		},
		{
			name: "通配符映射-中间",
			provider: Provider{
				ModelMapping: map[string]string{
					"claude-*-4": "anthropic/claude-*-v4",
				},
			},
			requestedModel: "claude-sonnet-4",
			expected:       "anthropic/claude-sonnet-v4",
		},

		// 精确优先于通配符
		{
			name: "精确映射优先",
			provider: Provider{
				ModelMapping: map[string]string{
					"claude-sonnet-4": "exact-match",
					"claude-*":        "wildcard-match",
				},
			},
			requestedModel: "claude-sonnet-4",
			expected:       "exact-match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.provider.GetEffectiveModel(tt.requestedModel)
			if result != tt.expected {
				t.Errorf("GetEffectiveModel(%q) = %q, 期望 %q",
					tt.requestedModel, result, tt.expected)
			}
		})
	}
}

// ==================== ValidateConfiguration 测试 ====================

func TestProvider_ValidateConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		provider      Provider
		expectErrors  bool
		errorContains string
	}{
		// 有效配置
		{
			name: "有效配置-完整",
			provider: Provider{
				Name: "test-provider",
				SupportedModels: map[string]bool{
					"model-a":          true,
					"internal-model-b": true,
				},
				ModelMapping: map[string]string{
					"external-model-b": "internal-model-b",
				},
			},
			expectErrors: false,
		},

		// 无效映射：目标模型不在白名单
		{
			name: "无效映射-目标不在白名单",
			provider: Provider{
				Name: "test-provider",
				SupportedModels: map[string]bool{
					"model-a": true,
				},
				ModelMapping: map[string]string{
					"external": "model-b",
				},
			},
			expectErrors:  true,
			errorContains: "不在 supportedModels 中",
		},

		// 警告：只配置映射未配置白名单
		{
			name: "警告-无白名单",
			provider: Provider{
				Name: "test-provider",
				ModelMapping: map[string]string{
					"external": "internal",
				},
			},
			expectErrors:  true,
			errorContains: "未配置 supportedModels",
		},

		// 警告：自映射
		{
			name: "警告-自映射",
			provider: Provider{
				Name: "test-provider",
				SupportedModels: map[string]bool{
					"model-a": true,
				},
				ModelMapping: map[string]string{
					"model-a": "model-a",
				},
			},
			expectErrors:  true,
			errorContains: "映射到自身",
		},

		// 通配符映射（不验证）
		{
			name: "通配符映射-跳过验证",
			provider: Provider{
				Name: "test-provider",
				SupportedModels: map[string]bool{
					"anthropic/claude-*": true,
				},
				ModelMapping: map[string]string{
					"claude-*": "anthropic/claude-*",
				},
			},
			expectErrors: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := tt.provider.ValidateConfiguration()

			if tt.expectErrors && len(errors) == 0 {
				t.Errorf("期望有验证错误，但没有返回错误")
			}

			if !tt.expectErrors && len(errors) > 0 {
				t.Errorf("不期望有验证错误，但返回了: %v", errors)
			}

			if tt.expectErrors && tt.errorContains != "" {
				found := false
				for _, err := range errors {
					if containsString(err, tt.errorContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("期望错误信息包含 %q，但实际错误是: %v", tt.errorContains, errors)
				}
			}
		})
	}
}

// ==================== Level 分组测试 ====================

func TestProviderLevelGrouping(t *testing.T) {
	tests := []struct {
		name      string
		providers []Provider
		expected  map[int][]string // level -> provider names
	}{
		{
			name: "默认 Level（未设置）",
			providers: []Provider{
				{ID: 1, Name: "Provider-A", Level: 0}, // 0 应默认为 1
				{ID: 2, Name: "Provider-B"},            // 未设置应默认为 1
			},
			expected: map[int][]string{
				1: {"Provider-A", "Provider-B"},
			},
		},
		{
			name: "多个 Level 分组",
			providers: []Provider{
				{ID: 1, Name: "Provider-L1-A", Level: 1},
				{ID: 2, Name: "Provider-L2-A", Level: 2},
				{ID: 3, Name: "Provider-L1-B", Level: 1},
				{ID: 4, Name: "Provider-L3-A", Level: 3},
			},
			expected: map[int][]string{
				1: {"Provider-L1-A", "Provider-L1-B"},
				2: {"Provider-L2-A"},
				3: {"Provider-L3-A"},
			},
		},
		{
			name: "保持同 Level 内顺序",
			providers: []Provider{
				{ID: 1, Name: "First", Level: 1},
				{ID: 2, Name: "Second", Level: 1},
				{ID: 3, Name: "Third", Level: 1},
			},
			expected: map[int][]string{
				1: {"First", "Second", "Third"},
			},
		},
		{
			name: "Level 10 到 Level 1 混合",
			providers: []Provider{
				{ID: 1, Name: "L10", Level: 10},
				{ID: 2, Name: "L1", Level: 1},
				{ID: 3, Name: "L5", Level: 5},
			},
			expected: map[int][]string{
				1:  {"L1"},
				5:  {"L5"},
				10: {"L10"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟分组逻辑
			levelGroups := make(map[int][]Provider)
			for _, provider := range tt.providers {
				level := provider.Level
				if level <= 0 {
					level = 1 // 默认 Level 1
				}
				levelGroups[level] = append(levelGroups[level], provider)
			}

			// 验证分组结果
			for expectedLevel, expectedNames := range tt.expected {
				actualProviders, exists := levelGroups[expectedLevel]
				if !exists {
					t.Errorf("Level %d 不存在，期望有 %d 个 provider", expectedLevel, len(expectedNames))
					continue
				}

				if len(actualProviders) != len(expectedNames) {
					t.Errorf("Level %d 的 provider 数量不匹配：实际 %d，期望 %d",
						expectedLevel, len(actualProviders), len(expectedNames))
					continue
				}

				// 验证顺序
				for i, expectedName := range expectedNames {
					if actualProviders[i].Name != expectedName {
						t.Errorf("Level %d 位置 %d：实际 %q，期望 %q",
							expectedLevel, i, actualProviders[i].Name, expectedName)
					}
				}
			}

			// 验证没有额外的 Level
			if len(levelGroups) != len(tt.expected) {
				t.Errorf("Level 分组数量不匹配：实际 %d，期望 %d",
					len(levelGroups), len(tt.expected))
			}
		})
	}
}

func TestProviderLevelOrdering(t *testing.T) {
	tests := []struct {
		name     string
		levels   []int
		expected []int
	}{
		{
			name:     "升序排序",
			levels:   []int{3, 1, 2},
			expected: []int{1, 2, 3},
		},
		{
			name:     "已排序",
			levels:   []int{1, 2, 3, 4, 5},
			expected: []int{1, 2, 3, 4, 5},
		},
		{
			name:     "逆序",
			levels:   []int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
			expected: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:     "重复 Level（实际不应出现，但算法应处理）",
			levels:   []int{2, 1, 2, 3, 1},
			expected: []int{1, 1, 2, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 使用 Go 的 sort 包（与实际代码一致）
			levels := make([]int, len(tt.levels))
			copy(levels, tt.levels)
			sort.Ints(levels)

			for i, expected := range tt.expected {
				if levels[i] != expected {
					t.Errorf("位置 %d：实际 %d，期望 %d", i, levels[i], expected)
				}
			}
		})
	}
}

func TestProviderLevelJSON(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		expected string
	}{
		{
			name: "Level 设置为 2",
			provider: Provider{
				ID:    1,
				Name:  "Test",
				Level: 2,
			},
			expected: `"level":2`,
		},
		{
			name: "Level 未设置（零值，应 omitempty）",
			provider: Provider{
				ID:    1,
				Name:  "Test",
				Level: 0,
			},
			expected: "", // omitempty 应该不序列化 level 字段
		},
		{
			name: "Level 设置为 1",
			provider: Provider{
				ID:    1,
				Name:  "Test",
				Level: 1,
			},
			expected: `"level":1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.provider)
			if err != nil {
				t.Fatalf("JSON 序列化失败: %v", err)
			}

			jsonStr := string(data)
			if tt.expected == "" {
				// 验证 level 字段不存在
				if containsString(jsonStr, `"level"`) {
					t.Errorf("期望 level 字段被 omit，但在 JSON 中找到: %s", jsonStr)
				}
			} else {
				// 验证 level 字段存在且正确
				if !containsString(jsonStr, tt.expected) {
					t.Errorf("期望 JSON 包含 %q，但实际是: %s", tt.expected, jsonStr)
				}
			}
		})
	}
}

// 辅助函数
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
