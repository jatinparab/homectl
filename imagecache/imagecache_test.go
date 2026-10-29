package imagecache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	calls      []call
	run        func(name string, args []string) ([]byte, error)
	runContext func(context.Context, string, []string) ([]byte, error)
}

func (r *fakeRunner) Run(ctx context.Context, _ func(string), name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, call{name: name, args: append([]string(nil), args...)})
	if r.runContext != nil {
		return r.runContext(ctx, name, args)
	}
	if r.run != nil {
		return r.run(name, args)
	}
	return nil, nil
}

func testLoader(t *testing.T, runner Runner) *Loader {
	t.Helper()
	loader := NewWithRunner(runner, t.TempDir())
	loader.authFile = filepath.Join(t.TempDir(), "missing")
	return loader
}

func TestEnsureCopiesMissingImageThroughStagedSkopeo(t *testing.T) {
	runner := &fakeRunner{run: func(name string, args []string) ([]byte, error) {
		if name == "docker" {
			return nil, errors.New("missing")
		}
		return nil, nil
	}}
	loader := testLoader(t, runner)
	image := "example/app:1"
	if err := loader.Ensure(context.Background(), image, nil); err != nil {
		t.Fatal(err)
	}
	stage := loader.stagingDir(image)
	raw := "oci:" + filepath.Join(stage, "source") + ":image"
	gzip := "oci:" + filepath.Join(stage, "gzip") + ":image"
	want := []call{
		{"docker", []string{"image", "inspect", image}},
		{"skopeo", []string{"copy", "--retry-times", "3", "--image-parallel-copies", "2", "--dest-oci-accept-uncompressed-layers", "docker://" + image, raw}},
		{"skopeo", []string{"copy", "--dest-compress-format", "gzip", raw, gzip}},
		{"skopeo", []string{"copy", "--format", "v2s2", gzip, "docker-daemon:" + image}},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("successful import retained staging: %v", err)
	}
}

func TestEnsureReusesLocalImageWithoutRegistry(t *testing.T) {
	runner := &fakeRunner{}
	logs := []string{}
	if err := testLoader(t, runner).Ensure(context.Background(), "example/app:latest", func(line string) { logs = append(logs, line) }); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || runner.calls[0].name != "docker" {
		t.Fatalf("expected local inspection only, got %#v", runner.calls)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "using cached image") {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestFailedStagesRetainCompletedBlobsForRetry(t *testing.T) {
	for _, failedStage := range []int{1, 2, 3} {
		t.Run(string(rune('0'+failedStage)), func(t *testing.T) {
			runner := &fakeRunner{}
			loader := testLoader(t, runner)
			image := "example/app:1"
			stage := loader.stagingDir(image)
			blob := filepath.Join(stage, "source", "blobs", "sha256", "completed-layer")
			partial := filepath.Join(stage, "source", "oci-put-blob123")
			copies := 0
			runner.run = func(name string, args []string) ([]byte, error) {
				if name == "docker" {
					return nil, errors.New("missing")
				}
				copies++
				if copies == 1 {
					if err := os.MkdirAll(filepath.Dir(blob), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(blob, []byte("verified"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(partial, []byte("incomplete"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if copies == failedStage {
					return []byte("network or import failure"), errors.New("exit 1")
				}
				return nil, nil
			}
			if err := loader.Ensure(context.Background(), image, nil); err == nil {
				t.Fatal("expected failure")
			}
			if _, err := os.Stat(blob); err != nil {
				t.Fatalf("completed layer lost: %v", err)
			}
			runner.calls = nil
			runner.run = func(name string, args []string) ([]byte, error) {
				if name == "docker" {
					return nil, errors.New("missing")
				}
				if _, err := os.Stat(blob); err != nil {
					t.Fatalf("retry lost completed layer: %v", err)
				}
				if _, err := os.Stat(partial); !os.IsNotExist(err) {
					t.Fatalf("retry retained incomplete blob: %v", err)
				}
				return nil, nil
			}
			if err := loader.Ensure(context.Background(), image, nil); err != nil {
				t.Fatal(err)
			}
			if len(runner.calls) != 4 {
				t.Fatalf("calls = %#v", runner.calls)
			}
			if _, err := os.Stat(stage); !os.IsNotExist(err) {
				t.Fatalf("staging not cleaned: %v", err)
			}
		})
	}
}

func TestRefreshFallsBackAfterPullDeadline(t *testing.T) {
	runner := &fakeRunner{runContext: func(ctx context.Context, name string, args []string) ([]byte, error) {
		if name == "skopeo" {
			<-ctx.Done()
			return []byte("Copying blob"), ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("cache inspection inherited expired context: %v", err)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("cache inspection must be bounded")
		}
		return nil, nil
	}}
	loader := testLoader(t, runner)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := loader.Refresh(ctx, "example/app:1", nil); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestRefreshPreservesDeadlineInError(t *testing.T) {
	runner := &fakeRunner{runContext: func(ctx context.Context, name string, args []string) ([]byte, error) {
		if name == "skopeo" {
			<-ctx.Done()
		}
		return []byte("Copying blob"), errors.New("killed")
	}}
	loader := testLoader(t, runner)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := loader.Refresh(ctx, "example/app:1", nil)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "Copying blob") {
		t.Fatalf("expected deadline and tooling output, got %v", err)
	}
}

func TestCancelledRequestsDoNotStartTooling(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		runner := &fakeRunner{}
		loader := testLoader(t, runner)
		load := loader.Ensure
		if refresh {
			load = loader.Refresh
		}
		if err := load(ctx, "example/app:1", nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("unexpected commands: %#v", runner.calls)
		}
	}
}

func TestImageGateWaitIsCancellableAcrossLoaders(t *testing.T) {
	runner := &fakeRunner{}
	loader := testLoader(t, runner)
	unlock, err := lockImage(context.Background(), loader.stagingDir("example/app:1"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	second := NewWithRunner(runner, loader.cacheDir)
	if err := second.Ensure(ctx, "example/app:1", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("commands ran while image locked: %#v", runner.calls)
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
	if err := testLoader(t, runner).Refresh(context.Background(), "example/app:1", func(line string) { logs = append(logs, line) }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "using cached image") {
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
	if err := testLoader(t, runner).Refresh(context.Background(), "example/app:1", nil); err == nil {
		t.Fatal("expected refresh failure")
	}
}
