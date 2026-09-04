package processing

import (
	"context"
	"io"
	"os"
	"testing"
)

type transcodeRunner struct {
	args []string
}

func (runner *transcodeRunner) Run(_ context.Context, name string, args []string, _ io.Reader) ([]byte, error) {
	runner.args = append([]string(nil), args...)
	if name != "ffmpeg" {
		return nil, os.ErrInvalid
	}
	return nil, os.WriteFile(args[len(args)-1], []byte("m4a"), 0o600)
}

func TestTranscodeCreatesUploadFriendlyM4A(t *testing.T) {
	destination := t.TempDir() + "/voice.m4a"
	runner := &transcodeRunner{}
	if err := Transcode(context.Background(), runner, "/recorder/voice.wav", destination); err != nil {
		t.Fatal(err)
	}
	if !containsSequence(runner.args, []string{"-i", "/recorder/voice.wav"}) || !containsSequence(runner.args, []string{"-ar", "48000", "-c:a", "aac"}) {
		t.Fatalf("unexpected ffmpeg arguments: %#v", runner.args)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
}
