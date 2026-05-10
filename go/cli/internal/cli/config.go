package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type appConfig struct {
	Auth     authConfig     `json:"auth,omitempty"`
	Projects projectsConfig `json:"projects,omitempty"`
}

type authConfig struct {
	Hetzner hetznerConfig `json:"hetzner,omitempty"`
}

type hetznerConfig struct {
	Token string `json:"token,omitempty"`
}

type projectsConfig struct {
	LocalRoot  string `json:"localRoot,omitempty"`
	RemoteRoot string `json:"remoteRoot,omitempty"`
}

func (cfg appConfig) MarshalJSON() ([]byte, error) {
	type appConfigJSON struct {
		Auth     *authConfig     `json:"auth,omitempty"`
		Projects *projectsConfig `json:"projects,omitempty"`
	}

	var out appConfigJSON
	if !cfg.Auth.isZero() {
		out.Auth = &cfg.Auth
	}
	if !cfg.Projects.isZero() {
		out.Projects = &cfg.Projects
	}

	return json.Marshal(out)
}

func (cfg authConfig) isZero() bool {
	return cfg.Hetzner.isZero()
}

func (cfg hetznerConfig) isZero() bool {
	return cfg.Token == ""
}

func (cfg projectsConfig) isZero() bool {
	return cfg.LocalRoot == "" && cfg.RemoteRoot == ""
}

func defaultAppConfigPath(env func(string) string) (string, error) {
	if path := env("ME_CONFIG"); path != "" {
		return path, nil
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(configDir, "me", "config.json"), nil
}

func loadAppConfig(path string) (appConfig, error) {
	var cfg appConfig

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

func saveAppConfig(path string, cfg appConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure config directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure config file: %w", err)
	}

	return nil
}
