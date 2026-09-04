package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kadencartwright/meeting-record/internal/meeting"
)

type fakeRunner struct {
	mountPoint string
}

func (runner fakeRunner) Output(_ context.Context, name string, _ ...string) ([]byte, error) {
	if name == "lsblk" {
		return []byte(`{"blockdevices":[{"path":"/dev/sda","uuid":"5AA7-563B","mountpoints":["` + runner.mountPoint + `"]}]}`), nil
	}
	if name == "ffprobe" {
		return []byte(`{"streams":[{"codec_name":"pcm_s16le","sample_rate":"16000","channels":2}],"format":{"duration":"25.664000"}}`), nil
	}
	return nil, nil
}

func TestDiscoverRecorderFiles(t *testing.T) {
	mountPoint := t.TempDir()
	recordings := filepath.Join(mountPoint, "RECORD")
	if err := os.Mkdir(recordings, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recordings, "R20260518012830.WAV"), []byte("wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recordings, "ignore.txt"), []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := Discover(context.Background(), fakeRunner{mountPoint: mountPoint}, []meeting.ExternalRecorder{{
		ID: "voice-memos", Label: "Voice recorder", FilesystemUUID: "5AA7-563B", RecordingsPath: "RECORD",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Recorders) != 1 || len(inventory.Recorders[0].Files) != 1 {
		t.Fatalf("unexpected inventory: %#v", inventory)
	}
	file := inventory.Recorders[0].Files[0]
	if file.ID != "voice-memos/R20260518012830.WAV" || file.Codec != "pcm_s16le" || file.SampleRate != 16000 || file.Channels != 2 || file.DurationSeconds != 25.664 {
		t.Fatalf("unexpected file: %#v", file)
	}
}

func TestRegistryIgnoresReplacedSourceFile(t *testing.T) {
	registry := Registry{Uploads: map[string]Upload{}}
	first := File{ID: "voice/file.wav", Fingerprint: "voice|file.wav|100|1"}
	registry.Put(first, meeting.NotionExport{PageID: "page"})
	if _, found := registry.Lookup(first); !found {
		t.Fatal("expected original file to be recognized")
	}
	replaced := first
	replaced.Fingerprint = "voice|file.wav|200|2"
	if _, found := registry.Lookup(replaced); found {
		t.Fatal("replaced file must not inherit the old upload")
	}
}
