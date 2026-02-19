package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const configFile = "config.json"

// CLIConfig holds user-level CLI configuration.
type CLIConfig struct {
	APIURL           string `json:"api_url,omitempty"`
	FrontendURL      string `json:"frontend_url,omitempty"`
	DefaultLocalHost string `json:"default_local_host,omitempty"`
	AutoReconnect    *bool  `json:"auto_reconnect,omitempty"`
	Inspect          bool   `json:"inspect,omitempty"`
}

// DefaultCLIConfig returns the built-in defaults.
func DefaultCLIConfig() CLIConfig {
	autoReconnect := true
	return CLIConfig{
		APIURL:           "https://api.launchtunnel.dev",
		FrontendURL:      "https://app.launchtunnel.dev",
		DefaultLocalHost: "127.0.0.1",
		AutoReconnect:    &autoReconnect,
		Inspect:          false,
	}
}

// ConfigPath returns the default config file path, or the override if set.
func ConfigPath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, dirName, configFile), nil
}

// LoadCLIConfig reads the CLI config file. Returns defaults if the file does not exist.
func LoadCLIConfig(path string) (CLIConfig, error) {
	cfg := DefaultCLIConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config: %w", err)
	}

	// Re-apply defaults for nil pointers.
	if cfg.AutoReconnect == nil {
		autoReconnect := true
		cfg.AutoReconnect = &autoReconnect
	}
	if cfg.DefaultLocalHost == "" {
		cfg.DefaultLocalHost = "127.0.0.1"
	}
	if cfg.APIURL == "" {
		cfg.APIURL = "https://api.launchtunnel.dev"
	}
	if cfg.FrontendURL == "" {
		cfg.FrontendURL = "https://app.launchtunnel.dev"
	}

	return cfg, nil
}
