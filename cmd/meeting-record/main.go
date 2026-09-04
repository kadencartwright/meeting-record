package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kadencartwright/meeting-record/internal/audio"
	"github.com/kadencartwright/meeting-record/internal/meeting"
	"github.com/kadencartwright/meeting-record/internal/supervisor"
)

const usage = `meeting-record records the default microphone and the complete default output sink.

Usage:
  meeting-record devices [--json]
  meeting-record start [--detach]
  meeting-record stop
  meeting-record status [--json]
  meeting-record list [--json]
  meeting-record show <session> [--json]
  meeting-record open <session>
  meeting-record play <session> [local|remote]
  meeting-record delete <session>
`

type application struct {
	storage meeting.Storage
	runtime supervisor.Runtime
	out     io.Writer
	errOut  io.Writer
}

func main() {
	storage, err := meeting.DefaultStorage()
	if err != nil {
		fmt.Fprintln(os.Stderr, "meeting-record:", err)
		os.Exit(1)
	}
	app := application{storage: storage, runtime: supervisor.DefaultRuntime(), out: os.Stdout, errOut: os.Stderr}
	if err := app.run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "meeting-record:", err)
		switch {
		case errors.Is(err, supervisor.ErrAlreadyRecording):
			os.Exit(3)
		case errors.Is(err, supervisor.ErrNotRecording):
			os.Exit(4)
		default:
			os.Exit(1)
		}
	}
}

func (app application) run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(app.out, usage)
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(app.out, usage)
		return nil
	case "devices":
		return app.devices(args[1:])
	case "start":
		return app.start(args[1:])
	case "stop":
		return app.stop(args[1:])
	case "status":
		return app.status(args[1:])
	case "list":
		return app.list(args[1:])
	case "show":
		return app.show(args[1:])
	case "open":
		return app.open(args[1:])
	case "play":
		return app.play(args[1:])
	case "delete":
		return app.delete(args[1:])
	case "_supervise":
		return app.supervise(args[1:])
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

func (app application) devices(args []string) error {
	jsonOutput, rest, err := parseJSONFlag("devices", args)
	if err != nil || len(rest) != 0 {
		if err != nil {
			return err
		}
		return fmt.Errorf("devices accepts only --json")
	}
	if err := audio.CheckDependencies(); err != nil {
		return err
	}
	devices, err := audio.Discover(context.Background(), audio.ExecRunner{})
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSON(app.out, devices)
	}
	fmt.Fprintf(app.out, "Output:\n  description: %s\n  node: %s\n\nMicrophone:\n  description: %s\n  node: %s\n",
		devices.Output.Description, devices.Output.NodeName,
		devices.Microphone.Description, devices.Microphone.NodeName)
	return nil
}

func (app application) start(args []string) error {
	flags := flag.NewFlagSet("start", flag.ContinueOnError)
	flags.SetOutput(app.errOut)
	detach := flags.Bool("detach", false, "run the recording supervisor independently")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("start takes no positional arguments")
	}
	state, _, err := app.inspect()
	if err != nil {
		return err
	}
	if state.Recording {
		return supervisor.ErrAlreadyRecording
	}
	if *detach {
		return app.startDetached()
	}
	var started supervisor.State
	err = supervisor.Run(context.Background(), supervisor.Config{
		Storage: app.storage, Runtime: app.runtime, Log: app.errOut,
		Started: func(state supervisor.State) {
			started = state
			fmt.Fprintf(app.out, "\nRecording meeting\n\n  microphone  %s\n  output      %s\n  directory   %s\n\nPress Ctrl-C to stop.\n",
				state.Session.Microphone.Description, state.Session.Output.Description, state.Session.Directory)
		},
	})
	if started.Session != nil {
		fmt.Fprintf(app.out, "\nRecording saved\n\n  duration    %s\n  local       local.flac\n  remote      remote.flac\n  directory   %s\n",
			formatDuration(time.Since(started.Session.StartedAt)), started.Session.Directory)
	}
	return err
}

