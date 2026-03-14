# ssh-easy

A simple SSH connection manager with Terminal UI (TUI) for managing and establishing SSH connections with local port forwarding tunnels.

> Also available in: [Deutsch](README.de.md)

## Features

- Save and manage SSH connections (name, host, port, user)
- Password, SSH key, and SSH agent authentication
- Local port forwarding (localhost:port -> remote:port)
- SSH key generation (Ed25519) directly from the TUI
- Automatic reconnect on connection loss (max. 5 attempts)
- SSH keepalive (every 30 seconds)
- Colored TUI with status display
- Logging to `~/.ssh-easy/ssh-easy.log`
- Cross-platform: Linux (x86/ARM), Windows (x86/ARM)

## Installation

Pre-compiled binaries are available in the `build/` directory:

| File | Platform |
|------|----------|
| `ssh-easy` | Linux x86_64 |
| `ssh-easy-linux-arm64` | Linux ARM64 |
| `ssh-easy-windows-amd64.exe` | Windows x86_64 |
| `ssh-easy-windows-arm64.exe` | Windows ARM64 |

### Windows

Run the installer `ssh-easy-setup.exe` — it will create a Start Menu entry and optionally a Desktop shortcut.

### Linux

```bash
chmod +x build/ssh-easy
./build/ssh-easy
```

## Usage

Start the program:
```bash
./build/ssh-easy
```

### Key Bindings

| Key | Action |
|-----|--------|
| `n` | Add new connection |
| `e` | Edit connection |
| `d` | Delete connection |
| `Enter` | Connect / show status |
| `x` | Disconnect |
| `g` | Generate SSH key (Ed25519) |
| `j/k` or arrow keys | Navigate |
| `Tab` | Next form field |
| `Esc` | Back / Cancel |
| `q` / `Ctrl+C` | Quit |

### Adding a Connection

1. Press `n`
2. Fill in the fields:
   - **Name**: Display name (e.g. "Webserver")
   - **Host**: IP address or hostname
   - **Port**: SSH port (default: 22)
   - **User**: SSH username
   - **Auth**: `password`, `key`, or `agent`
   - **Key path**: Path to SSH key file (only for `key`)
   - **Tunnel ports**: Comma-separated port list (e.g. `3306,8080,5432`)
3. Press `Enter` to save

### Authentication

- **password**: Password is requested on each connect (never stored)
- **key**: SSH key with optional passphrase
- **agent**: Uses SSH agent (keys must be loaded beforehand)

### Tunnels

Tunnel ports are specified as a comma-separated list. Each port is set up as a local port forward:

```
localhost:3306 -> remote:3306
localhost:8080 -> remote:8080
```

### Generating an SSH Key

1. Press `g`
2. Enter a **file path** (e.g. `~/.ssh/id_ed25519_myserver`)
3. Optionally enter a **passphrase**
4. Press `Enter` to generate
5. The public key is displayed and can be added to `~/.ssh/authorized_keys` on the target server

### Auto-Reconnect

On connection loss, ssh-easy automatically attempts to reconnect (max. 5 attempts, 3 seconds apart).

## Configuration

Connections are stored at:
- Linux: `~/.ssh-easy/connections.json`
- Windows: `%USERPROFILE%\.ssh-easy\connections.json`

Log file:
- `~/.ssh-easy/ssh-easy.log`

Passwords are **never** stored and must be entered on each connection attempt.

## Security

- Passwords are only held in memory at runtime
- Tunnels bind exclusively to `127.0.0.1`
- Host keys are verified against `~/.ssh/known_hosts` (no InsecureIgnoreHostKey)
- Unknown hosts are added to known_hosts after confirmation
- Changed host keys are rejected with a MITM warning
- Configuration file permissions are set to `0600`
- Atomic config file writes

## Building from Source

Requirements: Go 1.23+

```bash
cd src
go build -o ../build/ssh-easy .
```

Cross-compilation:
```bash
GOOS=linux  GOARCH=arm64 go build -o ../build/ssh-easy-linux-arm64 .
GOOS=windows GOARCH=amd64 go build -o ../build/ssh-easy-windows-amd64.exe .
GOOS=windows GOARCH=arm64 go build -o ../build/ssh-easy-windows-arm64.exe .
```

## License

MIT
