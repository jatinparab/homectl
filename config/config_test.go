package config

import (
	"reflect"
	"testing"
)

func TestParseComposeExtraArgs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
		{
			name:  "compose files and profile",
			input: "-f docker-compose.gpu.yml --profile gpu",
			want:  []string{"-f", "docker-compose.gpu.yml", "--profile", "gpu"},
		},
		{
			name:  "quoted value",
			input: `--profile "gpu node" -f 'docker-compose.gpu.yml'`,
			want:  []string{"--profile", "gpu node", "-f", "docker-compose.gpu.yml"},
		},
		{
			name:  "escaped space",
			input: `-f docker-compose\ gpu.yml`,
			want:  []string{"-f", "docker-compose gpu.yml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseComposeExtraArgs(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseComposeExtraArgs(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseComposeExtraArgsErrors(t *testing.T) {
	for _, input := range []string{`--profile "gpu`, `-f docker-compose.gpu.yml\`} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseComposeExtraArgs(input); err == nil {
				t.Fatalf("expected error for %q", input)
			}
		})
	}
}

func TestFixedRuntimePaths(t *testing.T) {
	if got := GetGitRepoDir("host"); got != RepoDir {
		t.Fatalf("GetGitRepoDir() = %q, want %q", got, RepoDir)
	}

	cfg := NewConfig().GetAgentConfig("host")
	if cfg.Name != "host" {
		t.Fatalf("agent name = %q, want host", cfg.Name)
	}
}
