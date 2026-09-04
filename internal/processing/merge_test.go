package processing

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kadencartwright/meeting-record/internal/meeting"
)

type mergeRunner struct {
	name string
	args []string
}

func (runner *mergeRunner) Run(_ context.Context, name string, args []string, _ io.Reader) ([]byte, error) {
	runner.name = name
	runner.args = append([]string(nil), args...)
	return nil, os.WriteFile(args[len(args)-1], []byte("merged"), 0o600)
}

func TestMergeKeepsSourcesAndAtomicallyPublishesMeetingTrack(t *testing.T) {
	directory := t.TempDir()
	for _, filename := range []string{"local.flac", "remote.flac"} {
		if err := os.WriteFile(filepath.Join(directory, filename), []byte(filename), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	metadata := meeting.Metadata{
		Local:  meeting.Track{File: "local.flac"},
		Remote: meeting.Track{File: "remote.flac"},
	}
	runner := &mergeRunner{}
	result, err := Merge(context.Background(), runner, directory, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if runner.name != "ffmpeg" || result.File != "meeting.m4a" || result.Channels != 2 {
		t.Fatalf("unexpected merge result %#v via %q", result, runner.name)
	}
	if _, err := os.Stat(filepath.Join(directory, "meeting.m4a")); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"local.flac", "remote.flac"} {
		if _, err := os.Stat(filepath.Join(directory, filename)); err != nil {
			t.Fatalf("source track %s was removed: %v", filename, err)
		}
	}
	if !containsSequence(runner.args, []string{"-ar", "48000", "-ac", "2", "-c:a", "aac", "-b:a", "192k"}) {
		t.Fatalf("ffmpeg arguments do not preserve the requested output format: %#v", runner.args)
	}
}

func containsSequence(values, sequence []string) bool {
	for index := 0; index+len(sequence) <= len(values); index++ {
		match := true
		for offset := range sequence {
			if values[index+offset] != sequence[offset] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
