# acme-ui

[中文说明](README-cn.md)

`acme-ui` is a temporary Linux Web console for operating `acme.sh` on remote servers. It helps configure Cloudflare DNS validation, issue certificates, install certificates for nginx or HAProxy, and run common certificate maintenance actions from a compact browser UI.

`acme-ui` does not replace `acme.sh`. Account data, DNS API credentials, renewal configuration, issued certificates, and install hooks remain managed by `acme.sh`.

## Install And Run

Start `acme-ui` on a Linux server:

```bash
curl -fsSL https://github.com/luodaoyi/acme-ui/releases/latest/download/acme-ui.sh | bash
```

If certificate files must be written under `/etc/nginx`, `/etc/haproxy`, or another root-owned path, run it with root privileges:

```bash
curl -fsSL https://github.com/luodaoyi/acme-ui/releases/latest/download/acme-ui.sh | sudo bash
```

The launcher downloads the Linux binary into a temporary directory, runs it in the foreground, and removes the downloaded binary when the process exits.

On startup, the terminal prints the listening address and a one-time `MasterKey`:

```text
acme-ui is running

Listen:    0.0.0.0:43127
Open:      http://<server-ip>:43127
MasterKey: ...
Mode:      foreground, press Ctrl+C to exit
```

Open the printed URL in a browser and enter the `MasterKey`.

## Features

- Foreground-only process; no daemon or background service is installed.
- Binds to `0.0.0.0` with a random port by default.
- Generates a per-run `MasterKey` for Web login.
- Keeps Web sessions in memory only.
- Installs `acme.sh` from the official `https://get.acme.sh` script when requested.
- Requires confirmation before running the `acme.sh` installer.
- Passes Cloudflare DNS API values only to the current `acme.sh` child process.
- Lets `acme.sh` persist DNS credentials in its own config for future renewals.
- Supports certificate issue, install, renew, revoke, and remove actions.
- Supports explicit uninstall of installed certificate files without recursive deletion.
- Supports nginx certificate install.
- Supports HAProxy combined PEM generation.
- Streams task logs in the browser and redacts sensitive values.

## acme.sh Installation

If `acme.sh` is not found, the environment page shows an install action. The action downloads and runs the official installer:

```bash
curl -fsSL https://get.acme.sh -o <tmp>/get.acme.sh
sh <tmp>/get.acme.sh
```

An email address can be provided in the UI. If provided, it is passed as:

```bash
sh <tmp>/get.acme.sh email=admin@example.com
```

The installer is an intentional `acme.sh` operation. It may create `~/.acme.sh`, shell aliases, and a daily cron job according to the official installer behavior.

## Certificate Flow

For Cloudflare DNS validation, enter the required Cloudflare values in the Web UI. `acme-ui` injects them into the `acme.sh` process environment for the selected command.

After a successful DNS issue operation, `acme.sh` stores reusable DNS credentials in its own configuration, such as:

```text
~/.acme.sh/account.conf
```

This allows future `acme.sh --cron` renewals to work without `acme-ui` running.

## Persistence Boundary

`acme-ui` itself is temporary. The launcher removes the downloaded binary after exit, and the Web session state is memory-only.

The following artifacts are intentionally persistent because they belong to `acme.sh` or the selected certificate installation:

- `~/.acme.sh/account.conf`
- `~/.acme.sh/<domain>/`
- Shell alias and daily cron job created by the official `acme.sh` installer
- Domain renewal configuration and `reloadcmd` saved by `acme.sh`
- nginx or HAProxy certificate target files

## Security Model

- The Web UI requires the generated `MasterKey`.
- State-changing API calls require an authenticated session and CSRF token.
- `acme.sh` actions are mapped to fixed commands; arbitrary shell input is not exposed.
- nginx and HAProxy reload commands are generated from controlled options.
- Certificate file uninstall removes only explicit absolute file paths and refuses directories.
- Secret values are redacted from task logs.

## Build From Source

```bash
go test ./...
go build ./cmd/acme-ui
```

Run on Linux:

```bash
./acme-ui
```

For local development on a non-Linux machine:

```bash
ACME_UI_ALLOW_NON_LINUX=1 go run ./cmd/acme-ui
```

## Release

Pushing a `v*` tag triggers the GitHub Actions release workflow:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Release assets:

- `acme-ui-linux-amd64`
- `acme-ui-linux-arm64`
- `acme-ui.sh`
- `checksums.txt`
