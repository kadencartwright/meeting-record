package meeting

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Destination struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	ParentPageID string `json:"parentPageId"`
}

type Config struct {
	Notion struct {
		Destinations []Destination `json:"destinations"`
	} `json:"notion"`
}

func DefaultConfigPath() (string, error) {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "meeting-record", "config.json"), nil
}

func LoadConfig() (Config, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read recorder configuration: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse recorder configuration: %w", err)
	}
	seen := make(map[string]bool)
	for index := range config.Notion.Destinations {
		destination := &config.Notion.Destinations[index]
		destination.ID = strings.TrimSpace(destination.ID)
		destination.Label = strings.TrimSpace(destination.Label)
		destination.ParentPageID = strings.TrimSpace(destination.ParentPageID)
		if destination.ID == "" || destination.ParentPageID == "" {
			return Config{}, fmt.Errorf("Notion destination %d requires id and parentPageId", index+1)
		}
		if seen[destination.ID] {
			return Config{}, fmt.Errorf("duplicate Notion destination id %q", destination.ID)
		}
		seen[destination.ID] = true
		if destination.Label == "" {
			destination.Label = destination.ID
		}
	}
	return config, nil
}
