package meeting

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDestinations(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	directory := filepath.Join(root, "meeting-record")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"notion":{"destinations":[{"id":"team","label":"Team meetings","parentPageId":"team-page"},{"id":"personal","label":"Personal notes","parentPageId":"personal-page"}]}}`)
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Notion.Destinations) != 2 || config.Notion.Destinations[1].ID != "personal" {
		t.Fatalf("unexpected destinations: %#v", config.Notion.Destinations)
	}
}

func TestLoadConfigRejectsDuplicateDestination(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	directory := filepath.Join(root, "meeting-record")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"notion":{"destinations":[{"id":"same","parentPageId":"one"},{"id":"same","parentPageId":"two"}]}}`)
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected duplicate destination to fail")
	}
}
