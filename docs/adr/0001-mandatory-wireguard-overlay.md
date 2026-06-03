# Mandatory WireGuard Overlay

Homectl uses a mandatory WireGuard overlay for manager-agent control traffic instead of assuming the manager and agents share a LAN, a pre-existing VPN, or reachable private addresses. The manager endpoint is configured as an explicit public IPv4 address with UDP port `51844`, because a containerized manager cannot reliably infer the externally routable address that agents must dial.
