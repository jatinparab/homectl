package technitium

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	PromotionDebounce   = 30 * time.Second
	ForcePromotionAfter = 2 * time.Minute
)

type clusterNode struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	IPAddresses []string `json:"ipAddresses"`
	Type        string   `json:"type"`
	State       string   `json:"state"`
}

type clusterState struct {
	ClusterInitialized bool          `json:"clusterInitialized"`
	DNSServerDomain    string        `json:"dnsServerDomain"`
	ClusterDomain      string        `json:"clusterDomain"`
	ClusterNodes       []clusterNode `json:"clusterNodes"`
	Nodes              []clusterNode `json:"nodes"`
}

func (s clusterState) allNodes() []clusterNode {
	if len(s.ClusterNodes) > 0 {
		return s.ClusterNodes
	}
	return s.Nodes
}

func (s clusterState) self() (clusterNode, bool) {
	for _, node := range s.allNodes() {
		if strings.EqualFold(node.State, "Self") || strings.EqualFold(node.Name, s.DNSServerDomain) {
			return node, true
		}
	}
	return clusterNode{}, false
}

func (s clusterState) primary() (clusterNode, bool) {
	for _, node := range s.allNodes() {
		if strings.EqualFold(node.Type, "Primary") {
			return node, true
		}
	}
	return clusterNode{}, false
}

type apiResponse struct {
	Status       string       `json:"status"`
	ErrorMessage string       `json:"errorMessage"`
	Token        string       `json:"token"`
	Response     clusterState `json:"response"`
}

func (c *Controller) ReconcileCluster(ctx context.Context, raw Config) error {
	cfg, err := normalizeConfig(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.GatewayNode) == "" || strings.TrimSpace(cfg.GatewayIP) == "" {
		return errors.New("gateway identity is not available")
	}

	c.mu.Lock()
	identity := fmt.Sprintf("%t|%s|%s", cfg.IsGateway, cfg.GatewayNode, cfg.GatewayIP)
	if c.gatewayIdentity != identity {
		c.gatewayIdentity = identity
		c.gatewaySince = time.Now()
		c.lastGracefulFailed = false
	}
	gatewaySince := c.gatewaySince
	lastGracefulFailed := c.lastGracefulFailed
	clusterRefreshNeeded := c.clusterRefreshNeeded
	c.mu.Unlock()

	localBase := fmt.Sprintf("http://%s:%d", ContainerName, AdminPort)
	state, err := c.getClusterState(ctx, localBase, cfg.Password)
	if err != nil {
		return err
	}
	if !state.ClusterInitialized {
		// A restored backup can retain another node's server domain. Docker's
		// DNS_SERVER_DOMAIN only seeds fresh data; repair the local identity
		// before joining, without renaming an established cluster member.
		expectedDomain := cfg.NodeName + "." + cfg.ClusterDomain
		if !strings.EqualFold(state.DNSServerDomain, expectedDomain) {
			token, err := c.login(ctx, localBase, cfg.Password)
			if err != nil {
				return err
			}
			if _, err := c.postForm(ctx, localBase+"/api/settings/set", token, url.Values{"dnsServerDomain": {expectedDomain}}); err != nil {
				return fmt.Errorf("repair uninitialized node identity: %w", err)
			}
		}
		if cfg.IsGateway {
			return c.initializeCluster(ctx, localBase, cfg)
		}
		return c.joinCluster(ctx, localBase, cfg)
	}

	self, ok := state.self()
	if !ok {
		return errors.New("Technitium cluster state has no self node")
	}
	expectedPrimary := cfg.GatewayNode + "." + cfg.ClusterDomain
	if cfg.IsGateway {
		if strings.EqualFold(self.Type, "Primary") {
			c.clearClusterRefresh()
			return nil
		}
		if time.Since(gatewaySince) < PromotionDebounce {
			return nil
		}
		force := lastGracefulFailed && time.Since(gatewaySince) >= ForcePromotionAfter
		err := c.promote(ctx, localBase, cfg.Password, force)
		if err != nil && !force {
			c.mu.Lock()
			c.lastGracefulFailed = true
			c.mu.Unlock()
		}
		return err
	}

	if strings.EqualFold(self.Type, "Primary") {
		remoteState, remoteErr := c.getClusterState(ctx, gatewayAPIBase(cfg.GatewayIP), cfg.Password)
		if remoteErr != nil {
			return fmt.Errorf("verify replacement primary: %w", remoteErr)
		}
		remoteSelf, remoteOK := remoteState.self()
		if !remoteOK || !remoteState.ClusterInitialized || !strings.EqualFold(remoteSelf.Type, "Primary") || !strings.EqualFold(remoteSelf.Name, expectedPrimary) {
			return errors.New("replacement gateway is not yet the Technitium primary")
		}
		if err := c.deleteLocalCluster(ctx, localBase, cfg.Password); err != nil {
			return err
		}
		return c.joinCluster(ctx, localBase, cfg)
	}

	primary, ok := state.primary()
	if ok && strings.EqualFold(primary.Name, expectedPrimary) && strings.EqualFold(primary.State, "Connected") && !clusterRefreshNeeded {
		return nil
	}
	remoteState, remoteErr := c.getClusterState(ctx, gatewayAPIBase(cfg.GatewayIP), cfg.Password)
	if remoteErr != nil {
		return fmt.Errorf("verify gateway primary: %w", remoteErr)
	}
	remoteSelf, remoteOK := remoteState.self()
	if !remoteOK || !remoteState.ClusterInitialized || !strings.EqualFold(remoteSelf.Type, "Primary") || !strings.EqualFold(remoteSelf.Name, expectedPrimary) {
		return errors.New("gateway is not yet the Technitium primary")
	}
	if err := c.updatePrimary(ctx, localBase, cfg.Password, cfg); err != nil {
		return err
	}
	c.clearClusterRefresh()
	return nil
}

