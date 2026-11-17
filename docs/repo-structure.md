# App repository structure

Point `HOMECTL_GIT_REPO` at a Git repository containing a `docker/` directory.
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
