package processing

import (
	"context"
	"fmt"
	"os"
)

// Transcode converts a recorder file to the same upload-friendly M4A format
// used by locally captured meetings.
func Transcode(ctx context.Context, runner Runner, source, destination string) error {
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-n",
		"-i", source,
		"-ar", "48000", "-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart", "-f", "mp4", destination,
	}
	if _, err := runner.Run(ctx, "ffmpeg", args, nil); err != nil {
		return fmt.Errorf("convert external recording: %w", err)
	}
	if err := os.Chmod(destination, 0o600); err != nil {
		return fmt.Errorf("secure converted recording: %w", err)
	}
	return nil
}
