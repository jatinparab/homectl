package imagecache

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	calls []call
	run   func(name string, args []string) ([]byte, error)
}

func (r *fakeRunner) Run(_ context.Context, _ func(string), name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, call{name: name, args: append([]string(nil), args...)})
	if r.run != nil {
		return r.run(name, args)
	}
	return nil, nil
}

func TestEnsureCopiesMissingImageThroughSkopeo(t *testing.T) {
	runner := &fakeRunner{run: func(name string, args []string) ([]byte, error) {
		if name == "docker" {
			return nil, errors.New("missing")
		}
		return nil, nil
	}}
	loader := NewWithRunner(runner)
	loader.authFile = t.TempDir() + "/missing"
	if err := loader.Ensure(context.Background(), "example/app:1", nil); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %#v", runner.calls)
	}
	copyCall := runner.calls[1]
	if copyCall.name != "skopeo" || strings.Join(copyCall.args, " ") != "copy --retry-times 3 docker://example/app:1 docker-daemon:example/app:1" {
		t.Fatalf("unexpected copy call: %#v", copyCall)
	}
}

func TestRefreshFallsBackToCachedImage(t *testing.T) {
	logs := []string{}
	runner := &fakeRunner{run: func(name string, args []string) ([]byte, error) {
		if name == "skopeo" {
			return []byte("registry unavailable"), errors.New("exit 1")
		}
		return []byte("present"), nil
	}}
	loader := NewWithRunner(runner)
	loader.authFile = t.TempDir() + "/missing"
	if err := loader.Refresh(context.Background(), "example/app:1", func(line string) { logs = append(logs, line) }); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "using cached image") {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestRefreshFailsWhenRegistryAndCacheAreUnavailable(t *testing.T) {
	runner := &fakeRunner{run: func(name string, args []string) ([]byte, error) {
		if name == "skopeo" {
			return []byte("registry unavailable"), errors.New("exit 1")
		}
		return nil, errors.New("missing")
	}}
	loader := NewWithRunner(runner)
	loader.authFile = t.TempDir() + "/missing"
	if err := loader.Refresh(context.Background(), "example/app:1", nil); err == nil {
		t.Fatal("expected refresh failure")
	}
}
