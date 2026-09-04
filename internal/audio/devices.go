package audio

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
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

type DeviceInventory struct {
	Devices
	Outputs     []Device `json:"outputs"`
	Microphones []Device `json:"microphones"`
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

func DiscoverAvailable(ctx context.Context, runner Runner) (DeviceInventory, error) {
	defaults, err := Discover(ctx, runner)
	if err != nil {
		return DeviceInventory{}, err
	}
	status, err := runner.Output(ctx, "wpctl", "status", "-n")
	if err != nil {
		return DeviceInventory{}, fmt.Errorf("list PipeWire audio nodes: %w", err)
	}
	inventory := DeviceInventory{Devices: defaults, Outputs: []Device{}, Microphones: []Device{}}
	for _, id := range ParseAudioNodeIDs(status) {
		output, inspectErr := runner.Output(ctx, "wpctl", "inspect", strconv.Itoa(id))
		if inspectErr != nil {
			continue
		}
		properties, parseErr := ParseInspect(output)
		if parseErr != nil {
			continue
		}
		device, deviceErr := deviceFromProperties(properties)
		if deviceErr != nil {
			continue
		}
		switch properties["media.class"] {
		case "Audio/Sink":
			inventory.Outputs = appendUnique(inventory.Outputs, device)
		case "Audio/Source":
			inventory.Microphones = appendUnique(inventory.Microphones, device)
		}
	}
	inventory.Outputs = appendUnique(inventory.Outputs, defaults.Output)
	inventory.Microphones = appendUnique(inventory.Microphones, defaults.Microphone)
	sortDevices(inventory.Outputs)
	sortDevices(inventory.Microphones)
	return inventory, nil
}

func ResolveSelection(ctx context.Context, runner Runner, microphoneNode, outputNode string) (Devices, error) {
	if strings.TrimSpace(microphoneNode) == "" && strings.TrimSpace(outputNode) == "" {
		return Discover(ctx, runner)
	}
	inventory, err := DiscoverAvailable(ctx, runner)
	if err != nil {
		return Devices{}, err
	}
	microphone := inventory.Microphone
	if node := strings.TrimSpace(microphoneNode); node != "" {
		var ok bool
		microphone, ok = findDevice(inventory.Microphones, node)
		if !ok {
			return Devices{}, fmt.Errorf("selected microphone %q is no longer available", node)
		}
	}
	output := inventory.Output
	if node := strings.TrimSpace(outputNode); node != "" {
		var ok bool
		output, ok = findDevice(inventory.Outputs, node)
		if !ok {
			return Devices{}, fmt.Errorf("selected output sink %q is no longer available", node)
		}
	}
	return Devices{Microphone: microphone, Output: output}, nil
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
	return deviceFromProperties(properties)
}

func deviceFromProperties(properties map[string]string) (Device, error) {
	name := properties["node.name"]
	if name == "" {
		return Device{}, fmt.Errorf("audio node has no node.name")
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

func ParseAudioNodeIDs(output []byte) []int {
	ids := []int{}
	seen := make(map[int]bool)
	inAudio := false
	for _, raw := range strings.Split(string(output), "\n") {
		section := strings.TrimSpace(raw)
		if section == "Audio" {
			inAudio = true
			continue
		}
		if inAudio && section == "Video" {
			break
		}
		if !inAudio {
			continue
		}
		line := strings.TrimLeft(raw, " \t│├└─*")
		first, _, ok := strings.Cut(line, ".")
		if !ok {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(first))
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func appendUnique(devices []Device, candidate Device) []Device {
	if _, found := findDevice(devices, candidate.NodeName); found {
		return devices
	}
	return append(devices, candidate)
}

func findDevice(devices []Device, nodeName string) (Device, bool) {
	for _, device := range devices {
		if device.NodeName == nodeName {
			return device, true
		}
	}
	return Device{}, false
}

func sortDevices(devices []Device) {
	sort.Slice(devices, func(left, right int) bool {
		if devices[left].Description == devices[right].Description {
			return devices[left].NodeName < devices[right].NodeName
		}
		return devices[left].Description < devices[right].Description
	})
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
	for _, binary := range []string{"wpctl", "pw-record", "ffmpeg"} {
		if _, err := exec.LookPath(binary); err != nil {
			return fmt.Errorf("required program %q was not found in PATH", binary)
		}
	}
	return nil
}
