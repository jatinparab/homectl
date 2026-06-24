# Homectl Config

`homectl-config.json` is an optional per-app metadata file stored next to an
app's Compose file:

```text
docker/<app-id>/homectl-config.json
```

Unknown fields are ignored by the current parser. The supported fields are:

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `exposed_subdomains` | object | unset | Maps public subdomain names to container-facing ports for generated routing config. Values may be numbers or numeric strings. |
| `exclude_nodes` | array of strings | `[]` | Agent names that should not run this app. |
| `backup_sensitive` | boolean | `false` | Marks the app as sensitive to backup interruptions, so backup scheduling can defer around recent activity. |
| `backup_enabled` | boolean | `true` | Enables or disables backup, restore, and prune work for this app. |
| `backup_interval` | duration string | global default | Overrides the default backup freshness interval. Uses Go duration syntax such as `3h`, `72h`, or `168h`. |
| `is_global` | boolean | `false` | Runs the app on every eligible connected agent instead of assigning it to one agent. |

## Examples

Expose one service on a custom subdomain:

```json
{
  "exposed_subdomains": {
    "photos": 2283
  },
  "exclude_nodes": ["agent-hostname"]
}
```

Expose several services from a stack:

```json
{
  "exposed_subdomains": {
    "torrent": "8081",
    "sonarr": "8989",
    "radarr": "7878"
  },
  "exclude_nodes": ["agent-hostname"]
}
```

Run an app on every eligible node:

```json
{
  "is_global": true,
  "backup_interval": "3h"
}
```

Disable backups for infrastructure apps:

```json
{
  "backup_enabled": false,
  "is_global": true
}
```

## Routing Behavior

If `exposed_subdomains` is set, each key becomes a route under
`HOMECTL_DOMAIN_SUFFIX`. For example, with:

```json
{
  "exposed_subdomains": {
    "files": "3923"
  }
}
```

and `HOMECTL_DOMAIN_SUFFIX=example.net`, Homectl generates routes for:

```text
files.example.net
files.internal.example.net
```

If `exposed_subdomains` is omitted, Homectl uses the app ID as the subdomain and
the port reported from the Compose file.

## Placement Behavior

`exclude_nodes` and `is_global` affect scheduling:

- `exclude_nodes` removes listed agents from the candidate set.
- `is_global: true` means the app should run on every eligible connected agent.
- `is_global: false` means the manager assigns the app to one eligible agent.
