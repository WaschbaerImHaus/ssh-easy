# ssh-easy

A simple SSH connection manager with Terminal UI (TUI) for managing and establishing SSH connections with local port forwarding tunnels.

> Also available in: [Deutsch](README.de.md)

## Features

- Save and manage SSH connections (name, host, port, user)
- Automatic authentication: SSH agent, all keys in `~/.ssh/`, password fallback
- Local port forwarding (localhost:port -> remote:port)
- SSH key generation (Ed25519) directly from the TUI
- Automatic reconnect on connection loss (max. 5 attempts)
- SSH keepalive (every 30 seconds)
- Colored TUI with status display
- Copy from remote programs: OSC 52 clipboard sequences (e.g. Claude Code, vim, tmux) are written to the local clipboard — also on the Windows console, which does not support OSC 52 itself
- Groups: a colon in the name groups connections (`Office:Server1`, `Office:NAS` → group *Office*), groups can be expanded and collapsed
- Scrollable list: more than 15 entries are shown in a scrolling window (`↑ +N` / `↓ +N` show the hidden rows)
- Fast failure for unreachable hosts: 5-second connect timeout, no repeated waiting per SSH key
- DNS fallback: if the network's DNS server fails, names are resolved via 1.1.1.1, 1.0.0.1, 8.8.8.8 and 8.8.4.4 (rotating)
- Optional auto-update from GitHub releases (off by default, toggle with `u`); every release is signed (Ed25519)
- Export the connection list — all connections or a selection, SSH keys are never exported (`E`)
- Remembers the window size (Windows console, unless maximized/fullscreen; Windows Terminal remembers it itself)
- 41 UI languages
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
| `→` / `Enter` on a group | Expand group |
| `←` | Collapse group (also from a group member) |
| `PgUp` / `PgDn` / `Home` / `End` | Page through long lists |
| `u` | Toggle auto-update (shown in the last menu line) |
| `E` | Export connections (all or a selection) |
| `Tab` | Next form field |
| `Ctrl+V` / `Shift+Insert` | Paste clipboard into form field and password prompt (also on Windows) |
| `Ctrl+Up` / `Ctrl+Down` | Increase / decrease font size (Windows, persisted; Ctrl+MouseWheel works too) |
| `Esc` | Back / Cancel |
| `q` / `Ctrl+C` / `Alt+F4` | Quit (Alt+F4 also works inside an SSH session) |

### Adding a Connection

1. Press `n`
2. Fill in the fields:
   - **Name**: Display name (e.g. "Webserver")
   - **Host**: IP address or hostname
   - **Port**: SSH port (default: 22)
   - **User**: SSH username
   - **Tunnel ports**: Comma-separated port list (e.g. `3306,8080,5432`)
3. Press `Enter` to save

### Authentication

Authentication is fully automatic — no need to choose a method:

1. **SSH agent** — checked first if a running agent is available
2. **SSH keys** — all keys in `~/.ssh/` are tried automatically
3. **Password** — requested as a fallback if the above fail (never stored)

After a successful password login, ssh-easy can automatically generate and deploy an Ed25519 key so future logins are passwordless.

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

### Groups

A colon in the connection name is a group separator. `Office:Server1` and `Office:NAS` appear as group **Office** with the entries *Server1* and *NAS*. Only the first colon splits (`Office:Rack:1` → group *Office*, entry *Rack:1*). Text is required before and after the colon. A green dot on a group means at least one member is connected. A newly created connection is selected right away (its group is expanded).

### Tunnel display

The list shows the first two tunnel ports, further ports only as a count: `[Tunnel: 3306,8080 +3 more]`. After connecting, ports whose tunnel could not be opened (e.g. port already in use) are shown in red — in the list and in the status view. Details are in the log file.

### Exporting connections

Press `E` (Shift+E) in the list. All connections are selected at first; groups are expanded:

- `Space` toggles the entry under the cursor — on a group it toggles all members (`[~]` = partly selected)
- `a` selects all / none
- `Tab` switches to the file name field (default: `ssh-easy-export-<date>-<time>.json` in your home folder; relative names and `~` refer to the home folder)
- `Enter` exports, `Esc` cancels

The file has the format of `connections.json`, so it can be used as configuration on another computer. It is written with permissions `0600` and never overwrites an existing file. **SSH keys are not part of the export** — neither the key files nor their path; connections that used a key are exported as password connections (automatic authentication still tries the SSH agent and all keys in `~/.ssh/` first).

### Password prompt

If automatic authentication fails, ssh-easy asks for the password and shows the host and — on the line below — the user name, so you know which password is meant.

### Auto-Update

Off by default. Press `u` in the main menu to switch it on (the setting is shown in the last menu line and saved). When on, ssh-easy checks the latest GitHub release at startup and downloads the matching file in the background; it is installed when you quit:

- Linux, or Windows with write access to the program folder: the binary is replaced
- Windows installed in *Program Files*: the setup runs silently with administrator rights (Windows asks via UAC); the desktop shortcut is kept

Every release contains a checksum list (`ssh-easy-SHA256SUMS`) signed with Ed25519 (`ssh-easy-SHA256SUMS.sig`). ssh-easy verifies the signature with the public key built into the program and only installs a file whose SHA-256 matches the signed list — so even someone with access to the GitHub account cannot push a manipulated update. Size and file format are checked as well — right after the download and again just before installation. Unsigned releases are rejected.

### Auto-Reconnect

On connection loss, ssh-easy automatically attempts to reconnect (max. 5 attempts, 3 seconds apart).

## Configuration

Connections are stored at:
- Linux: `~/.ssh-easy/connections.json`
- Windows: `%USERPROFILE%\.ssh-easy\connections.json`

Log file:
- `~/.ssh-easy/ssh-easy.log`

Passwords are **never** stored and must be entered on each connection attempt.

Further settings in `connections.json`: `language`, `font_size`, `window_cols` / `window_rows` (last window size, Windows only) and `auto_update`.

## Security

- Passwords are only held in memory at runtime
- Tunnels bind exclusively to `127.0.0.1`
- Host keys are verified against `~/.ssh/known_hosts` (no InsecureIgnoreHostKey)
- Unknown hosts are added to known_hosts after confirmation
- Changed host keys are rejected with a MITM warning
- Configuration file permissions are set to `0600`
- Atomic config file writes
- OSC 52 clipboard: remote programs may only **write** to the local clipboard (max. 1 MiB); read requests are never answered
- Auto-update: downloads only from this repository's GitHub releases; the SHA-256 must match the Ed25519-signed checksum list of exactly this release (public key built in, private key only on the build machine), re-checked before installing, plus size and format check
- Export: no SSH keys or key paths, file permissions `0600`, never overwrites or follows a symlink
- DNS fallback servers are only used when the network's DNS does not answer and never for local names (`.local`, `.lan`, names without a dot, …); an unknown host key reached via fallback DNS is not trusted automatically
- Shift+Insert paste uses bracketed paste when the remote shell enables it (`ESC[?2004h`): pasted text is wrapped in `ESC[200~ … ESC[201~` and embedded markers are removed, so multi-line clipboard content is not executed line by line

## Building from Source

Requirements: Go 1.26+ (release builds use the latest Go version)

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