type readyMessage struct {
	OK        bool   `json:"ok"`
	SessionID string `json:"sessionId,omitempty"`
	Directory string `json:"directory,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (app application) startDetached() error {
	if err := app.runtime.Prepare(); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	logFile, err := os.OpenFile(app.runtime.LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open supervisor log: %w", err)
	}
	defer logFile.Close()
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create supervisor readiness pipe: %w", err)
	}
	defer readPipe.Close()
	command := exec.Command(executable, "_supervise", "--ready-fd", "3")
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	command.ExtraFiles = []*os.File{writePipe}
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		writePipe.Close()
		return fmt.Errorf("start detached supervisor: %w", err)
	}
	writePipe.Close()

	ready := make(chan readyMessage, 1)
	go func() {
		var message readyMessage
		if err := json.NewDecoder(readPipe).Decode(&message); err != nil {
			message.Error = "supervisor exited before confirming startup: " + err.Error()
		}
		ready <- message
	}()
	select {
	case message := <-ready:
		if !message.OK {
			if message.Error == "" {
				message.Error = "supervisor did not confirm startup"
			}
			return errors.New(message.Error)
		}
		fmt.Fprintf(app.out, "Recording started\n\n  session     %s\n  directory   %s\n", message.SessionID, message.Directory)
		return nil
	case <-time.After(8 * time.Second):
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		return fmt.Errorf("timed out waiting for recording supervisor startup")
	}
}

func (app application) supervise(args []string) error {
	flags := flag.NewFlagSet("_supervise", flag.ContinueOnError)
	readyFD := flags.Int("ready-fd", -1, "internal readiness file descriptor")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *readyFD < 0 {
		return fmt.Errorf("missing readiness file descriptor")
	}
	readyFile := os.NewFile(uintptr(*readyFD), "ready")
	if readyFile == nil {
		return fmt.Errorf("invalid readiness file descriptor")
	}
	defer readyFile.Close()
	sent := false
	send := func(message readyMessage) {
		if sent {
			return
		}
		sent = true
		_ = json.NewEncoder(readyFile).Encode(message)
		_ = readyFile.Close()
	}
	err := supervisor.Run(context.Background(), supervisor.Config{
		Storage: app.storage, Runtime: app.runtime, Log: app.errOut,
		Started: func(state supervisor.State) {
			send(readyMessage{OK: true, SessionID: state.Session.ID, Directory: state.Session.Directory})
		},
	})
	if err != nil && !sent {
		send(readyMessage{Error: err.Error()})
	}
	return err
}

func (app application) stop(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("stop takes no arguments")
	}
	state, _, err := app.inspect()
	if err != nil {
		return err
	}
	if !state.Recording || state.Session == nil {
		return supervisor.ErrNotRecording
	}
	if err := app.runtime.Stop(); err != nil {
		return err
	}
	fmt.Fprintf(app.out, "Recording saved\n\n  session     %s\n  directory   %s\n", state.Session.ID, state.Session.Directory)
	return nil
}

func (app application) status(args []string) error {
	jsonOutput, rest, err := parseJSONFlag("status", args)
	if err != nil || len(rest) != 0 {
		if err != nil {
			return err
		}
		return fmt.Errorf("status accepts only --json")
	}
	state, stale, err := app.inspect()
	if err != nil {
		return err
	}
	if stale {
		state = supervisor.State{Recording: false}
	}
	if jsonOutput {
		return writeJSON(app.out, state)
	}
	if !state.Recording || state.Session == nil {
		fmt.Fprintln(app.out, "Not recording")
		return nil
	}
	fmt.Fprintf(app.out, "Recording\n\n  session     %s\n  elapsed     %s\n  microphone  %s\n  output      %s\n  directory   %s\n",
		state.Session.ID, formatDuration(time.Since(state.Session.StartedAt)), state.Session.Microphone.Description,
		state.Session.Output.Description, state.Session.Directory)
	return nil
}

func (app application) list(args []string) error {
	jsonOutput, rest, err := parseJSONFlag("list", args)
	if err != nil || len(rest) != 0 {
		if err != nil {
			return err
		}
		return fmt.Errorf("list accepts only --json")
	}
	if _, _, err := app.inspect(); err != nil {
		return err
	}
	result, err := app.storage.List()
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSON(app.out, result)
	}
	if len(result.Sessions) == 0 {
		fmt.Fprintln(app.out, "No recordings")
		return nil
	}
	for _, session := range result.Sessions {
		fmt.Fprintf(app.out, "%s  %-10s  %s\n", session.ID, formatDuration(time.Duration(session.DurationSeconds)*time.Second), session.Status)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintln(app.errOut, "warning:", warning)
	}
	return nil
}

type showResult struct {
	Directory string           `json:"directory"`
	Metadata  meeting.Metadata `json:"metadata"`
}

func (app application) show(args []string) error {
	jsonOutput, rest, err := parseJSONFlag("show", args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: meeting-record show <session> [--json]")
	}
	metadata, directory, err := app.storage.Load(rest[0])
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSON(app.out, showResult{Directory: directory, Metadata: metadata})
	}
	ended := "recording"
	if metadata.EndedAt != nil {
		ended = metadata.EndedAt.Format(time.RFC3339)
	}
	fmt.Fprintf(app.out, "Session %s\n\n  started     %s\n  ended       %s\n  duration    %s\n  status      %s\n  microphone  %s\n  output      %s\n  directory   %s\n",
		metadata.ID, metadata.StartedAt.Format(time.RFC3339), ended,
		formatDuration(time.Duration(metadata.DurationSeconds)*time.Second), metadata.Status,
		metadata.Local.Description, metadata.Remote.Description, directory)
	return nil
}

func (app application) open(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: meeting-record open <session>")
	}
	_, directory, err := app.storage.Load(args[0])
	if err != nil {
		return err
	}
	return launch("xdg-open", directory)
}

func (app application) play(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: meeting-record play <session> [local|remote]")
	}
	track := "remote"
	if len(args) == 2 {
		track = args[1]
	}
	metadata, directory, err := app.storage.Load(args[0])
	if err != nil {
		return err
	}
	filename := metadata.Remote.File
	if track == "local" {
		filename = metadata.Local.File
	} else if track != "remote" {
		return fmt.Errorf("track must be local or remote")
	}
	path := filepath.Join(directory, filename)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("recording file unavailable: %w", err)
	}
	return launch("xdg-open", path)
}

func (app application) delete(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: meeting-record delete <session>")
	}
	state, _, err := app.inspect()
	if err != nil {
		return err
	}
	if state.Recording && state.Session != nil && state.Session.ID == args[0] {
		return fmt.Errorf("cannot delete the active recording")
	}
	if err := app.storage.Delete(args[0]); err != nil {
		return err
	}
	fmt.Fprintln(app.out, "Deleted", args[0])
	return nil
}

func (app application) inspect() (supervisor.State, bool, error) {
	state, stale, err := app.runtime.Inspect()
	if err != nil {
		return state, stale, err
	}
	if stale && state.Session != nil {
		metadata, directory, loadErr := app.storage.Load(state.Session.ID)
		if loadErr == nil && metadata.EndedAt == nil {
			metadata.Finish(time.Now(), "recording supervisor exited unexpectedly")
			_ = app.storage.Write(directory, metadata)
		}
	}
	if stale {
		return supervisor.State{Recording: false}, true, nil
	}
	return state, stale, nil
}

func launch(binary string, argument string) error {
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("required desktop opener %q was not found in PATH", binary)
	}
	command := exec.Command(binary, argument)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %s", binary, strings.TrimSpace(string(output)))
	}
	return nil
}

func parseJSONFlag(name string, args []string) (bool, []string, error) {
	jsonOutput := false
	rest := make([]string, 0, len(args))
	for _, argument := range args {
		if argument == "--json" {
			jsonOutput = true
		} else if strings.HasPrefix(argument, "-") {
			return false, nil, fmt.Errorf("%s: unknown option %s", name, argument)
		} else {
			rest = append(rest, argument)
		}
	}
	return jsonOutput, rest, nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func formatDuration(duration time.Duration) string {
	seconds := int64(duration.Round(time.Second) / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	remaining := seconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, remaining)
	}
	return fmt.Sprintf("%02d:%02d", minutes, remaining)
}
