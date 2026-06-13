package buildinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateVersion(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"0.1.0", true},
		{"1.0.0", true},
		{"99.999.999", true},
		{"0.0.0", true},
		{"v0.1.0", false},
		{"0.1", false},
		{"0.1.0-beta", false},
		{"0.1.0+build", false},
		{"", false},
		{"dev", false},
		{"v1.2.3", false},
	}
	for _, tt := range tests {
		err := ValidateVersion(tt.in)
		got := err == nil
		if got != tt.want {
			t.Errorf("ValidateVersion(%q) = %v, want %v (err=%v)", tt.in, got, tt.want, err)
		}
	}
}

func TestReadVersionFromFile(t *testing.T) {
	dir := t.TempDir()

	// Valid
	path := filepath.Join(dir, "VERSION")
	if err := os.WriteFile(path, []byte("0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	v, err := ReadVersionFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if v != "0.1.0" {
		t.Errorf("expected 0.1.0, got %q", v)
	}

	// Invalid
	if err := os.WriteFile(path, []byte("v0.1.0"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = ReadVersionFromFile(path)
	if err == nil {
		t.Errorf("expected error for invalid version")
	}

	// Missing
	_, err = ReadVersionFromFile("/nonexistent/VERSION")
	if err == nil {
		t.Errorf("expected error for missing file")
	}
}

func TestForComponent(t *testing.T) {
	orig := [3]string{Version, Commit, BuildDate}
	Version, Commit, BuildDate = "1.2.3", "abc1234", "2024-01-01"
	defer func() {
		Version, Commit, BuildDate = orig[0], orig[1], orig[2]
	}()

	info := ForComponent("manager")
	if info.Component != "manager" {
		t.Errorf("expected component manager, got %q", info.Component)
	}
	if info.Version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %q", info.Version)
	}
	if info.Commit != "abc1234" {
		t.Errorf("expected commit abc1234, got %q", info.Commit)
	}
}
