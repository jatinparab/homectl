# Self-Hosted Access With Caddy, Authentik, and WireGuard

Homectl can use the existing Authentik instance at
`https://auth.example.net` as an optional identity layer for public routes.
This does not add or manage a production Authentik deployment.

Protected app flow:

```text
Internet -> Caddy public edge -> Authentik forward-auth -> WireGuard -> local service
```

App with its own auth:

```text
Internet -> Caddy public edge -> WireGuard -> local service with its own auth
```

## Config

Use these values in the manager environment or manager-stored shared node config:

```env
ROOT_DOMAIN=example.net
HOME_WG_IP=<wireguard-ip-of-home-machine>
AUTHENTIK_CORE_URL=https://auth.example.net
AUTHENTIK_OUTPOST_URL=https://auth.example.net
```

`HOME_WG_IP` is intentionally configurable. Do not hardcode the home peer IP into
route definitions.

## Auth Modes

Every public route has an `auth_mode`:

| Mode | Behavior |
| --- | --- |
| `authentik` | Caddy runs Authentik forward-auth before proxying. Use this for apps with weak or no native auth. |
| `upstream` | Caddy proxies directly because the app has its own login/auth layer. |
| `public` | Caddy proxies directly because the route is intentionally unauthenticated. |

The generated Caddyfile marks each route with `# Auth mode: ...`. Authentik is
not global; only routes with `auth_mode: authentik` import the
`authentik_forward_auth` snippet.

## Examples

Protected admin app:

```json
{
  "host": "grafana.example.net",
  "mode": "homelab",
  "auth_mode": "authentik",
  "enabled": true
}
```

Generated Caddy route shape:

```caddyfile
# Auth mode: authentik
grafana.example.net {
    route {
        import authentik_forward_auth
        reverse_proxy https://<gateway-wg-ip> {
            header_up Host {host}
            header_up X-Forwarded-Host {host}
            transport http {
                tls_server_name grafana.example.net
            }
        }
    }
}
```

Self-authenticated app:

```json
{
  "host": "jellyfin.example.net",
  "mode": "homelab",
  "auth_mode": "upstream",
  "enabled": true
}
```

Public route:

```json
{
  "host": "status.example.net",
  "mode": "custom",
  "auth_mode": "public",
  "custom_target": "http://homectl-localhost:3001",
  "enabled": true
}
```

## Authentik Route

`auth.example.net` must not be protected by `authentik_forward_auth`, or login
can break. If this repo manages that Caddy route, keep it as `auth_mode:
upstream`. If it is managed outside Homectl, leave it external and do not add a
duplicate route here.

## Adding Routes

To add an Authentik-protected app, create a public route with `auth_mode:
authentik`. Admin services such as Grafana, Portainer, Cockpit, and Uptime Kuma
admin views should usually use this mode and be limited to `homelab-admins`.

To add an app with its own auth, create a public route with `auth_mode:
upstream`. Jellyfin is a typical example.

To add a deliberately open route, create a public route with `auth_mode: public`.
Use this only for endpoints meant for unauthenticated access.

App repository metadata may also document the intended mode:

```json
{
  "exposed_subdomains": {
    "grafana": 3000
  },
  "auth_mode": "authentik"
}
```

## Authentik Manual Setup

For each app using `authentik` mode:

1. Go to `https://auth.example.net`.
2. Create an Authentik Application.
3. Create a Proxy Provider.
4. Select forward-auth mode.
5. Set external host to the public app URL, for example `https://grafana.example.net`.
6. Attach the provider to the application.
7. Bind access to the appropriate group.
8. Ensure the Authentik outpost includes this provider.
9. Test in a private browser session.

Recommended groups:

| Group | Use |
| --- | --- |
| `homelab-users` | Normal protected apps. |
| `homelab-admins` | Admin panels and infrastructure apps. |

## Safety Notes

- Only expose ports `80` and `443` on the public VPS/edge, plus required
  WireGuard UDP where Homectl needs it.
- Local services should bind only to LAN/WireGuard, never directly to the
  internet.
- Enable MFA in Authentik.
- Keep a break-glass access method such as direct WireGuard or SSH.
- Do not protect Authentik with Authentik.
- Do not assume every app needs Authentik; apps with strong native auth can use
  `upstream`, and double auth may break or annoy users.
- Apps with weak or no native auth should use `authentik`.
- Routes intended for unauthenticated access can use `public`.
- Admin panels should use Authentik and ideally also source-network or VPN
  restrictions where practical.
- Be careful with upstream apps that trust `X-Authentik-*` headers.
- Only Caddy should be able to inject trusted identity headers. The generated
  Authentik snippet strips incoming `X-Authentik-*` headers before forwarding.
- Keep Authentik updated.
