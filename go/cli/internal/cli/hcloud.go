package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type hcloudConfigFile struct {
	ActiveContext string                `toml:"active_context"`
	Contexts      []hcloudContextConfig `toml:"contexts"`
}

type hcloudContextConfig struct {
	Name  string `toml:"name"`
	Token string `toml:"token"`
}

type hcloudTokenCandidate struct {
	Name   string
	Token  string
	Active bool
}

func defaultHcloudConfigPath(env func(string) string) (string, error) {
	if path := env("HCLOUD_CONFIG"); path != "" {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find user home directory: %w", err)
	}
	return filepath.Join(home, ".config", "hcloud", "cli.toml"), nil
}

func loadHcloudTokenCandidates(path string) ([]hcloudTokenCandidate, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hcloud config: %w", err)
	}

	var cfg hcloudConfigFile
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse hcloud config: %w", err)
	}

	candidates := make([]hcloudTokenCandidate, 0, len(cfg.Contexts))
	for _, context := range cfg.Contexts {
		if context.Name == "" || context.Token == "" {
			continue
		}
		candidates = append(candidates, hcloudTokenCandidate{
			Name:   context.Name,
			Token:  context.Token,
			Active: context.Name == cfg.ActiveContext,
		})
	}

	return candidates, nil
}

func findHcloudTokenCandidate(candidates []hcloudTokenCandidate, name string) (hcloudTokenCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.Name == name {
			return candidate, true
		}
	}
	return hcloudTokenCandidate{}, false
}
