package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kadencartwright/meeting-record/internal/meeting"
)

type Runner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("%s: %s", name, message)
	}
	return stdout.Bytes(), nil
}

type File struct {
	ID              string                `json:"id"`
	RecorderID      string                `json:"recorderId"`
	Name            string                `json:"name"`
	RelativePath    string                `json:"relativePath"`
	RecordedAt      time.Time             `json:"recordedAt"`
	DurationSeconds float64               `json:"durationSeconds"`
	SizeBytes       int64                 `json:"sizeBytes"`
	Codec           string                `json:"codec"`
	SampleRate      int                   `json:"sampleRate"`
	Channels        int                   `json:"channels"`
	Fingerprint     string                `json:"-"`
	Path            string                `json:"-"`
	Notion          *meeting.NotionExport `json:"notion,omitempty"`
}

type Recorder struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Connected  bool   `json:"connected"`
	Mounted    bool   `json:"mounted"`
	MountPoint string `json:"mountPoint,omitempty"`
	Error      string `json:"error,omitempty"`
	Files      []File `json:"files"`
}

type Inventory struct {
	Recorders []Recorder `json:"recorders"`
}

type blockDevice struct {
	Path        string        `json:"path"`
	UUID        string        `json:"uuid"`
	MountPoints []string      `json:"mountpoints"`
	Children    []blockDevice `json:"children"`
}

type blockDevices struct {
	BlockDevices []blockDevice `json:"blockdevices"`
}

func Discover(ctx context.Context, runner Runner, configured []meeting.ExternalRecorder, mount bool) (Inventory, error) {
	devices, err := listBlockDevices(ctx, runner)
	if err != nil {
		return Inventory{}, err
	}
	result := Inventory{Recorders: make([]Recorder, 0, len(configured))}
	for _, config := range configured {
		recorder := Recorder{ID: config.ID, Label: config.Label, Files: []File{}}
		device, found := findConfiguredDevice(devices, config.FilesystemUUID)
		if !found {
			result.Recorders = append(result.Recorders, recorder)
			continue
		}
		recorder.Connected = true
		mountPoint := firstMountPoint(device.MountPoints)
		if mountPoint == "" && mount {
			if _, mountErr := runner.Output(ctx, "udisksctl", "mount", "--block-device", device.Path, "--options", "ro,nosuid,nodev,noexec"); mountErr != nil {
				recorder.Error = mountErr.Error()
			} else if refreshed, refreshErr := listBlockDevices(ctx, runner); refreshErr == nil {
				if mountedDevice, ok := findConfiguredDevice(refreshed, config.FilesystemUUID); ok {
					mountPoint = firstMountPoint(mountedDevice.MountPoints)
				}
			}
		}
		if mountPoint == "" {
			result.Recorders = append(result.Recorders, recorder)
			continue
		}
		recorder.Mounted = true
		recorder.MountPoint = mountPoint
		root := filepath.Join(mountPoint, filepath.FromSlash(config.RecordingsPath))
		files, scanErr := scanFiles(ctx, runner, config.ID, root)
		if scanErr != nil {
			recorder.Error = scanErr.Error()
		} else {
			recorder.Files = files
		}
		result.Recorders = append(result.Recorders, recorder)
	}
	return result, nil
}

func listBlockDevices(ctx context.Context, runner Runner) ([]blockDevice, error) {
	output, err := runner.Output(ctx, "lsblk", "--json", "--output", "PATH,UUID,MOUNTPOINTS")
	if err != nil {
		return nil, fmt.Errorf("enumerate removable storage: %w", err)
	}
	var decoded blockDevices
	if err := json.Unmarshal(output, &decoded); err != nil {
		return nil, fmt.Errorf("parse removable storage inventory: %w", err)
	}
	return decoded.BlockDevices, nil
}

func findByUUID(devices []blockDevice, uuid string) (blockDevice, bool) {
	for _, device := range devices {
		if strings.EqualFold(strings.TrimSpace(device.UUID), strings.TrimSpace(uuid)) {
			return device, true
		}
		if child, found := findByUUID(device.Children, uuid); found {
			return child, true
		}
	}
	return blockDevice{}, false
}

func findConfiguredDevice(devices []blockDevice, uuid string) (blockDevice, bool) {
	if device, found := findByUUID(devices, uuid); found {
		return device, true
	}
	target, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-uuid", uuid))
	if err != nil {
		return blockDevice{}, false
	}
	return findByPath(devices, target)
}

func findByPath(devices []blockDevice, path string) (blockDevice, bool) {
	for _, device := range devices {
		if filepath.Clean(device.Path) == filepath.Clean(path) {
			return device, true
		}
		if child, found := findByPath(device.Children, path); found {
			return child, true
		}
	}
	return blockDevice{}, false
}

func firstMountPoint(points []string) string {
	for _, point := range points {
		if strings.TrimSpace(point) != "" {
			return point
		}
	}
	return ""
}

var audioExtensions = map[string]bool{
	".aac": true, ".flac": true, ".m4a": true, ".mp3": true,
	".ogg": true, ".opus": true, ".wav": true,
}

func scanFiles(ctx context.Context, runner Runner, recorderID, root string) ([]File, error) {
	entries := []File{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !audioExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		file := File{
			ID: recorderID + "/" + relative, RecorderID: recorderID,
			Name: entry.Name(), RelativePath: relative, RecordedAt: info.ModTime(),
			SizeBytes: info.Size(), Path: path,
			Fingerprint: fmt.Sprintf("%s|%s|%d|%d", recorderID, relative, info.Size(), info.ModTime().Unix()),
		}
		probeFile(ctx, runner, &file)
		entries = append(entries, file)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan recorder files: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].RecordedAt.After(entries[right].RecordedAt)
	})
	return entries, nil
}

type probeResult struct {
	Streams []struct {
		CodecName  string `json:"codec_name"`
		SampleRate string `json:"sample_rate"`
		Channels   int    `json:"channels"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func probeFile(ctx context.Context, runner Runner, file *File) {
	output, err := runner.Output(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration:stream=codec_name,sample_rate,channels", "-of", "json", file.Path)
	if err != nil {
		return
	}
	var probe probeResult
	if json.Unmarshal(output, &probe) != nil {
		return
	}
	file.DurationSeconds, _ = strconv.ParseFloat(probe.Format.Duration, 64)
	if len(probe.Streams) > 0 {
		file.Codec = probe.Streams[0].CodecName
		file.SampleRate, _ = strconv.Atoi(probe.Streams[0].SampleRate)
		file.Channels = probe.Streams[0].Channels
	}
}

func Find(inventory Inventory, id string) (File, bool) {
	for _, recorder := range inventory.Recorders {
		for _, file := range recorder.Files {
			if file.ID == id {
				return file, true
			}
		}
	}
	return File{}, false
}
