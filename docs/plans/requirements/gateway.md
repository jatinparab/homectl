Setup gateway

Locally between the nodes, I'm setting up a keepalived VIP network based on the /ready endpoint. This means that when I try to query for services it goes through whatever node is currently the master on the network.

Previously the priority was managed by keepalived against static LAN addresses. The publishable setup should derive gateway state from registered agents on the WireGuard overlay instead.

I want this to be manager controlled, instead of all nodes replying ok to /healthy I want the manager to assign a "Gateway" status to the node, depending on whether traefik is currently running on that node or not. If a gateway node goes down, the manager immediately tries to find another node and assigs it the Gateway status if traefik is running there. This way keepavlied will also route traffic correctly from the gateway node only.

How this affects global app backups: 

Let's say I'm making some changes on a global app, since there are multiple instances, the latest changes will only be present on the app which is currently handling the requests.. I want the backups to only happen via that node since it has latest changes.

Other nodes should pull the latest backup (if one exists, as reported by the gateway node) instead of backuping stale data when their time for backup comes.

In the UI,
Show a badge of gateway to the currently assigned gateway node.