func (c *Controller) clearClusterRefresh() {
	c.mu.Lock()
	c.clusterRefreshNeeded = false
	c.mu.Unlock()
}

func gatewayAPIBase(ip string) string {
	return fmt.Sprintf("http://%s:%d", strings.TrimSpace(ip), AdminPort)
}

func (c *Controller) login(ctx context.Context, base, password string) (string, error) {
	response, err := c.postForm(ctx, base+"/api/user/login", "", url.Values{
		"user":        {"admin"},
		"pass":        {password},
		"includeInfo": {"true"},
	})
	if err != nil {
		return "", err
	}
	if response.Token == "" {
		return "", errors.New("Technitium login returned no token")
	}
	return response.Token, nil
}

func (c *Controller) rotatePassword(ctx context.Context, base, oldPassword, newPassword string) error {
	if _, err := c.login(ctx, base, newPassword); err == nil {
		return nil
	}
	token, err := c.login(ctx, base, oldPassword)
	if err != nil {
		return err
	}
	_, err = c.postForm(ctx, base+"/api/user/changePassword", token, url.Values{
		"pass":    {oldPassword},
		"newPass": {newPassword},
	})
	return err
}

func (c *Controller) getClusterState(ctx context.Context, base, password string) (clusterState, error) {
	token, err := c.login(ctx, base, password)
	if err != nil {
		return clusterState{}, err
	}
	response, err := c.request(ctx, http.MethodGet, base+"/api/admin/cluster/state?includeServerIpAddresses=true", token, nil)
	if err != nil {
		return clusterState{}, err
	}
	return response.Response, nil
}

func (c *Controller) initializeCluster(ctx context.Context, base string, cfg Config) error {
	token, err := c.login(ctx, base, cfg.Password)
	if err != nil {
		return err
	}
	_, err = c.postForm(ctx, base+"/api/admin/cluster/init", token, url.Values{
		"clusterDomain":          {cfg.ClusterDomain},
		"primaryNodeIpAddresses": {cfg.NodeIP},
	})
	return err
}

