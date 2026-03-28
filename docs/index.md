---
layout: home
title: ssh-easy
---

# ssh-easy

A simple SSH connection manager with **Terminal UI (TUI)** for Linux and Windows.
Save connections, auto-authenticate, and forward ports — all from your terminal.

---

## Features

- Save and manage SSH connections (name, host, port, user)
- **Automatic authentication**: SSH agent → all keys in `~/.ssh/` → password fallback
- **Auth caching**: remembers which key worked per connection
- Local port forwarding (`localhost:port → remote:port`)
- Interactive remote shell (`t` key)
- SSH key auto-deployment after password login (passwordless future logins)
- Automatic reconnect on connection loss (max. 5 attempts)
- SSH keepalive (every 30 seconds)
- Port conflict detection across active connections
- Colored TUI with status display
- 41 UI languages
- Cross-platform: Linux x86/ARM, Windows x86/ARM

---

## Download

Latest release: **[Releases on GitHub](https://github.com/WaschbaerImHaus/ssh-easy/releases/latest)**

| File | Platform |
|------|----------|
| `ssh-easy` | Linux x86_64 |
| `ssh-easy-linux-arm64` | Linux ARM64 |
| `ssh-easy-setup-amd64.exe` | Windows x86_64 (Installer) |
| `ssh-easy-setup-arm64.exe` | Windows ARM64 (Installer) |

### Linux

```bash
chmod +x ssh-easy
./ssh-easy
```

### Windows

Run `ssh-easy-setup-amd64.exe` — creates a Start Menu entry and optional Desktop shortcut.

---

## Usage

### Key Bindings — Connection List

| Key | Action |
|-----|--------|
| `n` | Add new connection |
| `e` | Edit connection |
| `d` | Delete connection |
| `Enter` | Connect / show status |
| `j/k` or arrow keys | Navigate |
| `q` / `Ctrl+C` | Quit |

### Key Bindings — Status View

| Key | Action |
|-----|--------|
| `t` | Open interactive remote shell |
| `r` | Remove deployed SSH key (from server + locally), reset to password auth |
| `x` | Disconnect and return to list |
| `q` | Return to list (stay connected) |

---

## Authentication

Authentication is fully automatic — no manual selection needed:

1. **SSH agent** — checked first if a running agent is available
2. **SSH keys** — all keys in `~/.ssh/` are tried; the working key is remembered
3. **Password** — fallback; after success, ssh-easy can deploy an Ed25519 key for future logins

Passwords are **never stored**. The auth cache is saved to `~/.ssh-easy/auth-cache.json`.

---

## Tunnels

Define tunnel ports when adding/editing a connection (comma-separated).
Each port becomes a local-to-remote forward:

```
localhost:3306  →  remote:3306
localhost:8080  →  remote:8080
```

If a port is already in use by another active connection, the status view shows which connection is occupying it.

---

## Configuration

Connections are stored at:
- Linux/macOS: `~/.ssh-easy/connections.json`
- Windows: `%USERPROFILE%\.ssh-easy\connections.json`

Log file: `~/.ssh-easy/ssh-easy.log`

---

## Security

- Passwords only held in memory during auth
- Tunnels bind exclusively to `127.0.0.1`
- Host keys verified against `~/.ssh/known_hosts`
- Unknown hosts require explicit confirmation
- Changed host keys are rejected with a MITM warning
- Config file permissions: `0600`

---

## Building from Source

Requirements: Go 1.24+, `CGO_ENABLED=0`

```bash
cd src
CGO_ENABLED=0 go build -o ../build/ssh-easy .
```

Cross-compilation:
```bash
CGO_ENABLED=0 GOOS=linux  GOARCH=arm64  go build -o ../build/ssh-easy-linux-arm64 .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o ../build/ssh-easy-windows-amd64.exe .
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -o ../build/ssh-easy-windows-arm64.exe .
```

---

## License

MIT — see [LICENSE](https://github.com/WaschbaerImHaus/ssh-easy/blob/main/LICENSE)
