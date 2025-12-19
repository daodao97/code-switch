package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Provider struct {
	ID              int64             `json:"id"`
	Name            string            `json:"name"`
	APIURL          string            `json:"apiUrl"`
	APIKey          string            `json:"apiKey"`
	Enabled         bool              `json:"enabled"`
	Level           int               `json:"level,omitempty"`
	SupportedModels map[string]bool   `json:"supportedModels,omitempty"`
	ModelMapping    map[string]string `json:"modelMapping,omitempty"`
}

type Config struct {
	Providers []Provider `json:"providers"`
}

// IsModelSupported 检查 provider 是否支持指定的模型
func (p *Provider) IsModelSupported(modelName string) bool {
	// 向后兼容：如果未配置白名单和映射，假设支持所有模型
	if (p.SupportedModels == nil || len(p.SupportedModels) == 0) &&
		(p.ModelMapping == nil || len(p.ModelMapping) == 0) {
		return true
	}
	return false // 简化版本，仅测试向后兼容逻辑
}

func main() {
	home, _ := os.UserHomeDir()
	filePath := filepath.Join(home, ".code-switch", "claude-code.json")

	fmt.Printf("读取配置文件: %s\n", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		fmt.Printf("解析错误: %v\n", err)
		return
	}

	fmt.Printf("\n找到 %d 个 provider:\n\n", len(config.Providers))

	testModel := "claude-sonnet-4-5-20250929"
	for i, p := range config.Providers {
		fmt.Printf("[%d] %s\n", i+1, p.Name)
		fmt.Printf("  - Enabled: %v\n", p.Enabled)
		fmt.Printf("  - SupportedModels: %v (nil: %v, len: %d)\n",
			p.SupportedModels, p.SupportedModels == nil, len(p.SupportedModels))
		fmt.Printf("  - ModelMapping: %v (nil: %v, len: %d)\n",
			p.ModelMapping, p.ModelMapping == nil, len(p.ModelMapping))

		supported := p.IsModelSupported(testModel)
		fmt.Printf("  - IsModelSupported(%s): %v\n", testModel, supported)

		if !supported && p.Enabled {
			fmt.Printf("  ⚠️  问题：已启用但不支持模型！\n")
		}
		fmt.Println()
	}
}
