package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type appConfig struct {
	Auth           authConfig           `json:"auth,omitempty"`
	Projects       projectsConfig       `json:"projects,omitempty"`
	SSH            sshConfig            `json:"ssh,omitempty"`
	PersonalServer personalServerConfig `json:"personalServer,omitempty"`
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

type sshConfig struct {
	IdentityFile string `json:"identityFile,omitempty"`
}

type personalServerConfig struct {
	ServerID int    `json:"serverID,omitempty"`
	User     string `json:"user,omitempty"`
	IPv4     string `json:"ipv4,omitempty"`
	IPv6     string `json:"ipv6,omitempty"`
}

type personalServerConnectionConfigState int

const (
	personalServerConnectionConfigAbsent personalServerConnectionConfigState = iota
	personalServerConnectionConfigIncomplete
	personalServerConnectionConfigMissingAddress
	personalServerConnectionConfigReady
)

type personalServerConnectionConfig struct {
	User string
	Host string
}

func (cfg appConfig) MarshalJSON() ([]byte, error) {
	type appConfigJSON struct {
		Auth           *authConfig           `json:"auth,omitempty"`
		Projects       *projectsConfig       `json:"projects,omitempty"`
		SSH            *sshConfig            `json:"ssh,omitempty"`
		PersonalServer *personalServerConfig `json:"personalServer,omitempty"`
	}

	var out appConfigJSON
	if !cfg.Auth.isZero() {
		out.Auth = &cfg.Auth
	}
	if !cfg.Projects.isZero() {
		out.Projects = &cfg.Projects
	}
	if !cfg.SSH.isZero() {
		out.SSH = &cfg.SSH
	}
	if !cfg.PersonalServer.isZero() {
		out.PersonalServer = &cfg.PersonalServer
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

func (cfg sshConfig) isZero() bool {
	return cfg.IdentityFile == ""
}

func (cfg personalServerConfig) isZero() bool {
	return cfg.ServerID == 0 && cfg.User == "" && cfg.IPv4 == "" && cfg.IPv6 == ""
}

func (cfg personalServerConfig) connectionConfigState() (personalServerConnectionConfigState, personalServerConnectionConfig) {
	if cfg.isZero() {
		return personalServerConnectionConfigAbsent, personalServerConnectionConfig{}
	}
	user := strings.TrimSpace(cfg.User)
	if cfg.ServerID == 0 || user == "" {
		return personalServerConnectionConfigIncomplete, personalServerConnectionConfig{}
	}
	host := personalServerSSHHost(cfg.IPv4, cfg.IPv6)
	if host == "" {
		return personalServerConnectionConfigMissingAddress, personalServerConnectionConfig{}
	}
	return personalServerConnectionConfigReady, personalServerConnectionConfig{
		User: user,
		Host: host,
	}
}

func (state personalServerConnectionConfigState) validationError() error {
	switch state {
	case personalServerConnectionConfigAbsent, personalServerConnectionConfigIncomplete:
		return fmt.Errorf("Personal Server Configuration is incomplete; run `myn configure`")
	case personalServerConnectionConfigMissingAddress:
		return fmt.Errorf("Personal Server Configuration is missing a saved Personal Server address; run `myn configure`")
	default:
		return nil
	}
}

func defaultAppConfigPath(env func(string) string) (string, error) {
	if path := env("MYN_CONFIG"); path != "" {
		return path, nil
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(configDir, "myn", "config.json"), nil
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
	_, statErr := os.Stat(dir)
	createdDir := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !createdDir {
		return fmt.Errorf("stat config directory: %w", statErr)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if createdDir {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("secure config directory: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure config file: %w", err)
	}

	return nil
}
