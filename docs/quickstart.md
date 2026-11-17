# Two-node quickstart

Use two disposable Linux hosts with Docker Engine and Compose v2. Homectl mounts
the Docker socket, which gives it administrative access to the Docker host.

The manager needs a reachable public IPv4 address, DNS pointing to that address,
TCP 80 and 443 for HTTPS, and UDP 51844 reachable from the agent for WireGuard.
Use a DNS zone you control and a scoped token for the DNS provider configured in
the manager Compose file. Reserve an unused address in the relevant LAN if using
the gateway VIP.

1. Check out the release on both hosts.

   ```sh
   git clone --branch v0.2.0 --depth 1 https://github.com/jatinparab/homectl.git
   cd homectl
   ```

2. Copy the contents of `examples/homelab/` to the root of a Git repository
   readable by the cluster and push it. Set `HOMECTL_GIT_REPO` to its URL.
3. On the manager host, copy `docker-compose.example.yml` to `compose.yaml`.
   Set the image to `ghcr.io/jatinparab/homectl/manager:v0.2.0`. Fill in the
   public IPv4 address, manager hostname, domain suffix, and Caddy settings.
   Set `HOMECTL_JOIN_TOKEN`, `ADMIN_PASSWORD`, and `CF_DNS_API_TOKEN` in an
   untracked local `.env` file. Use a long random join token and admin password.
   Review the optional settings and run `docker compose up -d`.
4. On the agent host, copy `docker-compose.agent.example.yml` to `compose.yaml`.
   Set the image to `ghcr.io/jatinparab/homectl/agent:v0.2.0`, give it a stable
   hostname, and point `HOMECTL_MANAGER_URL` at the manager HTTPS origin. Set
   the same `HOMECTL_JOIN_TOKEN` in a local `.env` file and run
   `docker compose up -d`.

Sign in at the manager hostname. Check that the agent is online and that the
`hello` app is running on that agent. The initial join uses HTTPS. After join,
the agent uses the WireGuard overlay for manager traffic at
`http://10.44.0.1:1338` with the default configuration. On the agent, use
`docker ps` to confirm the Nginx container is running. The sample app needs no
public route or backup storage.

Use `docker compose down` on the agent and manager to stop the control-plane
containers. On disposable hosts, delete the hosts when you finish to remove
managed containers and persisted state.

## Build from source

To build both images locally, run `make docker-images` from the repository root
on each host and keep the example Compose image names. Use the same revision
for the manager and agent.

## Agent settings

The example configures bootstrap DNS separately from the host resolver so the
agent can rejoin after a cold start. Shared Caddy, DNS, virtual IP, and Restic
values belong in the manager's node settings. The manager stores them encrypted
and syncs them to agents in memory.
