package technitium

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUninitializedRestoreRepairsNodeIdentityBeforeJoin(t *testing.T) {
	for _, failRepair := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful repair", true: "failed repair"}[failRepair], func(t *testing.T) {
			stateCalls, repairs, joins := 0, 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/user/login":
					_, _ = w.Write([]byte(`{"status":"ok","token":"session"}`))
				case "/api/admin/cluster/state":
					stateCalls++
					if stateCalls == 1 {
						_, _ = w.Write([]byte(`{"status":"ok","response":{"clusterInitialized":false,"dnsServerDomain":"node-1"}}`))
					} else {
						_, _ = w.Write([]byte(`{"status":"ok","response":{"clusterInitialized":true,"dnsServerDomain":"node-1.cluster.home.arpa","clusterNodes":[{"name":"node-1.cluster.home.arpa","type":"Primary","state":"Self"}]}}`))
					}
				case "/api/settings/set":
					_ = r.ParseForm()
					if r.Form.Get("dnsServerDomain") != "node-2.cluster.home.arpa" {
						t.Errorf("wrong node identity: %q", r.Form.Get("dnsServerDomain"))
					}
					repairs++
					if failRepair {
						_, _ = w.Write([]byte(`{"status":"error","errorMessage":"repair rejected"}`))
					} else {
						_, _ = w.Write([]byte(`{"status":"ok"}`))
					}
				case "/api/admin/cluster/initJoin":
					if repairs != 1 {
						t.Error("joined before identity repair")
					}
					joins++
					_, _ = w.Write([]byte(`{"status":"ok"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			controller := NewController()
			controller.client = &http.Client{Transport: rewriteTransport{target: mustURL(t, server.URL)}}
			err := controller.ReconcileCluster(context.Background(), Config{
				NodeName: "node-2", NodeIP: "192.168.50.102", GatewayNode: "node-1", GatewayIP: "192.168.50.101",
				Password: "secret", ClusterDomain: "cluster.home.arpa", DataDir: t.TempDir(), HostDataDir: "/srv/homectl/app-data",
			})
			if (err != nil) != failRepair || repairs != 1 || (joins == 0) != failRepair {
				t.Fatalf("err=%v repairs=%d joins=%d", err, repairs, joins)
			}
		})
	}
}
