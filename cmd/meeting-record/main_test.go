package main

import (
	"bytes"
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
