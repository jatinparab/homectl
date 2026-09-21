package imagecache

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const defaultDockerAuthFile = "/root/.docker/config.json"

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
}

func New() *Loader {
	return &Loader{runner: execRunner{}, authFile: defaultDockerAuthFile}
}

func NewWithRunner(runner Runner) *Loader {
	return &Loader{runner: runner, authFile: defaultDockerAuthFile}
}

// Ensure leaves an existing local image untouched and downloads it only when
// absent. This is used for infrastructure services where availability is more
// important than refreshing a mutable tag during every reconcile.
func (l *Loader) Ensure(ctx context.Context, image string, onLog func(string)) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("image reference is required")
	}
	if _, err := l.runner.Run(ctx, nil, "docker", "image", "inspect", image); err == nil {
		return nil
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
	if err := l.copy(ctx, image, onLog); err == nil {
		return nil
	} else if _, inspectErr := l.runner.Run(ctx, nil, "docker", "image", "inspect", image); inspectErr == nil {
		if onLog != nil {
			onLog("registry refresh failed; using cached image " + image)
		}
		return nil
	} else {
		return err
	}
}

func (l *Loader) copy(ctx context.Context, image string, onLog func(string)) error {
	args := []string{"copy", "--retry-times", "3"}
	if _, err := os.Stat(l.authFile); err == nil {
		args = append(args, "--authfile", l.authFile)
	}
	args = append(args, "docker://"+image, "docker-daemon:"+image)
	output, err := l.runner.Run(ctx, onLog, "skopeo", args...)
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("preload image %s: %s", image, message)
	}
	return nil
}
