package manager

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func TestSaveLoadRestoreSnapshotOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	// Register an app and set an override
	appID := common.AppID("test-app")
	m.appRegistry.Register(appID, common.DesiredRunning, false)
	override := common.RestoreSnapshotOverride{
		SnapshotID: "snap-abc123",
		ShortID:    "abc12345",
		Time:       time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC),
		Node:       "node1",
	}
	if err := m.appRegistry.SetRestoreSnapshotOverride(appID, override); err != nil {
		t.Fatalf("SetRestoreSnapshotOverride: %v", err)
	}

	// Save overrides
	m.saveRestoreSnapshotOverrides()

	// Verify file exists
	path := m.restoreSnapshotOverridesPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("restore-snapshot-overrides.json should exist after save")
	}

	// Load overrides
	loaded := m.loadRestoreSnapshotOverrides()
	if len(loaded) != 1 {
		t.Fatalf("expected 1 override, got %d", len(loaded))
	}

	loadedOverride, ok := loaded[string(appID)]
	if !ok {
		t.Fatal("expected override for test-app")
	}
	if loadedOverride.SnapshotID != "snap-abc123" {
		t.Errorf("expected SnapshotID 'snap-abc123', got '%s'", loadedOverride.SnapshotID)
	}
	if loadedOverride.ShortID != "abc12345" {
		t.Errorf("expected ShortID 'abc12345', got '%s'", loadedOverride.ShortID)
	}
	if loadedOverride.Node != "node1" {
		t.Errorf("expected Node 'node1', got '%s'", loadedOverride.Node)
	}
}

func TestSaveRestoreSnapshotOverrides_OmitsAppsWithoutOverride(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	// Register two apps, set override on only one
	m.appRegistry.Register("app1", common.DesiredRunning, false)
	m.appRegistry.Register("app2", common.DesiredRunning, false)

	override := common.RestoreSnapshotOverride{
		SnapshotID: "snap-xyz",
	}
	_ = m.appRegistry.SetRestoreSnapshotOverride("app1", override)

	m.saveRestoreSnapshotOverrides()

	loaded := m.loadRestoreSnapshotOverrides()
	if len(loaded) != 1 {
		t.Fatalf("expected 1 override (app1 only), got %d", len(loaded))
	}
	if _, ok := loaded["app2"]; ok {
		t.Error("app2 should not have an override")
	}
}

func TestLoadRestoreSnapshotOverrides_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	// File doesn't exist - should return empty map, not panic
	loaded := m.loadRestoreSnapshotOverrides()
	if len(loaded) != 0 {
		t.Errorf("expected empty map for missing file, got %d entries", len(loaded))
	}
}

