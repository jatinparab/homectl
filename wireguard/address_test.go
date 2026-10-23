package wireguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentUpdatesExistingInterfaceAddressOnRejoin(t *testing.T) {
	for _, tc := range []struct {
		name, existing string
		wantChange     bool
	}{
		{"unchanged", "4: wg0 inet 10.44.0.3/24 scope global wg0", false},
		{"reassigned", "4: wg0 inet 10.44.0.4/24 scope global wg0", true},
		{"missing", "", true},
		{"multiple stale addresses", "4: wg0 inet 10.44.0.4/24 scope global wg0\n4: wg0 inet 10.44.0.3/24 scope global secondary wg0", true},
		{"wrong prefix", "4: wg0 inet 10.44.0.3/32 scope global wg0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "commands")
			commands := map[string]string{
				"wg-quick": "#!/bin/sh\necho 'wg0 already exists' >&2\nexit 1\n",
				"wg":       "#!/bin/sh\nwhile IFS= read -r line; do :; done\n",
				"ip":       "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$IP_COMMANDS\"\nif [ \"$*\" = '-4 -o addr show dev wg0' ]; then printf '%s\\n' \"$IP_EXISTING\"; fi\n",
			}
			for name, body := range commands {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("IP_COMMANDS", logPath)
			t.Setenv("IP_EXISTING", tc.existing)
			if err := EnsureAgent(AgentConfig{
				ConfigDir: filepath.Join(dir, "config"), Address: "10.44.0.3", EndpointIP: "203.0.113.10",
				PrivateKey: "test-private-fixture", ManagerPublicKey: "test-public-fixture",
			}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			log := string(data)
			if strings.Contains(log, "-4 addr flush dev wg0") != tc.wantChange || strings.Contains(log, "-4 addr add 10.44.0.3/24 dev wg0") != tc.wantChange {
				t.Fatalf("incorrect interface reconciliation: %s", log)
			}
			if tc.wantChange && strings.Index(log, "addr flush") > strings.Index(log, "addr add") {
				t.Fatal("added new address before removing stale primary")
			}
		})
	}
}

func TestAgentAddressReconciliationFailsClosed(t *testing.T) {
	for _, fail := range []string{"inspect", "flush", "add"} {
		t.Run(fail, func(t *testing.T) {
			dir := t.TempDir()
			body := "#!/bin/sh\ncase \"$*\" in\n '-4 -o addr show dev wg0') [ \"$IP_FAIL\" != inspect ] ;;\n '-4 addr flush dev wg0') [ \"$IP_FAIL\" != flush ] ;;\n '-4 addr add 10.44.0.3/24 dev wg0') [ \"$IP_FAIL\" != add ] ;;\n *) exit 1 ;;\nesac\n"
			if err := os.WriteFile(filepath.Join(dir, "ip"), []byte(body), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("IP_FAIL", fail)
			if err := syncAgentAddress("10.44.0.3"); err == nil {
				t.Fatal("failed address update accepted")
			}
		})
	}
}
