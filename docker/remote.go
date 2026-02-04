package docker

import (
	"strings"

	"github.com/jatinparab/homectl/manager/client"
)

// RemoteFileReader reads files from a remote manager server
type RemoteFileReader struct {
	client *client.Client
}

// NewRemoteFileReader creates a new RemoteFileReader with the given manager URL
func NewRemoteFileReader(managerURL string) *RemoteFileReader {
	return &RemoteFileReader{
		client: client.NewClient(managerURL),
	}
}

func (r *RemoteFileReader) ReadFile(path string) ([]byte, error) {
	return r.client.ReadFile(path)
}

func (r *RemoteFileReader) ListDir(path string) ([]string, error) {
	entries, err := r.client.ListDir(path)
	if err != nil {
		return nil, err
	}

	// Extract just the file/dir names from the full paths
	var names []string
	for _, entry := range entries {
		// The manager returns paths like "docker/v2/stash/"
		// We need to extract just "stash/" for the dir name
		parts := strings.Split(strings.TrimSuffix(entry, "/"), "/")
		name := parts[len(parts)-1]
		if strings.HasSuffix(entry, "/") {
			name += "/"
		}
		names = append(names, name)
	}
	return names, nil
}

// NewDockerWithRemote creates a Docker instance that reads from a remote manager
func NewDockerWithRemote(rootDir string, managerURL string) *Docker {
	return NewDockerWithFileReader(rootDir, NewRemoteFileReader(managerURL))
}
