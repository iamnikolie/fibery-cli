package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	APIToken  string `yaml:"api_token"`
	Workspace string `yaml:"workspace"`
}

func (c *Config) BaseURL() string {
	return fmt.Sprintf("https://%s.fibery.io", c.Workspace)
}

func (c *Config) Validate() error {
	if c.APIToken == "" {
		return fmt.Errorf("api_token not set: run 'fibery config init' or set FIBERY_API_TOKEN")
	}
	if c.Workspace == "" {
		return fmt.Errorf("workspace not set: run 'fibery config init' or set FIBERY_WORKSPACE")
	}
	return nil
}

// fiberyHome returns the config directory for the given account.
// Empty account → ~/.fibery (or FIBERY_HOME for tests).
// Non-empty account → ~/.fibery/<account>.
func fiberyHome(account string) (string, error) {
	var base string
	if h := os.Getenv("FIBERY_HOME"); h != "" {
		base = h
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".fibery")
	}
	if account != "" {
		return filepath.Join(base, account), nil
	}
	return base, nil
}

// Load reads config from the account's directory.
// Env vars FIBERY_API_TOKEN and FIBERY_WORKSPACE override file values.
func Load(account string) (*Config, error) {
	cfg := &Config{
		APIToken:  os.Getenv("FIBERY_API_TOKEN"),
		Workspace: os.Getenv("FIBERY_WORKSPACE"),
	}

	dir, err := fiberyHome(account)
	if err != nil {
		return nil, fmt.Errorf("config.Load: %w", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("config.Load: %w", err)
	}
	if err == nil {
		var fileCfg Config
		if parseErr := yaml.Unmarshal(data, &fileCfg); parseErr != nil {
			return nil, fmt.Errorf("config.Load: parse: %w", parseErr)
		}
		if cfg.APIToken == "" {
			cfg.APIToken = fileCfg.APIToken
		}
		if cfg.Workspace == "" {
			cfg.Workspace = fileCfg.Workspace
		}
	}

	return cfg, nil
}

// Save writes config to the account's directory (~/.fibery/<account>/config.yaml).
func Save(token, workspace, account string) error {
	dir, err := fiberyHome(account)
	if err != nil {
		return fmt.Errorf("config.Save: %w", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("config.Save: mkdir: %w", err)
	}
	data, err := yaml.Marshal(Config{APIToken: token, Workspace: workspace})
	if err != nil {
		return fmt.Errorf("config.Save: marshal: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600)
}
