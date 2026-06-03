# App Repository Structure

Homectl reads application definitions from a separate Git repository configured
with `HOMECTL_GIT_REPO`. The manager clones it into `HOMECTL_REPO_DIR` on first
startup and syncs it with `git pull --ff-only`.

## Required Layout

Each app must live in its own directory under `docker/`:

```text
homelab/
├── docker/
│   ├── adguard/
│   │   ├── docker-compose.yml
│   │   └── homectl-config.json
│   ├── paperless/
│   │   ├── docker-compose.yml
│   │   └── homectl-config.json
│   └── vaultwarden/
│       ├── docker-compose.yml
│       └── homectl-config.json
└── scripts/
    └── decrypt-secrets
```

The app ID is the directory name under `docker/`. For example,
`docker/paperless/docker-compose.yml` becomes app ID `paperless`.

## Compose File Naming

Homectl expects this exact file name:

```text
docker/<app-id>/docker-compose.yml
```

Other Compose names such as `compose.yml`, `compose.yaml`, or
`docker-compose.yaml` are not discovered.

## Optional Homectl Config

Each app may include:

```text
docker/<app-id>/homectl-config.json
```

This file is optional. See [Homectl Config](./homectl-config.md) for supported
fields and examples.

If it is absent, Homectl uses default behavior:

- The app is eligible to run on any connected agent.
- Backups are enabled.
- The default backup interval is used.
- The app is not global.
- Route generation uses the app ID and reported Compose port.

## Default Desired State

On discovery, Homectl currently marks these apps as desired running by default:

- `traefik`
- `adguard`

Other apps are discovered as desired stopped unless persisted manager state has a
different value from the UI/API.

## Secrets Hook

If the repository contains an executable script at:

```text
scripts/decrypt-secrets
```

the manager runs it after a successful git sync. This hook is optional and
repository-specific.
