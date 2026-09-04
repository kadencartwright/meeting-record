package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kadencartwright/meeting-record/internal/audio"
	"github.com/kadencartwright/meeting-record/internal/meeting"
	"github.com/kadencartwright/meeting-record/internal/processing"
)

var (
	ErrAlreadyRecording = errors.New("a meeting recording is already active")
	ErrNotRecording     = errors.New("no meeting recording is active")
	ErrAlreadyPaused    = errors.New("the active recording is already paused")
	ErrNotPaused        = errors.New("the active recording is not paused")
)

type StartedFunc func(State)

type controlRequest struct {
	command string
	done    chan string
}

type Config struct {
	Storage        meeting.Storage
	Runtime        Runtime
	Log            io.Writer
	Started        StartedFunc
	Now            func() time.Time
	MicrophoneNode string
	OutputNode     string
}

func Run(ctx context.Context, config Config) (runErr error) {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Log == nil {
		config.Log = io.Discard
	}
	if err := audio.CheckDependencies(); err != nil {
		return err
	}
	lock, err := config.Runtime.Acquire()
	if err != nil {
		return err
	}
	defer lock.Close()

	devices, err := audio.ResolveSelection(ctx, audio.ExecRunner{}, config.MicrophoneNode, config.OutputNode)
	if err != nil {
		return err
	}
	startedAt := config.Now()
	id, directory, err := config.Storage.Create(startedAt)
	if err != nil {
		return err
	}
	metadata := meeting.NewMetadata(id, startedAt, devices)
	if err := config.Storage.Write(directory, metadata); err != nil {
		return err
	}

	_ = os.Remove(config.Runtime.SocketPath())
	listener, err := net.Listen("unix", config.Runtime.SocketPath())
	if err != nil {
		return fmt.Errorf("listen on control socket: %w", err)
	}
	defer listener.Close()
	if err := os.Chmod(config.Runtime.SocketPath(), 0o600); err != nil {
		return fmt.Errorf("secure control socket: %w", err)
	}
	defer os.Remove(config.Runtime.SocketPath())

	state := State{Recording: true, Session: &SessionState{
		ID: id, StartedAt: startedAt, Directory: directory,
		Microphone: devices.Microphone, Output: devices.Output,
	}}

	results := make(chan audio.Result, 2)
	local := audio.NewLocalRecorder(devices.Microphone, filepath.Join(directory, "local.flac"), config.Log)
	remote := audio.NewRemoteRecorder(devices.Output, filepath.Join(directory, "remote.flac"), config.Log)
	processes := []*audio.Process{local, remote}
	if err := local.Start(); err != nil {
		return failMetadata(config, directory, &metadata, err)
	}
	audio.WaitAsync(local, results)
	if err := remote.Start(); err != nil {
		_ = local.Interrupt()
		<-results
		return failMetadata(config, directory, &metadata, err)
	}
	audio.WaitAsync(remote, results)

	startupTimer := time.NewTimer(audio.StartupGrace)
	select {
	case result := <-results:
		startupTimer.Stop()
		stopAndCollect(processes, results, &result)
		err := fmt.Errorf("%s recorder exited during startup: %v", result.Track, result.Err)
		return failMetadata(config, directory, &metadata, err)
	case <-startupTimer.C:
	case <-ctx.Done():
		stopAndCollect(processes, results, nil)
		return failMetadata(config, directory, &metadata, ctx.Err())
	}

	if err := config.Runtime.WriteState(state); err != nil {
		stopAndCollect(processes, results, nil)
		return failMetadata(config, directory, &metadata, err)
	}
	defer config.Runtime.WriteState(State{Recording: false})
	if config.Started != nil {
		config.Started(state)
	}

	controlRequests := make(chan controlRequest)
	go serveControl(listener, controlRequests)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	var first *audio.Result
	var failure string
	var stopRequest *controlRequest
	var pausedAt *time.Time
	var pausedDuration time.Duration
	running := true
	for running {
		select {
		case <-ctx.Done():
			running = false
		case <-signals:
			running = false
		case received := <-controlRequests:
			switch received.command {
			case "stop":
				stopRequest = &received
				running = false
			case "pause":
				if pausedAt != nil {
					received.done <- ErrAlreadyPaused.Error()
					continue
				}
				if pauseErr := pauseAll(processes); pauseErr != nil {
					received.done <- pauseErr.Error()
					continue
				}
				now := config.Now()
				pausedAt = &now
				state.Paused = true
				state.Session.PausedAt = pausedAt
				state.Session.PausedDurationSeconds = pausedDuration.Seconds()
				if writeErr := config.Runtime.WriteState(state); writeErr != nil {
					_ = resumeAll(processes)
					pausedAt = nil
					state.Paused = false
					state.Session.PausedAt = nil
					received.done <- writeErr.Error()
					continue
				}
				received.done <- "ok"
			case "resume":
				if pausedAt == nil {
					received.done <- ErrNotPaused.Error()
					continue
				}
				if resumeErr := resumeAll(processes); resumeErr != nil {
					received.done <- resumeErr.Error()
					continue
				}
				pausedDuration += config.Now().Sub(*pausedAt)
				pausedAt = nil
				state.Paused = false
				state.Session.PausedAt = nil
				state.Session.PausedDurationSeconds = pausedDuration.Seconds()
				if writeErr := config.Runtime.WriteState(state); writeErr != nil {
					received.done <- writeErr.Error()
					continue
				}
				received.done <- "ok"
			}
		case result := <-results:
			first = &result
			failure = fmt.Sprintf("%s recorder exited unexpectedly: %v", result.Track, result.Err)
			running = false
		}
	}

	if pausedAt != nil {
		_ = resumeAll(processes)
		pausedDuration += config.Now().Sub(*pausedAt)
	}
	stopAndCollect(processes, results, first)
	metadata.FinishWithPaused(config.Now(), pausedDuration, failure)
	if failure == "" {
		mergeContext, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		merged, mergeErr := processing.Merge(mergeContext, processing.ExecRunner{}, directory, metadata)
		cancel()
		if mergeErr != nil {
			metadata.MergeFailure = mergeErr.Error()
			fmt.Fprintf(config.Log, "meeting-record: %s\n", metadata.MergeFailure)
		} else {
			metadata.Merged = &merged
			metadata.MergeFailure = ""
		}
	}
	if err := config.Storage.Write(directory, metadata); err != nil {
		runErr = err
	}
	if err := config.Runtime.WriteState(State{Recording: false}); err != nil && runErr == nil {
		runErr = err
	}
	if stopRequest != nil {
		if runErr != nil {
			stopRequest.done <- runErr.Error()
		} else {
			stopRequest.done <- "ok"
		}
	}
	if failure != "" && runErr == nil {
		runErr = errors.New(failure)
	}
	return runErr
}

