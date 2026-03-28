---
layout: page
title: Download
permalink: /download/
---

# Download

All releases are available on the [GitHub Releases page](https://github.com/WaschbaerImHaus/ssh-easy/releases).

## Latest Release

**[Download latest version](https://github.com/WaschbaerImHaus/ssh-easy/releases/latest)**

| File | Platform | Notes |
|------|----------|-------|
| `ssh-easy` | Linux x86_64 | |
| `ssh-easy-linux-arm64` | Linux ARM64 | Raspberry Pi, etc. |
| `ssh-easy-setup-amd64.exe` | Windows x86_64 | Installer with Start Menu entry |
| `ssh-easy-setup-arm64.exe` | Windows ARM64 | Installer with Start Menu entry |
| `ssh-easy-windows-amd64.exe` | Windows x86_64 | Portable (no installer) |
| `ssh-easy-windows-arm64.exe` | Windows ARM64 | Portable (no installer) |

## Installation

### Linux

```bash
# Download
wget https://github.com/WaschbaerImHaus/ssh-easy/releases/latest/download/ssh-easy

# Make executable
chmod +x ssh-easy

# Run
./ssh-easy
```

Optional – move to PATH:
```bash
sudo mv ssh-easy /usr/local/bin/
```

### Windows

1. Download `ssh-easy-setup-amd64.exe`
2. Run the installer
3. Find **ssh-easy** in your Start Menu

The installer creates a Start Menu entry and offers an optional Desktop shortcut. It silently removes any previous installation first.

## Requirements

- No runtime dependencies — fully static binary (`CGO_ENABLED=0`)
- Linux: any modern distribution (kernel 3.2+)
- Windows: Windows 10 / Windows 11

## Building from Source

See [GitHub repository](https://github.com/WaschbaerImHaus/ssh-easy) for build instructions.
