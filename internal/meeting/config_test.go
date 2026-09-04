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
	data := []byte(`{"notion":{"destinations":[{"id":"team","label":"Team meetings","parentPageId":"team-page"},{"id":"personal","label":"Personal notes","parentPageId":"personal-page"}]},"externalRecorders":[{"id":"voice-memos","label":"Voice recorder","filesystemUuid":"5AA7-563B","recordingsPath":"RECORD"}]}`)
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
	if len(config.ExternalRecorders) != 1 || config.ExternalRecorders[0].RecordingsPath != "RECORD" {
		t.Fatalf("unexpected external recorders: %#v", config.ExternalRecorders)
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
