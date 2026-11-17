# App repository structure

Mount an app-definition directory containing a `docker/` directory at
`/homelab` in the manager container. You can manage these files with Git on
the host.
Each immediate child directory defines one app and its name becomes the app ID.

```text
docker/
└── hello/
    ├── docker-compose.yml
    └── homectl-config.json
```

The Compose file defines the services. The optional
[per-app config](homectl-config.md) controls placement, routes, and backups.
See [the hello app](../examples/homelab/docker/hello) for a minimal example.
