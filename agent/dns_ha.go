package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/config"
	"github.com/jatinparab/homectl/keepalived"
	"github.com/jatinparab/homectl/technitium"
)

func (a *Agent) ensureDNSKeepalived() {
	hostConfigRoot, err := resolveSelfBindMountSource(config.ConfigDir)
	if err != nil {
		agentLog.Error("Failed to resolve config host path for DNS keepalived", "err", err)
		return
	}
	if err := keepalived.EnsureDNS(filepath.Join(config.ConfigDir, "dns-keepalived"), filepath.Join(hostConfigRoot, "dns-keepalived"), a.configValue(common.ConfigKeyDNSHAIP), a.configValue(common.ConfigKeyLocalHAIP)); err != nil {
		agentLog.Error("Failed to reconcile DNS keepalived", "err", err)
	}
}

func (a *Agent) dnsEligible() bool {
	if a.configValue(common.ConfigKeyDNSHAIP) == "" || a.dnsDraining.Load() {
		return false
	}
	rt := a.getRuntime(common.DNSAppID)
	if rt == nil {
		return false
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.Running == common.RunningStatusRunning && rt.Health == common.HealthHealthy && rt.Phase == common.PhaseRunning && !rt.Quarantined
}

func (rs *ReadyServer) handleDNSReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// No manager connection/gateway/primary requirement: a functioning secondary
	// continues resolving when the VPS or the primary is offline.
	if !rs.agent.dnsEligible() {
		http.Error(w, "DNS not eligible or draining", http.StatusServiceUnavailable)
		return
	}
	ip := rs.agent.configValue(common.ConfigKeyTechnitiumNodeIP)
	if net.ParseIP(ip) == nil {
		http.Error(w, "DNS node IP missing", http.StatusServiceUnavailable)
		return
	}
	if err := technitium.CheckDNS(r.Context(), net.JoinHostPort(ip, "53"), rs.agent.configValue(common.ConfigKeyTechnitiumClusterDomain)); err != nil {
		http.Error(w, "DNS not ready", http.StatusServiceUnavailable)
		return
	}
	// A backup may have started during the probe.
	if !rs.agent.dnsEligible() {
		http.Error(w, "DNS draining", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}

// prepareDNSBackup is called before stopping DNS, with PhaseBackupStopping
// already set. The old resolver stays up while VRRP transfers ownership.
// A missing or broken replacement aborts the backup rather than stopping the
// final working DNS server. Actual host VIP ownership is checked, not a marker.
func (a *Agent) prepareDNSBackup(ctx context.Context) error {
	vip := a.configValue(common.ConfigKeyDNSHAIP)
	if vip == "" {
		return nil // backward compatible when DNS HA is not configured
	}
	a.dnsDraining.Store(true)
	defer a.dnsDraining.Store(false)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return waitDNSHandoff(ctx, time.Second, func(ctx context.Context) (bool, error) {
		owned, err := keepalived.OwnsDNSVIP(ctx, vip)
		if err != nil || owned {
			return false, err
		}
		if err := technitium.CheckDNS(ctx, net.JoinHostPort(vip, "53"), a.configValue(common.ConfigKeyTechnitiumClusterDomain)); err != nil {
			return false, nil // wait for a replacement to become reachable
		}
		return true, nil
	})
}

func waitDNSHandoff(ctx context.Context, interval time.Duration, replacementReady func(context.Context) (bool, error)) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	consecutive := 0
	for {
		ready, err := replacementReady(ctx)
		if err != nil {
			return err
		}
		if ready {
			consecutive++
			if consecutive >= 3 {
				return nil
			}
		} else {
			consecutive = 0
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("DNS backup deferred: no stable replacement VIP owner: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
