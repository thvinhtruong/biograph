package config

import (
	"fmt"
	"runtime"

	"github.com/spf13/viper"
)

// Config is the root configuration structure mirroring biograph.yaml.
type Config struct {
	Vault      VaultConfig      `mapstructure:"vault"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Search     SearchConfig     `mapstructure:"search"`
	Activation ActivationConfig `mapstructure:"activation"`
	Plasticity PlasticityConfig `mapstructure:"plasticity"`
	LLM        LLMConfig        `mapstructure:"llm"`
	Ingestion  IngestionConfig  `mapstructure:"ingestion"`
}

type VaultConfig struct {
	Path      string `mapstructure:"path"`
	EntityDir string `mapstructure:"entity_dir"`
	SourceDir string `mapstructure:"source_dir"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

type SearchConfig struct {
	BleveIndexPath          string  `mapstructure:"bleve_index_path"`
	BleveConfidenceThreshold float64 `mapstructure:"bleve_confidence_threshold"`
	LLMFallbackEnabled      bool    `mapstructure:"llm_fallback_enabled"`
}

type ActivationConfig struct {
	MaxHops         int     `mapstructure:"max_hops"`
	MinEnergy       float64 `mapstructure:"min_energy"`
	DecayPerHop     float64 `mapstructure:"decay_per_hop"`
	MaxContextNodes int     `mapstructure:"max_context_nodes"`
	MaxContextTokens int    `mapstructure:"max_context_tokens"`
}

type PlasticityConfig struct {
	ReinforcementAlpha float64 `mapstructure:"reinforcement_alpha"`
	SigmoidCenter      float64 `mapstructure:"sigmoid_center"`
	DecayEnabled       bool    `mapstructure:"decay_enabled"`
	DecayInterval      string  `mapstructure:"decay_interval"`
}

type LLMConfig struct {
	Provider   string `mapstructure:"provider"`
	Model      string `mapstructure:"model"`
	VLMModel   string `mapstructure:"vlm_model"`
	APIKeyEnv  string `mapstructure:"api_key_env"`
	MaxRetries int    `mapstructure:"max_retries"`
}

type IngestionConfig struct {
	Workers         int    `mapstructure:"workers"`
	ForceVLM        bool   `mapstructure:"force_vlm"`
	PDFToTextPath   string `mapstructure:"pdftotext_path"`
	DefaultCourse   string `mapstructure:"default_course"`
}

// Load reads config from Viper and applies defaults.
func Load(v *viper.Viper) (*Config, error) {
	setDefaults(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	applyDerivedDefaults(&cfg)
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("vault.entity_dir", "entities")
	v.SetDefault("vault.source_dir", "sources")
	v.SetDefault("database.path", "./biograph.db")
	v.SetDefault("search.bleve_index_path", "./biograph.bleve")
	v.SetDefault("search.bleve_confidence_threshold", 0.5)
	v.SetDefault("search.llm_fallback_enabled", true)
	v.SetDefault("activation.max_hops", 2)
	v.SetDefault("activation.min_energy", 0.05)
	v.SetDefault("activation.decay_per_hop", 0.7)
	v.SetDefault("activation.max_context_nodes", 20)
	v.SetDefault("activation.max_context_tokens", 6000)
	v.SetDefault("plasticity.reinforcement_alpha", 0.05)
	v.SetDefault("plasticity.sigmoid_center", 0.5)
	v.SetDefault("plasticity.decay_enabled", true)
	v.SetDefault("plasticity.decay_interval", "24h")
	v.SetDefault("llm.provider", "anthropic")
	v.SetDefault("llm.model", "claude-haiku-4-5-20251001")
	v.SetDefault("llm.vlm_model", "claude-haiku-4-5-20251001")
	v.SetDefault("llm.api_key_env", "ANTHROPIC_API_KEY")
	v.SetDefault("llm.max_retries", 3)
	v.SetDefault("ingestion.workers", runtime.NumCPU())
	v.SetDefault("ingestion.force_vlm", false)
	v.SetDefault("ingestion.pdftotext_path", "pdftotext")
}

func applyDerivedDefaults(cfg *Config) {
	if cfg.Ingestion.Workers <= 0 {
		cfg.Ingestion.Workers = runtime.NumCPU()
	}
}
