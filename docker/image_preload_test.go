package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jatinparab/homectl/imagecache"
)

type preloadRunner struct {
	missing bool
	calls   []string
}

func (r *preloadRunner) Run(_ context.Context, _ func(string), name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if name == "docker" && r.missing {
		return nil, errors.New("image missing")
	}
	return nil, nil
}

func TestComposeImagePreloading(t *testing.T) {
	for _, tc := range []struct {
		name    string
		missing bool
		refresh bool
		copies  int
	}{
		{"start cached latest", false, false, 0},
		{"start missing image", true, false, 3},
		{"explicit refresh", false, true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			appDir := filepath.Join(dir, "app")
			if err := os.MkdirAll(appDir, 0755); err != nil {
				t.Fatal(err)
			}
			// Compose is stubbed, but its inputs still exercise real start code.
			compose := "services:\n  web:\n    image: example/app:latest\n    pull_policy: always\n"
			if err := os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte(compose), 0644); err != nil {
				t.Fatal(err)
			}
			commands := filepath.Join(dir, "commands")
			t.Setenv("COMMAND_LOG", commands)
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$COMMAND_LOG"
case "$*" in
  *'config --format json'*) printf '%s\n' '{"services":{"web":{"image":"example/app:latest"},"worker":{"image":"example/app:latest"}}}' ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			d := NewDocker(dir, dir)
			runner := &preloadRunner{missing: tc.missing}
			d.imageLoader = imagecache.NewWithRunner(runner, t.TempDir())
			var err error
			if tc.refresh {
				err = d.Pull("app", nil)
			} else {
				err = d.StartWithLogs("app", nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			copies := 0
			for _, call := range runner.calls {
				if strings.HasPrefix(call, "skopeo ") {
					copies++
				}
			}
			if copies != tc.copies {
				t.Fatalf("registry copies = %d, want %d; calls %v", copies, tc.copies, runner.calls)
			}
			if !tc.refresh {
				output, err := os.ReadFile(commands)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(output), "up -d --build --pull never") {
					t.Fatalf("daemon pulls not disabled: %s", output)
				}
			}
		})
	}
}
