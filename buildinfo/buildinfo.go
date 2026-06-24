// Package buildinfo provides Homectl build and runtime version metadata.
//
// Values are injected at build time via -ldflags. Local builds use sensible
// fallback defaults so no compile flags are required during development.
//
//	ldflags: -X github.com/jatinparab/homectl/buildinfo.Version=0.1.0
//	         -X github.com/jatinparab/homectl/buildinfo.Commit=abc1234
//	         -X github.com/jatinparab/homectl/buildinfo.BuildDate=2024-01-01T00:00:00Z
//	         -X github.com/jatinparab/homectl/buildinfo.Component=manager|agent
package buildinfo

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Build-time variables. Set via -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// canonicalVersionRE matches strict semver X.Y.Z without a leading v or
// prerelease/build suffix.
var canonicalVersionRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// ValidateVersion returns an error if v does not match the canonical semver
// format: X.Y.Z with no leading v, no prerelease suffix, no build metadata.
func ValidateVersion(v string) error {
	v = strings.TrimSpace(v)
	if !canonicalVersionRE.MatchString(v) {
		return fmt.Errorf("invalid version %q: must be X.Y.Z (no leading v, no prerelease/build suffix)", v)
	}
	return nil
}

// ReadVersionFromFile reads the canonical version from the given path.
// If path is empty it defaults to "VERSION".
func ReadVersionFromFile(path string) (string, error) {
	if path == "" {
		path = "VERSION"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read version file: %w", err)
	}
	v := strings.TrimSpace(string(b))
	if err := ValidateVersion(v); err != nil {
		return "", err
	}
	return v, nil
}

// Info groups runtime version metadata exposed over the API.
type Info struct {
	Component string `json:"component"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

// ForComponent returns an Info struct filled with this package's build-time
// values. component should be "manager" or "agent".
func ForComponent(component string) Info {
	return Info{
		Component: component,
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
	}
}
