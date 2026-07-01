package transcode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const hlsJobTimeout = 6 * time.Hour

var incompatibleAudio = map[string]bool{
	"ac3":    true,
	"eac3":   true,
	"dts":    true,
	"truehd": true,
	"dtshd":  true,
}

type hlsJob struct {
	outDir  string
	srcPath string
	done    chan struct{}

	mu  sync.Mutex
	err error
}

type hlsRunner func(context.Context, *hlsJob) error

func (j *hlsJob) waitReady(ctx context.Context, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-j.done:
		j.mu.Lock()
		err := j.err
		j.mu.Unlock()
		if err != nil {
			return err
		}
		if _, statErr := os.Stat(filepath.Join(j.outDir, "index.m3u8")); statErr != nil {
			return fmt.Errorf("HLS output missing")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("timeout waiting for HLS output")
	}
}

func runHLSJob(ctx context.Context, j *hlsJob) error {
	if _, err := os.Stat(filepath.Join(j.outDir, "index.m3u8")); err == nil {
		return nil
	}

	codec, err := probeAudioCodec(ctx, j.srcPath)
	if err != nil {
		return err
	}

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", j.srcPath,
		"-map", "0:v:0?", "-map", "0:a:0?",
	}
	if incompatibleAudio[strings.ToLower(codec)] {
		args = append(args, "-c:v", "copy", "-c:a", "aac", "-b:a", "192k")
	} else {
		args = append(args, "-c", "copy")
	}
	segmentPattern := filepath.Join(j.outDir, "seg%03d.ts")
	args = append(args,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", segmentPattern,
		filepath.Join(j.outDir, "index.m3u8"),
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("ffmpeg: %w", ctxErr)
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (j *hlsJob) setErr(err error) {
	j.mu.Lock()
	j.err = err
	j.mu.Unlock()
}

func probeAudioCodec(ctx context.Context, srcPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name",
		"-of", "csv=p=0",
		srcPath,
	)
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("ffprobe: %w", ctxErr)
		}
		if _, statErr := os.Stat(srcPath); statErr != nil {
			return "", fmt.Errorf("source file missing")
		}
		return "", fmt.Errorf("ffprobe: %w", err)
	}
	codec := strings.TrimSpace(string(out))
	if codec == "" {
		return "", fmt.Errorf("no audio stream found")
	}
	return codec, nil
}
