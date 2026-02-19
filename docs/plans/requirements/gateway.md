Setup gateway

Locally between the nodes, I'm setting up a keepalived VIP network based on the /ready endpoint. This means that when I try to query for services it goes through whatever node is currently the master on the network.

Currently the priority is managed by keepalived. That 192.168.1.101 is first, then 102 then 103. 

I want this to be manager controlled, instead of all nodes replying ok to /healthy I want the manager to assign a "Gateway" status to the node, depending on whether traefik is currently running on that node or not. If a gateway node goes down, the manager immediately tries to find another node and assigs it the Gateway status if traefik is running there. This way keepavlied will also route traffic correctly from the gateway node only.

How this affects global app backups: 

Let's say I'm making some changes on a global app, since there are multiple instances, the latest changes will only be present on the app which is currently handling the requests.. I want the backups to only happen via that node since it has latest changes.

Other nodes should pull the latest backup (if one exists, as reported by the gateway node) instead of backuping stale data when their time for backup comes.

In the UI,
Show a badge of gateway to the currently assigned gateway node.
