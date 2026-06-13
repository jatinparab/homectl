package keepalived

import "testing"

func TestDefaultRouteInterface(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "gateway route",
			in:   "default via 192.168.1.1 dev eth0 proto dhcp src 192.168.1.103 metric 100\n",
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
