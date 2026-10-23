package manager

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jatinparab/homectl/common"
)

func wireGuardTestKey(name string) string {
	sum := sha256.Sum256([]byte(name))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func TestWireGuardAllocationAfterCleanupPreservesReservations(t *testing.T) {
	peers := []wireGuardPeerState{
		{Name: "node-1", PublicKey: wireGuardTestKey("node-1"), Address: "10.44.0.2"},
		{Name: "node-3", PublicKey: wireGuardTestKey("node-3"), Address: "10.44.0.4"},
	}
	peers, added, err := upsertWireGuardPeer(peers, "node-2", wireGuardTestKey("node-2"))
	if err != nil || added.Address != "10.44.0.3" {
		t.Fatalf("join after cleanup: address=%s err=%v", added.Address, err)
	}
	peers, rejoined, err := upsertWireGuardPeer(peers, "node-3", wireGuardTestKey("rotated"))
	if err != nil || rejoined.Address != "10.44.0.4" || rejoined.PublicKey != wireGuardTestKey("rotated") || len(peers) != 3 {
		t.Fatalf("rejoin moved reservation or failed: %+v err=%v", rejoined, err)
	}
}

func TestWireGuardRepairsExistingCollisionsWithoutMovingHealthyPeers(t *testing.T) {
	input := []wireGuardPeerState{
		{Name: "node-3", Address: "10.44.0.4"},
		{Name: "node-2", Address: "10.44.0.4"},
		{Name: "node-1", Address: "10.44.0.2"},
		{Name: "invalid", Address: "192.168.1.20"},
		{Name: "healthy", Address: "10.44.0.3"},
	}
	peers, repaired, err := normalizeWireGuardPeers(input)
	if err != nil || !repaired {
		t.Fatalf("repair: changed=%v err=%v", repaired, err)
	}
	addresses := make(map[string]string)
	used := make(map[string]bool)
	for _, peer := range peers {
		if !validWireGuardPeerAddress(peer.Address) || used[peer.Address] {
			t.Fatalf("invalid or duplicate repaired address: %+v", peer)
		}
		used[peer.Address] = true
		addresses[peer.Name] = peer.Address
	}
	if addresses["node-1"] != "10.44.0.2" || addresses["healthy"] != "10.44.0.3" || addresses["node-2"] != "10.44.0.4" {
		t.Fatalf("repair moved a retained reservation: %v", addresses)
	}
	if input[0].Address != "10.44.0.4" {
		t.Fatal("repair mutated caller's state")
	}
	if _, changed, err := normalizeWireGuardPeers(peers); err != nil || changed {
		t.Fatalf("repair not idempotent: changed=%v err=%v", changed, err)
	}
}

func TestWireGuardAddressPoolBoundariesAndExhaustion(t *testing.T) {
	for _, address := range []string{"10.44.0.0", "10.44.0.1", "10.44.0.255", "10.44.1.2", "::ffff:10.44.0.2", "::1", "garbage", ""} {
		if validWireGuardPeerAddress(address) {
			t.Errorf("accepted reserved/invalid address %q", address)
		}
	}
	var peers []wireGuardPeerState
	for i := 2; i <= 254; i++ {
		peers = append(peers, wireGuardPeerState{Name: fmt.Sprintf("node-%03d", i), PublicKey: wireGuardTestKey(fmt.Sprint(i)), Address: fmt.Sprintf("10.44.0.%d", i)})
	}
	if _, _, err := upsertWireGuardPeer(peers, "overflow", wireGuardTestKey("overflow")); err == nil {
		t.Fatal("exhausted address pool accepted a new peer")
	}
	if _, peer, err := upsertWireGuardPeer(peers, peers[0].Name, wireGuardTestKey("rotated")); err != nil || peer.Address != peers[0].Address {
		t.Fatalf("existing peer cannot rejoin a full pool: %+v err=%v", peer, err)
	}
}

func TestWireGuardRejectsClonedKeysAndInvalidIdentity(t *testing.T) {
	key := wireGuardTestKey("shared")
	peers := []wireGuardPeerState{{Name: "first", PublicKey: key, Address: "10.44.0.2"}}
	if _, _, err := upsertWireGuardPeer(peers, "clone", key); err == nil {
		t.Fatal("two agents accepted with the same public key")
	}
	for _, key := range []string{"", strings.Repeat("!", 44), base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, _, err := upsertWireGuardPeer(nil, "node", key); err == nil {
			t.Fatal("invalid public key accepted")
		}
	}
	if _, _, err := upsertWireGuardPeer(nil, " ", key); err == nil {
		t.Fatal("empty name accepted")
	}
}

func fakeWireGuardCommands(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	commands := map[string]string{
		"wg":       "#!/bin/sh\ncase \"$1\" in\n genkey) echo private-test-fixture ;;\n pubkey) echo '" + wireGuardTestKey("manager") + "' ;;\n *) while IFS= read -r line; do :; done ;;\nesac\n",
		"wg-quick": "#!/bin/sh\nexit 0\n",
		"ip":       "#!/bin/sh\nexit 0\n",
	}
	for name, body := range commands {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOMECTL_WG_ENDPOINT_IP", "203.0.113.10")
}

func TestWireGuardConcurrentJoinsCleanupAndAtomicReaders(t *testing.T) {
	fakeWireGuardCommands(t)
	m := &Manager{stateDir: t.TempDir(), leaseManager: NewLeaseManager(DefaultLeaseConfig())}
	const count = 16
	for i := 0; i < count; i++ {
		m.leaseManager.Heartbeat(fmt.Sprint(i), "", "", nil, common.NodeMetrics{})
	}
	done := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-done:
				return
			default:
				data, err := os.ReadFile(m.wireGuardPeersPath())
				if err != nil {
					if !os.IsNotExist(err) {
						t.Errorf("read peers: %v", err)
						return
					}
					continue
				}
				var peers []wireGuardPeerState
				if err := json.Unmarshal(data, &peers); err != nil {
					t.Errorf("reader observed partial JSON: %v", err)
					return
				}
			}
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := m.JoinAgent(fmt.Sprint(i), wireGuardTestKey(fmt.Sprint(i))); err != nil {
				t.Errorf("join: %v", err)
			}
			if err := m.CleanupStaleWireGuardPeers(); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}(i)
	}
	wg.Wait()
	close(done)
	<-readerDone
	peers, err := m.loadWireGuardPeers()
	if err != nil || len(peers) != count {
		t.Fatalf("lost concurrent registrations: count=%d err=%v", len(peers), err)
	}
	used := make(map[string]bool)
	for _, peer := range peers {
		if !validWireGuardPeerAddress(peer.Address) || used[peer.Address] {
			t.Fatalf("concurrent join reused address: %s", peer.Address)
		}
		used[peer.Address] = true
	}
	info, err := os.Stat(m.wireGuardPeersPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("peer file permissions: info=%v err=%v", info, err)
	}
}

func TestEnsureWireGuardRepairsPersistedCollisions(t *testing.T) {
	fakeWireGuardCommands(t)
	m := &Manager{stateDir: t.TempDir()}
	if err := m.saveWireGuardPeers([]wireGuardPeerState{
		{Name: "node-1", PublicKey: wireGuardTestKey("node-1"), Address: "10.44.0.2"},
		{Name: "node-2", PublicKey: wireGuardTestKey("node-2"), Address: "10.44.0.4"},
		{Name: "node-3", PublicKey: wireGuardTestKey("node-3"), Address: "10.44.0.4"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureWireGuard(); err != nil {
		t.Fatal(err)
	}
	peers, err := m.loadWireGuardPeers()
	if err != nil || len(peers) != 3 || peers[1].Address != "10.44.0.4" || peers[2].Address != "10.44.0.3" {
		t.Fatalf("startup did not persist collision repair: %+v err=%v", peers, err)
	}
}