func failMetadata(config Config, directory string, metadata *meeting.Metadata, cause error) error {
	metadata.Finish(config.Now(), cause.Error())
	if err := config.Storage.Write(directory, *metadata); err != nil {
		return fmt.Errorf("%v; additionally failed to update metadata: %w", cause, err)
	}
	return cause
}

func stopAndCollect(processes []*audio.Process, results <-chan audio.Result, first *audio.Result) {
	remaining := len(processes)
	if first != nil {
		remaining--
	}
	for _, process := range processes {
		_ = process.Interrupt()
	}
	remaining = collectWithin(results, remaining, audio.StopGrace)
	if remaining == 0 {
		return
	}
	for _, process := range processes {
		_ = process.Terminate(syscall.SIGTERM)
	}
	remaining = collectWithin(results, remaining, audio.KillGrace)
	if remaining == 0 {
		return
	}
	for _, process := range processes {
		_ = process.Terminate(syscall.SIGKILL)
	}
	for remaining > 0 {
		<-results
		remaining--
	}
}

func collectWithin(results <-chan audio.Result, remaining int, timeout time.Duration) int {
	if remaining == 0 {
		return 0
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for remaining > 0 {
		select {
		case <-results:
			remaining--
		case <-timer.C:
			return remaining
		}
	}
	return 0
}

func pauseAll(processes []*audio.Process) error {
	for index, process := range processes {
		if err := process.Pause(); err != nil {
			for previous := 0; previous < index; previous++ {
				_ = processes[previous].Resume()
			}
			return fmt.Errorf("pause %s recorder: %w", process.Track, err)
		}
	}
	return nil
}

func resumeAll(processes []*audio.Process) error {
	var first error
	for _, process := range processes {
		if err := process.Resume(); err != nil && first == nil {
			first = fmt.Errorf("resume %s recorder: %w", process.Track, err)
		}
	}
	return first
}

func serveControl(listener net.Listener, requests chan<- controlRequest) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go handleControl(connection, requests)
	}
}

func handleControl(connection net.Conn, requests chan<- controlRequest) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(11 * time.Minute))
	command, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		return
	}
	switch strings.TrimSpace(command) {
	case "ping":
		_, _ = fmt.Fprintln(connection, "pong")
	case "stop", "pause", "resume":
		done := make(chan string, 1)
		requests <- controlRequest{command: strings.TrimSpace(command), done: done}
		_, _ = fmt.Fprintln(connection, <-done)
	default:
		_, _ = fmt.Fprintln(connection, "unknown command")
	}
}
