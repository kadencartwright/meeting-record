package processing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kadencartwright/meeting-record/internal/meeting"
)

const MergedFilename = "meeting.m4a"

// Merge combines the local microphone and remote sink tracks without removing
// either source track. The microphone is upmixed to stereo and limited after
// mixing to avoid clipping when both tracks are loud.
func Merge(ctx context.Context, runner Runner, directory string, metadata meeting.Metadata) (meeting.AudioFile, error) {
	local, err := trackPath(directory, metadata.Local.File)
	if err != nil {
		return meeting.AudioFile{}, err
	}
	remote, err := trackPath(directory, metadata.Remote.File)
	if err != nil {
		return meeting.AudioFile{}, err
	}
	for label, path := range map[string]string{"local": local, "remote": remote} {
		if _, err := os.Stat(path); err != nil {
			return meeting.AudioFile{}, fmt.Errorf("%s track unavailable: %w", label, err)
		}
	}

	temporary, err := os.CreateTemp(directory, ".meeting-*.m4a")
	if err != nil {
		return meeting.AudioFile{}, fmt.Errorf("create merged audio temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return meeting.AudioFile{}, err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return meeting.AudioFile{}, err
	}
	defer os.Remove(temporaryPath)

	filter := "[0:a]aformat=sample_rates=48000:channel_layouts=stereo[local];" +
		"[1:a]aformat=sample_rates=48000:channel_layouts=stereo[remote];" +
		"[local][remote]amix=inputs=2:duration=longest:dropout_transition=0:normalize=0," +
		"alimiter=limit=0.95[out]"
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-n",
		"-i", local, "-i", remote,
		"-filter_complex", filter, "-map", "[out]",
		"-ar", "48000", "-ac", "2", "-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart", "-f", "mp4",
		temporaryPath,
	}
	if _, err := runner.Run(ctx, "ffmpeg", args, nil); err != nil {
		return meeting.AudioFile{}, fmt.Errorf("merge recording tracks: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return meeting.AudioFile{}, fmt.Errorf("secure merged recording: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, MergedFilename)); err != nil {
		return meeting.AudioFile{}, fmt.Errorf("finalize merged recording: %w", err)
	}
	return meeting.AudioFile{File: MergedFilename, Channels: 2}, nil
}

func trackPath(directory, filename string) (string, error) {
	if filename == "" || filepath.Base(filename) != filename {
		return "", fmt.Errorf("invalid recording filename %q", filename)
	}
	return filepath.Join(directory, filename), nil
}
