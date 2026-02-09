package manager

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed ui/dist/*
var uiFiles embed.FS

// ServeUI returns an http.Handler that serves the embedded UI files
// with SPA fallback for client-side routing
func (m *Manager) ServeUI() http.Handler {
	// Get the dist subdirectory
	distFS, err := fs.Sub(uiFiles, "ui/dist")
	if err != nil {
		// Return a handler that reports the error
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI not available", http.StatusInternalServerError)
		})
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Clean the path
		if path == "/" {
			path = "/index.html"
		}

		// Try to open the file
		cleanPath := strings.TrimPrefix(path, "/")
		_, err := fs.Stat(distFS, cleanPath)

		if err != nil {
			// File doesn't exist, serve index.html for SPA routing
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	})
}
