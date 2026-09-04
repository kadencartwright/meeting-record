package supervisor

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kadencartwright/meeting-record/internal/audio"
)

type SessionState struct {
	ID                    string       `json:"id"`
	StartedAt             time.Time    `json:"startedAt"`
	Directory             string       `json:"directory"`
	Microphone            audio.Device `json:"microphone"`
	Output                audio.Device `json:"output"`
	PausedAt              *time.Time   `json:"pausedAt,omitempty"`
	PausedDurationSeconds float64      `json:"pausedDurationSeconds,omitempty"`
}

type State struct {
	Recording bool          `json:"recording"`
	Paused    bool          `json:"paused"`
	Session   *SessionState `json:"session"`
}

type Runtime struct {
	Directory string
}

func DefaultRuntime() Runtime {
	root := os.Getenv("XDG_RUNTIME_DIR")
	if root == "" {
		root = filepath.Join(os.TempDir(), fmt.Sprintf("meeting-record-%d", os.Getuid()))
	}
	return Runtime{Directory: filepath.Join(root, "meeting-record")}
}

func (r Runtime) StatePath() string  { return filepath.Join(r.Directory, "state.json") }
func (r Runtime) SocketPath() string { return filepath.Join(r.Directory, "control.sock") }
func (r Runtime) LogPath() string    { return filepath.Join(r.Directory, "supervisor.log") }
func (r Runtime) StartupErrorPath() string {
	return filepath.Join(r.Directory, "startup-error")
}

func (r Runtime) Prepare() error {
	if err := os.MkdirAll(r.Directory, 0o700); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	return os.Chmod(r.Directory, 0o700)
}

func (r Runtime) Acquire() (*os.File, error) {
	if err := r.Prepare(); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(r.Directory, "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open supervisor lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRecording
		}
		return nil, fmt.Errorf("lock supervisor state: %w", err)
	}
	return lock, nil
}

func (r Runtime) WriteState(state State) error {
	if err := r.Prepare(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize runtime state: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(r.StatePath(), data)
}

func (r Runtime) WriteStartupError(message string) error {
	if err := r.Prepare(); err != nil {
		return err
	}
	return atomicWrite(r.StartupErrorPath(), []byte(message+"\n"))
}

func (r Runtime) ReadState() (State, error) {
	data, err := os.ReadFile(r.StatePath())
	if errors.Is(err, os.ErrNotExist) {
		return State{Recording: false}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read runtime state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse runtime state: %w", err)
	}
	return state, nil
}

// Inspect verifies state against the control socket. A state file alone never
// proves that a recorder is alive. stale is true when a formerly active state
// was reconciled to idle.
func (r Runtime) Inspect() (state State, stale bool, err error) {
	if _, statErr := os.Stat(r.StatePath()); errors.Is(statErr, os.ErrNotExist) {
		if writeErr := r.WriteState(State{Recording: false}); writeErr != nil {
			return State{}, false, writeErr
		}
	}
	state, err = r.ReadState()
	if err != nil {
		// Malformed ephemeral state is recoverable when no supervisor owns the lock.
		if lock, lockErr := r.Acquire(); lockErr == nil {
			lock.Close()
			_ = os.Remove(r.SocketPath())
			idle := State{Recording: false}
			if writeErr := r.WriteState(idle); writeErr != nil {
				return State{}, false, writeErr
			}
			return idle, true, nil
		}
		return State{}, false, err
	}
	if !state.Recording {
		return state, false, nil
	}
	if r.ping() == nil {
		return state, false, nil
	}
	lock, lockErr := r.Acquire()
	if lockErr != nil {
		// A supervisor owns the lock and may still be publishing its socket.
		return state, false, nil
	}
	lock.Close()
	_ = os.Remove(r.SocketPath())
	if err := r.WriteState(State{Recording: false}); err != nil {
		return State{}, false, err
	}
	return state, true, nil
}

func (r Runtime) ping() error {
	connection, err := net.DialTimeout("unix", r.SocketPath(), 750*time.Millisecond)
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := fmt.Fprintln(connection, "ping"); err != nil {
		return err
	}
	reply, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		return err
	}
	if reply != "pong\n" {
		return fmt.Errorf("unexpected supervisor response %q", reply)
	}
	return nil
}

func (r Runtime) Stop() error {
	return r.request("stop", 11*time.Minute)
}

func (r Runtime) Pause() error {
	return r.request("pause", 5*time.Second)
}

func (r Runtime) Resume() error {
	return r.request("resume", 5*time.Second)
}

func (r Runtime) request(command string, timeout time.Duration) error {
	state, stale, err := r.Inspect()
	if err != nil {
		return err
	}
	if stale || !state.Recording {
		return ErrNotRecording
	}
	if command == "pause" && state.Paused {
		return ErrAlreadyPaused
	}
	if command == "resume" && !state.Paused {
		return ErrNotPaused
	}
	connection, err := net.DialTimeout("unix", r.SocketPath(), time.Second)
	if err != nil {
		return fmt.Errorf("connect to recording supervisor: %w", err)
	}
	defer connection.Close()
	// Finalization includes encoding the merged M4A after both FLAC writers
	// close, so allow long recordings enough time to finish cleanly.
	_ = connection.SetDeadline(time.Now().Add(timeout))
	if _, err := fmt.Fprintln(connection, command); err != nil {
		return fmt.Errorf("request recording %s: %w", command, err)
	}
	reply, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		return fmt.Errorf("wait for recording %s: %w", command, err)
	}
	if reply != "ok\n" {
		return fmt.Errorf("supervisor %s failed: %s", command, strings.TrimSpace(reply))
	}
	return nil
}

func atomicWrite(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