func TestLoadRestoreSnapshotOverrides_CorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	// Write corrupt JSON
	path := m.restoreSnapshotOverridesPath()
	if err := os.WriteFile(path, []byte("not valid json{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should return empty map, not panic
	loaded := m.loadRestoreSnapshotOverrides()
	if len(loaded) != 0 {
		t.Errorf("expected empty map for corrupt file, got %d entries", len(loaded))
	}
}

func TestRestoreSnapshotOverridesPath(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	expected := filepath.Join(tmpDir, ".homectl", "restore-snapshot-overrides.json")
	got := m.restoreSnapshotOverridesPath()
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestHandleAPIAppRestoreSnapshotOverride_PUT_RejectsEmptySnapshotID(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	m.appRegistry.Register("test-app", common.DesiredRunning, false)

	body := `{"snapshot_id": ""}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/apps/test-app/restore-snapshot-override", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	m.handleAPIAppRestoreSnapshotOverride(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAPIAppRestoreSnapshotOverride_PUT_RejectsWhitespaceInSnapshotID(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	m.appRegistry.Register("test-app", common.DesiredRunning, false)

	tests := []struct {
		name string
		id   string
	}{
		{"leading space", " snap-123"},
		{"trailing space", "snap-123 "},
		{"internal space", "snap 123"},
		{"tab", "snap\t123"},
		{"unicode whitespace", "snap\u00A0123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"snapshot_id": "` + tt.id + `"}`
			req := httptest.NewRequest(http.MethodPut, "/api/v1/apps/test-app/restore-snapshot-override", bytes.NewReader([]byte(body)))
			w := httptest.NewRecorder()
			m.handleAPIAppRestoreSnapshotOverride(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d: %s", tt.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleAPIAppRestoreSnapshotOverride_PUT_Succeeds(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	m.appRegistry.Register("test-app", common.DesiredRunning, false)

	body := `{"snapshot_id": "snap-abc123", "short_id": "abc12345", "time": "2026-06-21T10:00:00Z"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/apps/test-app/restore-snapshot-override", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	m.handleAPIAppRestoreSnapshotOverride(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", resp["status"])
	}
}

func TestHandleAPIAppRestoreSnapshotOverride_PUT_NodeUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	m.appRegistry.Register("test-app", common.DesiredRunning, false)

	body := `{"snapshot_id": "snap-abc123", "node": "definitely-unknown-node"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/apps/test-app/restore-snapshot-override", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	m.handleAPIAppRestoreSnapshotOverride(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for unavailable node, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAPIAppRestoreSnapshotOverride_PUT_UnknownApp(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	body := `{"snapshot_id": "snap-abc123"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/apps/unknown-app/restore-snapshot-override", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	m.handleAPIAppRestoreSnapshotOverride(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown app, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAPIAppRestoreSnapshotOverride_DELETE_Succeeds(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	m.appRegistry.Register("test-app", common.DesiredRunning, false)

	// Set override first
	override := common.RestoreSnapshotOverride{SnapshotID: "snap-xyz"}
	_ = m.appRegistry.SetRestoreSnapshotOverride("test-app", override)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/apps/test-app/restore-snapshot-override", nil)
	w := httptest.NewRecorder()
	m.handleAPIAppRestoreSnapshotOverride(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", resp["status"])
	}

	// Verify override is cleared
	app := m.appRegistry.Get("test-app")
	if app.RestoreSnapshotOverride != nil {
		t.Error("expected override to be cleared after DELETE")
	}
}

func TestHandleAPIAppRestoreSnapshotOverride_DELETE_UnknownApp(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/apps/unknown-app/restore-snapshot-override", nil)
	w := httptest.NewRecorder()
	m.handleAPIAppRestoreSnapshotOverride(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown app, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAPIAppRestoreSnapshotOverride_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	m.appRegistry.Register("test-app", common.DesiredRunning, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/test-app/restore-snapshot-override", nil)
	w := httptest.NewRecorder()
	m.handleAPIAppRestoreSnapshotOverride(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestDesiredAppsResponse_IncludeRestoreSnapshotID(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	// Register an app with an override
	appID := common.AppID("test-app")
	m.appRegistry.Register(appID, common.DesiredRunning, false)
	override := common.RestoreSnapshotOverride{
		SnapshotID: "snap-override-123",
		ShortID:    "override12",
		Time:       time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC),
		Node:       "node1",
	}
	_ = m.appRegistry.SetRestoreSnapshotOverride(appID, override)

	// Simulate what handleDesiredApps does
	agentApps := m.appRegistry.AppsForAgent("node1")
	for _, app := range agentApps {
		if app.ID == appID {
			da := common.DesiredApp{
				AppID:             app.ID,
				DesiredState:      app.DesiredState,
				BackupEnabled:     false,
				RestoreEnabled:    app.RestoreEnabled,
				RestoreSnapshotID: "",
			}
			if app.RestoreSnapshotOverride != nil {
				da.RestoreSnapshotID = app.RestoreSnapshotOverride.SnapshotID
			}
			if da.RestoreSnapshotID != "snap-override-123" {
				t.Errorf("expected RestoreSnapshotID 'snap-override-123', got '%s'", da.RestoreSnapshotID)
			}
		}
	}
}

func TestDesiredAppsResponse_NoOverride_EmptySnapshotID(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	appID := common.AppID("test-app")
	m.appRegistry.Register(appID, common.DesiredRunning, false)

	agentApps := m.appRegistry.AppsForAgent("node1")
	for _, app := range agentApps {
		if app.ID == appID {
			da := common.DesiredApp{
				AppID:             app.ID,
				DesiredState:      app.DesiredState,
				BackupEnabled:     false,
				RestoreEnabled:    app.RestoreEnabled,
				RestoreSnapshotID: "",
			}
			if app.RestoreSnapshotOverride != nil {
				da.RestoreSnapshotID = app.RestoreSnapshotOverride.SnapshotID
			}
			if da.RestoreSnapshotID != "" {
				t.Errorf("expected empty RestoreSnapshotID, got '%s'", da.RestoreSnapshotID)
			}
		}
	}
}
