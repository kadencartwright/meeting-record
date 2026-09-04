package audio

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	DefaultSinkAlias   = "@DEFAULT_AUDIO_SINK@"
	DefaultSourceAlias = "@DEFAULT_AUDIO_SOURCE@"
)

type Device struct {
	Description string `json:"description"`
	NodeName    string `json:"nodeName"`
}

type Devices struct {
	Output     Device `json:"output"`
	Microphone Device `json:"microphone"`
}

type Runner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), message)
	}
	return stdout.Bytes(), nil
}

func Discover(ctx context.Context, runner Runner) (Devices, error) {
	sink, err := discoverOne(ctx, runner, DefaultSinkAlias, "Audio/Sink")
	if err != nil {
		return Devices{}, fmt.Errorf("resolve default output sink: %w", err)
	}
	source, err := discoverOne(ctx, runner, DefaultSourceAlias, "Audio/Source")
	if err != nil {
		return Devices{}, fmt.Errorf("resolve default microphone: %w", err)
	}
	return Devices{Output: sink, Microphone: source}, nil
}

func discoverOne(ctx context.Context, runner Runner, alias, expectedClass string) (Device, error) {
	out, err := runner.Output(ctx, "wpctl", "inspect", alias)
	if err != nil {
		return Device{}, err
	}
	properties, err := ParseInspect(out)
	if err != nil {
		return Device{}, err
	}
	if got := properties["media.class"]; got != expectedClass {
		return Device{}, fmt.Errorf("%s resolved to %q, expected %q", alias, got, expectedClass)
	}
	name := properties["node.name"]
	if name == "" {
		return Device{}, fmt.Errorf("%s has no node.name", alias)
	}
	description := properties["node.description"]
	if description == "" {
		description = properties["node.nick"]
	}
	if description == "" {
		description = name
	}
	return Device{Description: description, NodeName: name}, nil
}

func ParseInspect(output []byte) (map[string]string, error) {
	properties := make(map[string]string)
	for _, raw := range strings.Split(string(output), "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		key, value, ok := strings.Cut(line, " = ")
		if !ok || key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, `"`) {
			decoded, err := strconv.Unquote(value)
			if err != nil {
				return nil, fmt.Errorf("parse wpctl property %s: %w", key, err)
			}
			value = decoded
		}
		properties[strings.TrimSpace(key)] = value
	}
	if len(properties) == 0 {
		return nil, fmt.Errorf("wpctl inspect returned no properties")
	}
	return properties, nil
}

func CheckDependencies() error {
	for _, binary := range []string{"wpctl", "pw-record"} {
		if _, err := exec.LookPath(binary); err != nil {
			return fmt.Errorf("required program %q was not found in PATH", binary)
		}
	}
	return nil
}
