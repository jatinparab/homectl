package imagecache

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in real-tool regression: serves a tiny OCI/zstd image from an isolated
// HTTPS registry, interrupts its download, then imports it into Docker. No
// application containers are started, stopped, or modified.
func TestSkopeoFirstImportIntegration(t *testing.T) {
	if os.Getenv("HOMECTL_IMAGE_IMPORT_INTEGRATION") != "1" {
		t.Skip("set HOMECTL_IMAGE_IMPORT_INTEGRATION=1 with skopeo and a Docker socket")
	}
	for _, tool := range []string{"skopeo", "docker"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal(err)
		}
	}
	// Deterministic zstd-compressed tar layers, each containing one tiny file.
	encoded := []string{
		"KLUv/WAAJy0CACQCb25lADAwMDA2NDQwMDAwMDAAMwAgMHVzdGFyADAwb25lCgwAKgV4wLQaipw5CgTPcAXemKGo+nAr/NQGwLFymZ4AEQ==",
		"KLUv/WAAJzUCADQCdHdvADAwMDA2NDQwMDAwMDAANzMAIDB1c3RhcgAwMHR3bwoMACoFeMC0GoqcOQoEz3AF3pjRqIxx1v4sBsCxcpmeABE=",
	}
	diffIDs := []string{
		"sha256:f3a9f9a0dc20e1706d5bbcf3ba582c883887494f4586d782b819a33cecf87a49",
		"sha256:f4f11a10d4ef50e9423879f37423bb737a5b717e2bb7b5de62b24644ffa792dd",
	}
	blobs := map[string][]byte{}
	var digests []string
	var layers []map[string]any
	for _, encoded := range encoded {
		blob, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		digest := fmt.Sprintf("sha256:%x", sha256.Sum256(blob))
		blobs[digest] = blob
		digests = append(digests, digest)
		layers = append(layers, map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+zstd", "digest": digest, "size": len(blob)})
	}
	config, err := json.Marshal(map[string]any{"architecture": runtime.GOARCH, "os": "linux", "config": map[string]any{}, "rootfs": map[string]any{"type": "layers", "diff_ids": diffIDs}})
	if err != nil {
		t.Fatal(err)
	}
	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(config))
	blobs[configDigest] = config
	manifest, err := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": configDigest, "size": len(config)}, "layers": layers})
	if err != nil {
		t.Fatal(err)
	}
	var blockSecond atomic.Bool
	blockSecond.Store(true)
	var firstLayerGETs atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			w.Write([]byte("{}"))
		case strings.Contains(r.URL.Path, "/manifests/"):
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.Write(manifest)
		case strings.Contains(r.URL.Path, "/blobs/"):
			digest := filepath.Base(r.URL.Path)
			blob, ok := blobs[digest]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if r.Method == http.MethodGet {
				if digest == digests[0] {
					firstLayerGETs.Add(1)
				}
				if digest == digests[1] && blockSecond.Load() {
					<-r.Context().Done()
					return
				}
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", fmt.Sprint(len(blob)))
			w.Write(blob)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	certFile := filepath.Join(t.TempDir(), "registry-ca.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", certFile)
	image := strings.TrimPrefix(server.URL, "https://") + "/homectl-import-test:first"
	cache := t.TempDir()
	loader := NewWithRunner(execRunner{}, cache)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- loader.Ensure(ctx, image, func(line string) { t.Log(line) }) }()
	blobPath := filepath.Join(loader.stagingDir(image), "source", "blobs", "sha256", strings.TrimPrefix(digests[0], "sha256:"))
	for {
		if _, err := os.Stat(blobPath); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("copy ended before retaining a completed layer: %v", err)
		case <-ctx.Done():
			t.Fatal("first layer was not persisted")
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled first attempt, got %v", err)
	}
	blockSecond.Store(false)
	ctx, cancel = context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// A fresh Loader with the same staging root also reuses completed blobs.
	loader = NewWithRunner(execRunner{}, cache)
	defer exec.Command("docker", "image", "rm", image).Run()
	if err := loader.Ensure(ctx, image, func(line string) { t.Log(line) }); err != nil {
		t.Fatal(err)
	}
	if got := firstLayerGETs.Load(); got != 1 {
		t.Fatalf("completed layer downloaded %d times, want 1", got)
	}
	output, err := exec.Command("docker", "image", "inspect", "--format", "{{json .RootFS.Layers}}", image).Output()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, diffIDs) {
		t.Fatalf("imported layer diffIDs = %v, want %v", got, diffIDs)
	}
	if _, err := os.Stat(loader.stagingDir(image)); !os.IsNotExist(err) {
		t.Fatalf("staging retained after import: %v", err)
	}
	if err := loader.Ensure(ctx, image, func(line string) { t.Log(line) }); err != nil {
		t.Fatal(err)
	}
	if got := firstLayerGETs.Load(); got != 1 {
		t.Fatalf("cached start downloaded a layer: %d", got)
	}
}