func (c *Controller) joinCluster(ctx context.Context, base string, cfg Config) error {
	remoteState, err := c.getClusterState(ctx, gatewayAPIBase(cfg.GatewayIP), cfg.Password)
	if err != nil {
		return fmt.Errorf("read gateway cluster state: %w", err)
	}
	remoteSelf, ok := remoteState.self()
	if !ok || !remoteState.ClusterInitialized || !strings.EqualFold(remoteSelf.Type, "Primary") {
		return errors.New("gateway Technitium node is not initialized as primary")
	}
	token, err := c.login(ctx, base, cfg.Password)
	if err != nil {
		return err
	}
	join := func() error {
		_, joinErr := c.postForm(ctx, base+"/api/admin/cluster/initJoin", token, url.Values{
			"secondaryNodeIpAddresses": {cfg.NodeIP},
			"primaryNodeUrl":           {primaryURL(cfg)},
			"primaryNodeIpAddress":     {cfg.GatewayIP},
			"ignoreCertificateErrors":  {"true"},
			"primaryNodeUsername":      {"admin"},
			"primaryNodePassword":      {cfg.Password},
			"primaryNodeTotp":          {""},
		})
		return joinErr
	}
	if err := join(); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "zone '") || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(cfg.ClusterDomain)) {
			return err
		}
		// A former primary retains its generated cluster zones after promotion.
		// They are stale local copies; the new primary owns the authoritative data.
		_ = c.deleteZone(ctx, base, token, "cluster-catalog."+cfg.ClusterDomain)
		if deleteErr := c.deleteZone(ctx, base, token, cfg.ClusterDomain); deleteErr != nil {
			return fmt.Errorf("remove stale cluster zone: %w", deleteErr)
		}
		return join()
	}
	return nil
}

func (c *Controller) deleteZone(ctx context.Context, base, token, zone string) error {
	_, err := c.postForm(ctx, base+"/api/zones/delete", token, url.Values{"zone": {zone}})
	return err
}

func primaryURL(cfg Config) string {
	return fmt.Sprintf("https://%s.%s:%d/", cfg.GatewayNode, cfg.ClusterDomain, ClusterPort)
}

func (c *Controller) promote(ctx context.Context, base, password string, force bool) error {
	if !force {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	token, err := c.login(ctx, base, password)
	if err != nil {
		return err
	}
	_, err = c.postForm(ctx, base+"/api/admin/cluster/secondary/promote", token, url.Values{
		"forceDeletePrimary": {fmt.Sprintf("%t", force)},
	})
	return err
}

func (c *Controller) deleteLocalCluster(ctx context.Context, base, password string) error {
	token, err := c.login(ctx, base, password)
	if err != nil {
		return err
	}
	_, err = c.postForm(ctx, base+"/api/admin/cluster/primary/delete", token, url.Values{"forceDelete": {"true"}})
	return err
}

func (c *Controller) updatePrimary(ctx context.Context, base, password string, cfg Config) error {
	token, err := c.login(ctx, base, password)
	if err != nil {
		return err
	}
	_, err = c.postForm(ctx, base+"/api/admin/cluster/secondary/updatePrimary", token, url.Values{
		"primaryNodeUrl":         {primaryURL(cfg)},
		"primaryNodeIpAddresses": {cfg.GatewayIP},
	})
	return err
}

func (c *Controller) postForm(ctx context.Context, endpoint, token string, values url.Values) (apiResponse, error) {
	return c.request(ctx, http.MethodPost, endpoint, token, strings.NewReader(values.Encode()))
}

func (c *Controller) request(ctx context.Context, method, endpoint, token string, body io.Reader) (apiResponse, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return apiResponse{}, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return apiResponse{}, err
	}
	defer response.Body.Close()
	var result apiResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&result); err != nil {
		return apiResponse{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return apiResponse{}, fmt.Errorf("Technitium API returned HTTP %d", response.StatusCode)
	}
	if result.Status != "ok" {
		if result.ErrorMessage == "" {
			result.ErrorMessage = result.Status
		}
		return apiResponse{}, errors.New(result.ErrorMessage)
	}
	return result, nil
}
