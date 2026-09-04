package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	for _, test := range []struct {
		input time.Duration
		want  string
	}{
		{4 * time.Second, "00:04"},
		{12*time.Minute + 37*time.Second, "12:37"},
		{time.Hour + 4*time.Minute + 19*time.Second, "1:04:19"},
	} {
		if got := formatDuration(test.input); got != test.want {
			t.Errorf("formatDuration(%s) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestHelp(t *testing.T) {
	var output bytes.Buffer
	app := application{out: &output, errOut: &output}
	if err := app.run([]string{"help"}); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("help output was empty")
	}
}

func TestParseUploadOptionsAllowsFlagsAroundSession(t *testing.T) {
	options, err := parseUploadOptions([]string{"--parent-page", "page-id", "session-id", "--json", "--title", "Weekly sync"})
	if err != nil {
		t.Fatal(err)
	}
	if options.SessionID != "session-id" || options.ParentPage != "page-id" || options.Title != "Weekly sync" || !options.JSON {
		t.Fatalf("unexpected options: %#v", options)
	}
}

func TestParseUploadOptionsAcceptsDestination(t *testing.T) {
	options, err := parseUploadOptions([]string{"session-id", "--destination", "team"})
	if err != nil {
		t.Fatal(err)
	}
	if options.SessionID != "session-id" || options.Destination != "team" {
		t.Fatalf("unexpected options: %#v", options)
	}
}

func TestResolveDestination(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("MEETING_RECORD_NOTION_PARENT_PAGE_ID", "")
	directory := filepath.Join(configRoot, "meeting-record")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	configuration := `{"notion":{"destinations":[{"id":"team","label":"Team meetings","parentPageId":"team-page"},{"id":"personal","label":"Personal notes","parentPageId":"personal-page"}]}}`
	if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}

	destination, err := resolveDestination(uploadOptions{Destination: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	if destination.ID != "personal" || destination.Label != "Personal notes" || destination.ParentPageID != "personal-page" {
		t.Fatalf("unexpected destination: %#v", destination)
	}
	if _, err := resolveDestination(uploadOptions{}); err == nil {
		t.Fatal("expected selecting no destination with multiple configured to fail")
	}
	if _, err := resolveDestination(uploadOptions{Destination: "missing"}); err == nil {
		t.Fatal("expected unknown destination to fail")
	}
}
