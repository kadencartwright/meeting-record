package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspectReconcilesStaleState(t *testing.T) {
	runtime := Runtime{Directory: t.TempDir()}
	old := State{Recording: true, Session: &SessionState{ID: "old", StartedAt: time.Now()}}
	if err := runtime.WriteState(old); err != nil {
		t.Fatal(err)
	}
	state, stale, err := runtime.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if !stale || state.Session == nil || state.Session.ID != "old" {
		t.Fatalf("expected stale state details, got state=%#v stale=%v", state, stale)
	}
	written, err := runtime.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if written.Recording {
		t.Fatal("stale state was not rewritten to idle")
	}
}

func TestInspectRecoversMalformedState(t *testing.T) {
	runtime := Runtime{Directory: t.TempDir()}
	if err := os.WriteFile(filepath.Join(runtime.Directory, "state.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, stale, err := runtime.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if !stale || state.Recording {
		t.Fatalf("expected recovered idle state, got %#v stale=%v", state, stale)
	}
}

func TestAcquirePreventsSecondSupervisor(t *testing.T) {
	runtime := Runtime{Directory: t.TempDir()}
	first, err := runtime.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := runtime.Acquire(); !errors.Is(err, ErrAlreadyRecording) {
		t.Fatalf("Acquire() error = %v, want ErrAlreadyRecording", err)
	}
}
