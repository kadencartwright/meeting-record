package meeting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kadencartwright/meeting-record/internal/audio"
)

func TestSessionIDIsFilesystemSafe(t *testing.T) {
	when := time.Date(2026, 9, 4, 9, 42, 13, 0, time.FixedZone("CDT", -5*60*60))
	if got, want := SessionID(when), "2026-09-04T09-42-13"; got != want {
		t.Fatalf("SessionID() = %q, want %q", got, want)
	}
}

func TestCreateAvoidsCollision(t *testing.T) {
	storage := Storage{Root: t.TempDir()}
	when := time.Date(2026, 9, 4, 9, 42, 13, 0, time.UTC)
	first, _, err := storage.Create(when)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := storage.Create(when)
	if err != nil {
		t.Fatal(err)
	}
	if first != "2026-09-04T09-42-13" || second != first+"-2" {
		t.Fatalf("unexpected ids: %q, %q", first, second)
	}
}

func TestMetadataRoundTripAndNewestFirst(t *testing.T) {
	storage := Storage{Root: t.TempDir()}
	devices := audio.Devices{
		Microphone: audio.Device{Description: "Mic", NodeName: "source.name"},
		Output:     audio.Device{Description: "Headphones", NodeName: "sink.name"},
	}
	for _, hour := range []int{8, 10} {
		started := time.Date(2026, 9, 4, hour, 0, 0, 0, time.UTC)
		id, directory, err := storage.Create(started)
		if err != nil {
			t.Fatal(err)
		}
		metadata := NewMetadata(id, started, devices)
		metadata.Finish(started.Add(time.Minute), "")
		metadata.Merged = &AudioFile{File: "meeting.m4a", Channels: 2}
		if err := storage.Write(directory, metadata); err != nil {
			t.Fatal(err)
		}
	}
	result, err := storage.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 2 || result.Sessions[0].StartedAt.Hour() != 10 {
		t.Fatalf("sessions not newest-first: %#v", result.Sessions)
	}
	data, err := os.ReadFile(filepath.Join(result.Sessions[0].Directory, "meeting.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded Metadata
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.DurationSeconds != 60 || decoded.Status != "complete" {
		t.Fatalf("unexpected metadata: %#v", decoded)
	}
	if decoded.Merged == nil || decoded.Merged.File != "meeting.m4a" || result.Sessions[0].MergedFile == "" {
		t.Fatalf("merged audio was not serialized: %#v", decoded)
	}
}

func TestDeleteRejectsTraversal(t *testing.T) {
	storage := Storage{Root: t.TempDir()}
	if err := storage.Delete("../elsewhere"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}
