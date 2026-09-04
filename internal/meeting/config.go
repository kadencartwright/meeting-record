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

type ExternalRecorder struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	FilesystemUUID string `json:"filesystemUuid"`
	RecordingsPath string `json:"recordingsPath"`
}

type Config struct {
	Notion struct {
		Destinations []Destination `json:"destinations"`
	} `json:"notion"`
	ExternalRecorders []ExternalRecorder `json:"externalRecorders"`
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
	seen = make(map[string]bool)
	for index := range config.ExternalRecorders {
		recorder := &config.ExternalRecorders[index]
		recorder.ID = strings.TrimSpace(recorder.ID)
		recorder.Label = strings.TrimSpace(recorder.Label)
		recorder.FilesystemUUID = strings.TrimSpace(recorder.FilesystemUUID)
		recorder.RecordingsPath = filepath.Clean(strings.TrimSpace(recorder.RecordingsPath))
		if recorder.ID == "" || strings.ContainsAny(recorder.ID, `/\\`) || recorder.FilesystemUUID == "" {
			return Config{}, fmt.Errorf("external recorder %d requires a safe id and filesystemUuid", index+1)
		}
		if seen[recorder.ID] {
			return Config{}, fmt.Errorf("duplicate external recorder id %q", recorder.ID)
		}
		seen[recorder.ID] = true
		if recorder.Label == "" {
			recorder.Label = recorder.ID
		}
		if recorder.RecordingsPath == "." {
			recorder.RecordingsPath = ""
		}
		if filepath.IsAbs(recorder.RecordingsPath) || recorder.RecordingsPath == ".." || strings.HasPrefix(recorder.RecordingsPath, ".."+string(filepath.Separator)) {
			return Config{}, fmt.Errorf("external recorder %q has an unsafe recordingsPath", recorder.ID)
		}
	}
	return config, nil
}
