package imagecache

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultDockerAuthFile = "/root/.docker/config.json"
	defaultCacheDir       = "/config/image-cache"
)

// All loaders in an agent share these gates, including infrastructure loaders.
// OCI index updates and cleanup for the same image must not overlap. Waiting
// for a gate remains cancellable; other image references can download in parallel.
var imageGates sync.Map

func lockImage(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gate, _ := imageGates.LoadOrStore(key, make(chan struct{}, 1))
	ch := gate.(chan struct{})
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Runner executes image tooling. It is injectable for tests.
type Runner interface {
	Run(ctx context.Context, onLog func(string), name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, onLog func(string), name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var mu sync.Mutex
	var output strings.Builder
	consume := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			mu.Lock()
			output.WriteString(line)
			output.WriteByte('\n')
			mu.Unlock()
			if onLog != nil {
				onLog(line)
			}
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); consume(stdout) }()
	go func() { defer wg.Done(); consume(stderr) }()
	wg.Wait()
	err = cmd.Wait()
	return []byte(output.String()), err
}

// Loader downloads registry images in the Homectl agent process and imports
// them into the host Docker daemon through its mounted socket. Registry DNS is
// therefore resolved using the agent container's resolver, not dockerd's.
type Loader struct {
	runner   Runner
	authFile string
	cacheDir string
}

func New() *Loader {
	return NewWithRunner(execRunner{}, defaultCacheDir)
}

// NewWithRunner configures injectable tooling and a persistent staging root.
func NewWithRunner(runner Runner, cacheDir string) *Loader {
	return &Loader{runner: runner, authFile: defaultDockerAuthFile, cacheDir: cacheDir}
}

func (l *Loader) stagingDir(image string) string {
	return filepath.Join(l.cacheDir, fmt.Sprintf("%x", sha256.Sum256([]byte(image))))
}

// Ensure leaves an existing local image untouched and downloads it only when
// absent. Normal starts and backup recovery must not refresh mutable tags or
// depend on registry availability.
func (l *Loader) Ensure(ctx context.Context, image string, onLog func(string)) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("image reference is required")
	}
	unlock, err := lockImage(ctx, l.stagingDir(image))
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := l.runner.Run(ctx, nil, "docker", "image", "inspect", image); err == nil {
		if onLog != nil {
			onLog("using cached image " + image)
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.copy(ctx, image, onLog)
}

// Refresh downloads the current registry image. If the registry is unavailable
// but a local copy exists, Homectl keeps running with that cached image.
func (l *Loader) Refresh(ctx context.Context, image string, onLog func(string)) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("image reference is required")
	}
	unlock, err := lockImage(ctx, l.stagingDir(image))
	if err != nil {
		return err
	}
	defer unlock()
	err = l.copy(ctx, image, onLog)
	if err == nil {
		return nil
	}
	if ctx.Err() == context.Canceled {
		return ctx.Err()
	}
	// A timed-out download must not prevent checking the local cache. Keep
	// this separate from the expired pull deadline, but still bounded.
	inspectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, inspectErr := l.runner.Run(inspectCtx, nil, "docker", "image", "inspect", image); inspectErr == nil {
		if onLog != nil {
			onLog("registry refresh failed; using cached image " + image)
		}
		return nil
	}
	return err
}

func (l *Loader) copy(ctx context.Context, image string, onLog func(string)) error {
	// Keep the registry's original blob digests so completed layers can be
	// reused after interruption. Recompressing during download would change
	// their digests and defeat that reuse for zstd sources.
	stage := l.stagingDir(image)
	if err := os.MkdirAll(stage, 0700); err != nil {
		return fmt.Errorf("create image staging directory: %w", err)
	}
	// SIGKILL cannot run Skopeo's deferred cleanup. Discard incomplete temp
	// blobs from a previous attempt, but retain verified blobs by digest.
	for _, layout := range []string{"source", "gzip"} {
		paths, err := filepath.Glob(filepath.Join(stage, layout, "oci-put-blob*"))
		if err != nil {
			return err
		}
		for _, path := range paths {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove incomplete image blob: %w", err)
			}
		}
	}
	raw := "oci:" + filepath.Join(stage, "source") + ":image"
	compatible := "oci:" + filepath.Join(stage, "gzip") + ":image"
	args := []string{"copy", "--retry-times", "3", "--image-parallel-copies", "2", "--dest-oci-accept-uncompressed-layers"}
	if _, err := os.Stat(l.authFile); err == nil {
		args = append(args, "--authfile", l.authFile)
	}
	args = append(args, "docker://"+image, raw)
	if onLog != nil {
		onLog("downloading image layers (completed layers are reused on retry) " + image)
	}
	if err := l.runCopy(ctx, image, onLog, args...); err != nil {
		return err
	}
	// docker-daemon preserves input compression, even if --dest-compress-format
	// is supplied. Normalize through OCI (which actually supports compression
	// changes) before asking Skopeo to produce a Docker v2 schema-2 manifest.
	if onLog != nil {
		onLog("converting image layers to Docker-compatible gzip " + image)
	}
	if err := l.runCopy(ctx, image, onLog, "copy", "--dest-compress-format", "gzip", raw, compatible); err != nil {
		return err
	}
	if err := l.runCopy(ctx, image, onLog, "copy", "--format", "v2s2", compatible, "docker-daemon:"+image); err != nil {
		return err
	}
	// Do not keep a second full copy once Docker owns the image. Failed
	// downloads/imports deliberately retain staging for the next attempt.
	if err := os.RemoveAll(stage); err != nil && onLog != nil {
		onLog("image loaded, but staging cleanup failed: " + err.Error())
	}
	return nil
}

func (l *Loader) runCopy(ctx context.Context, image string, onLog func(string), args ...string) error {
	output, err := l.runner.Run(ctx, onLog, "skopeo", args...)
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return fmt.Errorf("preload image %s: %w: %s", image, err, message)
	}
	return nil
}
