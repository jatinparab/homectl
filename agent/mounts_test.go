package agent

import "testing"

func TestBindMountSourceFromInspect(t *testing.T) {
	data := []byte(`[
		{
			"Mounts": [
				{"Type": "bind", "Source": "/srv/homectl/app-data", "Destination": "/app-data"},
				{"Type": "bind", "Source": "/srv/homectl/config", "Destination": "/config"}
			]
		}
	]`)

	got, err := bindMountSourceFromInspect(data, "/app-data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/srv/homectl/app-data" {
		t.Fatalf("source = %q, want %q", got, "/srv/homectl/app-data")
	}
}

func TestBindMountSourceFromInspectRejectsVolume(t *testing.T) {
	data := []byte(`[
		{
			"Mounts": [
				{"Type": "volume", "Source": "/var/lib/docker/volumes/homectl/_data", "Destination": "/app-data"}
			]
		}
	]`)

	if _, err := bindMountSourceFromInspect(data, "/app-data"); err == nil {
		t.Fatal("expected volume mount to fail")
	}
}

func TestBindMountSourceFromInspectRequiresDestination(t *testing.T) {
	data := []byte(`[{"Mounts": []}]`)

	if _, err := bindMountSourceFromInspect(data, "/app-data"); err == nil {
		t.Fatal("expected missing mount to fail")
	}
}

func TestBindMountSourceFromInspectRejectsMalformedJSON(t *testing.T) {
	if _, err := bindMountSourceFromInspect([]byte(`not-json`), "/app-data"); err == nil {
		t.Fatal("expected malformed inspect output to fail")
	}
}
