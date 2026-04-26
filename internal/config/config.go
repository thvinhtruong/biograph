package config

import (
	"fmt"
	"runtime"

	"github.com/spf13/viper"
)

// Config is the root configuration structure mirroring biograph.yaml.
type Config struct {
	Output   OutputConfig   `mapstructure:"output"`
	Database DatabaseConfig `mapstructure:"database"`
	LLM      LLMConfig      `mapstructure:"llm"`
	Ingestion IngestionConfig `mapstructure:"ingestion"`
}

// OutputConfig controls where generated markdown files are written.
type OutputConfig struct {
	ContentDir string `mapstructure:"content_dir"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

type LLMConfig struct {
	Provider   string `mapstructure:"provider"`
	Model      string `mapstructure:"model"`
	APIKeyEnv  string `mapstructure:"api_key_env"`
	MaxRetries int    `mapstructure:"max_retries"`
}

type IngestionConfig struct {
	Workers int `mapstructure:"workers"`
}

// Load reads config from Viper and applies defaults.
func Load(v *viper.Viper) (*Config, error) {
	setDefaults(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.Ingestion.Workers <= 0 {
		cfg.Ingestion.Workers = runtime.NumCPU()
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("output.content_dir", "./courses")
	v.SetDefault("database.path", "./biograph.db")
	v.SetDefault("llm.provider", "anthropic")
	v.SetDefault("llm.model", "claude-haiku-4-5-20251001")
	v.SetDefault("llm.api_key_env", "ANTHROPIC_API_KEY")
	v.SetDefault("llm.max_retries", 3)
	v.SetDefault("ingestion.workers", runtime.NumCPU())
}
