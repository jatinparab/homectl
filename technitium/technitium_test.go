package technitium

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type runnerCall struct {
	env  map[string]string
	args []string
}

type fakeRunner struct {
	mu        sync.Mutex
	calls     []runnerCall
	responses func(args []string) ([]byte, error)
}

func (r *fakeRunner) Run(_ context.Context, env map[string]string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyEnv := map[string]string{}
	for key, value := range env {
		copyEnv[key] = value
	}
	r.calls = append(r.calls, runnerCall{env: copyEnv, args: append([]string(nil), args...)})
	if r.responses != nil {
		return r.responses(args)
	}
	return nil, nil
}

func TestEnsurePassesPasswordOnlyThroughEnvironment(t *testing.T) {
	runner := &fakeRunner{responses: func(args []string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "inspect" {
			return nil, errors.New("not found")
		}
		return nil, nil
	}}
	controller := NewController()
	controller.runner = runner
	password := "very-secret-password"
	dataDir := t.TempDir()
	hostDataDir := "/srv/homectl/app-data"
	cfg := Config{
		NodeName:      "node1",
		NodeIP:        "10.44.0.2",
		Password:      password,
		ClusterDomain: "cluster.home.arpa",
		DataDir:       dataDir,
		HostDataDir:   hostDataDir,
	}
	if err := controller.Ensure(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	var runCall *runnerCall
	for i := range runner.calls {
		call := &runner.calls[i]
		for _, arg := range call.args {
			if strings.Contains(arg, password) {
				t.Fatal("password leaked into docker command arguments")
			}
		}
		if len(call.args) > 0 && call.args[0] == "run" {
			runCall = call
		}
	}
	if runCall == nil {
		t.Fatal("docker run was not invoked")
	}
	if got := runCall.env["DNS_SERVER_ADMIN_PASSWORD"]; got != password {
		t.Fatalf("password environment = %q", got)
	}
	if !containsAdjacent(runCall.args, "-e", "DNS_SERVER_ADMIN_PASSWORD") {
		t.Fatal("docker run did not inherit DNS_SERVER_ADMIN_PASSWORD")
	}
	if got := filepath.Join(cfg.HostDataDir, "technitium", "config"); !containsSubstring(runCall.args, got+":/etc/dns") {
		t.Fatalf("docker run did not mount %s", got)
	}
}

func TestEnsureKeepsMatchingRunningContainer(t *testing.T) {
	cfg := Config{NodeName: "node1", NodeIP: "10.44.0.2", Password: "secret", DataDir: t.TempDir(), HostDataDir: "/srv/homectl/app-data"}
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{responses: func(args []string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "inspect" && args[len(args)-1] == ContainerName {
			return []byte("true|" + configHash(normalized)), nil
		}
		return nil, nil
	}}
	controller := NewController()
	controller.runner = runner
	if err := controller.Ensure(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.calls {
		if len(call.args) > 0 && call.args[0] == "run" {
			t.Fatal("matching running container was recreated")
		}
	}
}

func TestGatewaySecondaryPromotesGracefully(t *testing.T) {
	var promoted string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			_, _ = w.Write([]byte(`{"status":"ok","token":"session"}`))
		case "/api/admin/cluster/state":
			_, _ = w.Write([]byte(`{"status":"ok","response":{"clusterInitialized":true,"dnsServerDomain":"node2.cluster.home.arpa","clusterDomain":"cluster.home.arpa","clusterNodes":[{"name":"node1.cluster.home.arpa","type":"Primary","state":"Connected"},{"name":"node2.cluster.home.arpa","type":"Secondary","state":"Self"}]}}`))
		case "/api/admin/cluster/secondary/promote":
			_ = r.ParseForm()
			promoted = r.Form.Get("forceDeletePrimary")
			_, _ = w.Write([]byte(`{"status":"ok","response":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	controller := NewController()
	controller.client = &http.Client{Transport: rewriteTransport{target: mustURL(t, server.URL)}}
	controller.gatewayIdentity = "true|node2|10.44.0.3"
	controller.gatewaySince = time.Now().Add(-time.Minute)
	if err := controller.ReconcileCluster(context.Background(), Config{
		NodeName: "node2", NodeIP: "10.44.0.3", GatewayNode: "node2", GatewayIP: "10.44.0.3",
		IsGateway: true, Password: "secret", ClusterDomain: "cluster.home.arpa", DataDir: t.TempDir(), HostDataDir: "/srv/homectl/app-data",
	}); err != nil {
		t.Fatal(err)
	}
	if promoted != "false" {
		t.Fatalf("forceDeletePrimary = %q, want false", promoted)
	}
}

func TestFormerPrimaryRemovesStaleClusterZoneBeforeRejoin(t *testing.T) {
	stateCalls := 0
	joinCalls := 0
	deletedZone := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			_, _ = w.Write([]byte(`{"status":"ok","token":"session"}`))
		case "/api/admin/cluster/state":
			stateCalls++
			if stateCalls == 1 {
				_, _ = w.Write([]byte(`{"status":"ok","response":{"clusterInitialized":false,"dnsServerDomain":"node1"}}`))
			} else {
				_, _ = w.Write([]byte(`{"status":"ok","response":{"clusterInitialized":true,"dnsServerDomain":"node2.cluster.home.arpa","clusterNodes":[{"name":"node2.cluster.home.arpa","type":"Primary","state":"Self"}]}}`))
			}
		case "/api/admin/cluster/initJoin":
			joinCalls++
			if joinCalls == 1 {
				_, _ = w.Write([]byte(`{"status":"error","errorMessage":"Failed to join Cluster: the zone 'cluster.home.arpa' already exists."}`))
			} else {
				_, _ = w.Write([]byte(`{"status":"ok","response":{}}`))
			}
		case "/api/zones/delete":
			_ = r.ParseForm()
			deletedZone = r.Form.Get("zone")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	controller := NewController()
	controller.client = &http.Client{Transport: rewriteTransport{target: mustURL(t, server.URL)}}
	if err := controller.ReconcileCluster(context.Background(), Config{
		NodeName: "node1", NodeIP: "192.168.50.101", GatewayNode: "node2", GatewayIP: "192.168.50.102",
		Password: "secret", ClusterDomain: "cluster.home.arpa", DataDir: t.TempDir(), HostDataDir: "/srv/homectl/app-data",
	}); err != nil {
		t.Fatal(err)
	}
	if joinCalls != 2 || deletedZone != "cluster.home.arpa" {
		t.Fatalf("join calls=%d deleted zone=%q", joinCalls, deletedZone)
	}
}

func TestGatewaySecondaryForcePromotesAfterDelay(t *testing.T) {
	var promoted string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			_, _ = w.Write([]byte(`{"status":"ok","token":"session"}`))
		case "/api/admin/cluster/state":
			_, _ = w.Write([]byte(`{"status":"ok","response":{"clusterInitialized":true,"dnsServerDomain":"node2.cluster.home.arpa","clusterNodes":[{"name":"node1.cluster.home.arpa","type":"Primary","state":"Connected"},{"name":"node2.cluster.home.arpa","type":"Secondary","state":"Self"}]}}`))
		case "/api/admin/cluster/secondary/promote":
			_ = r.ParseForm()
			promoted = r.Form.Get("forceDeletePrimary")
			_, _ = w.Write([]byte(`{"status":"ok","response":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	controller := NewController()
	controller.client = &http.Client{Transport: rewriteTransport{target: mustURL(t, server.URL)}}
	controller.gatewayIdentity = "true|node2|10.44.0.3"
	controller.gatewaySince = time.Now().Add(-3 * time.Minute)
	controller.lastGracefulFailed = true
	if err := controller.ReconcileCluster(context.Background(), Config{
		NodeName: "node2", NodeIP: "10.44.0.3", GatewayNode: "node2", GatewayIP: "10.44.0.3",
		IsGateway: true, Password: "secret", ClusterDomain: "cluster.home.arpa", DataDir: t.TempDir(), HostDataDir: "/srv/homectl/app-data",
	}); err != nil {
		t.Fatal(err)
	}
	if promoted != "true" {
		t.Fatalf("forceDeletePrimary = %q, want true", promoted)
	}
}

type rewriteTransport struct{ target *url.URL }

func (r rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = r.target.Scheme
	clone.URL.Host = r.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func containsAdjacent(values []string, first, second string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == first && values[i+1] == second {
			return true
		}
	}
	return false
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
