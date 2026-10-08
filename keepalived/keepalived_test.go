package keepalived

import (
	"strings"
	"testing"
)

func TestDNSVRRPIndependentFromGateway(t *testing.T) {
	config := dnsConfig("eth0", "192.168.50.251")
	for _, want := range []string{"virtual_router_id 45", "192.168.50.251", "nopreempt", "init_fail", "fall 2", "rise 3", "dns_ready"} {
		if !strings.Contains(config, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(config, "virtual_router_id 44") {
		t.Fatal("DNS election shares HTTP VRID")
	}
}

func TestDNSRejectsInvalidOrSharedVIPBeforeDocker(t *testing.T) {
	for _, vip := range []string{"192.168.50.250", "::ffff:192.168.50.250", "::ffff:192.168.50.251", "::1", "0.0.0.0", "127.0.0.1", "224.0.0.1", "not-an-ip"} {
		if err := EnsureDNS(t.TempDir(), "/unused", vip, "192.168.50.250"); err == nil {
			t.Errorf("accepted invalid DNS VIP %q", vip)
		}
	}
}

func TestVIPOwnershipMatchesExactAddress(t *testing.T) {
	addresses := "2: eth0 inet 192.168.50.251/32 scope global eth0\n"
	if !hasVIP(addresses, "192.168.50.251") {
		t.Fatal("VIP not detected")
	}
	if hasVIP(addresses, "192.168.1.13") || hasVIP(addresses, "192.168.50.250") {
		t.Fatal("wrong VIP detected")
	}
}

func TestDefaultRouteInterface(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "gateway route",
			in:   "default via 192.168.1.1 dev eth0 proto dhcp src 192.168.50.103 metric 100\n",
			want: "eth0",
		},
		{
			name: "direct route",
			in:   "default dev wlan0 scope link\n",
			want: "wlan0",
		},
		{
			name: "ignores non default routes",
			in:   "10.0.0.0/24 dev wg0 scope link\ndefault via 10.0.0.1 dev enp1s0\n",
			want: "enp1s0",
		},
		{
			name: "missing dev",
			in:   "default via 192.168.1.1\n",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultRouteInterface(tt.in); got != tt.want {
				t.Fatalf("defaultRouteInterface() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUniqueStrings(t *testing.T) {
	got := uniqueStrings([]string{" homectl/agent:local ", "", "homectl/agent:latest", "homectl/agent:local"})
	want := []string{"homectl/agent:local", "homectl/agent:latest"}
	if len(got) != len(want) {
		t.Fatalf("uniqueStrings() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("uniqueStrings()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
