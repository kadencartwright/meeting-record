package audio

import (
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"
)

type Track string

const (
	LocalTrack  Track = "local"
	RemoteTrack Track = "remote"
)

type Process struct {
	Track Track
	Cmd   *exec.Cmd
}

func NewLocalRecorder(device Device, output string, log io.Writer) *Process {
	properties := `{ stream.monitor = true node.name = "meeting-record-local" media.name = "Meeting Recorder Local" }`
	return newRecorder(LocalTrack, device.NodeName, output, 1, "mono", properties, log)
}

func NewRemoteRecorder(device Device, output string, log io.Writer) *Process {
	properties := `{ stream.capture.sink = true node.name = "meeting-record-remote" media.name = "Meeting Recorder Remote" }`
	return newRecorder(RemoteTrack, device.NodeName, output, 2, "stereo", properties, log)
}

func newRecorder(track Track, target, output string, channels int, channelMap, properties string, log io.Writer) *Process {
	cmd := exec.Command("pw-record",
		"--target", target,
		"--rate", "48000",
		"--channels", fmt.Sprint(channels),
		"--channel-map", channelMap,
		"--container", "flac",
		"--properties", properties,
		output,
	)
	cmd.Stdin = nil
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &Process{Track: track, Cmd: cmd}
}

func (p *Process) Start() error {
	if err := p.Cmd.Start(); err != nil {
		return fmt.Errorf("start %s recorder: %w", p.Track, err)
	}
	return nil
}

func (p *Process) Interrupt() error {
	return p.signal(syscall.SIGINT)
}

func (p *Process) Pause() error {
	return p.signal(syscall.SIGSTOP)
}

func (p *Process) Resume() error {
	return p.signal(syscall.SIGCONT)
}

func (p *Process) signal(signal syscall.Signal) error {
	if p.Cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-p.Cmd.Process.Pid, signal)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

func (p *Process) Terminate(signal syscall.Signal) error {
	return p.signal(signal)
}

type Result struct {
	Track Track
	Err   error
}

func WaitAsync(process *Process, results chan<- Result) {
	go func() {
		results <- Result{Track: process.Track, Err: process.Cmd.Wait()}
	}()
}

const (
	StartupGrace = 350 * time.Millisecond
	StopGrace    = 5 * time.Second
	KillGrace    = 2 * time.Second
)
