---
layout: default
title: Download
permalink: /download/
---

# Download

<p style="color:#7a9aac; margin-bottom:2rem;">
  All releases on <a href="https://github.com/WaschbaerImHaus/ssh-easy/releases">GitHub Releases</a> · No runtime dependencies · Fully static binaries
</p>

## Latest Release

<a href="https://github.com/WaschbaerImHaus/ssh-easy/releases/latest"
   style="display:inline-flex;align-items:center;gap:0.5rem;background:#00ff410f;border:1px solid #00ff4144;color:#00ff41;text-decoration:none;padding:0.65rem 1.4rem;border-radius:4px;font-family:'Share Tech Mono',monospace;font-size:1rem;margin-bottom:2rem;transition:all 0.15s;"
   onmouseover="this.style.background='#00ff411a';this.style.boxShadow='0 0 20px #00ff4133'"
   onmouseout="this.style.background='#00ff410f';this.style.boxShadow='none'">
  ↓ Download latest version ↗
</a>

<div class="dl-grid">
  <a class="dl-card" href="https://github.com/WaschbaerImHaus/ssh-easy/releases/latest/download/ssh-easy">
    <span class="dl-platform">Linux x86_64</span>
    <span class="dl-filename">ssh-easy</span>
    <span class="dl-type">ELF binary · no installer needed</span>
  </a>
  <a class="dl-card" href="https://github.com/WaschbaerImHaus/ssh-easy/releases/latest/download/ssh-easy-linux-arm64">
    <span class="dl-platform">Linux ARM64</span>
    <span class="dl-filename">ssh-easy-linux-arm64</span>
    <span class="dl-type">Raspberry Pi, Apple Silicon, etc.</span>
  </a>
  <a class="dl-card" href="https://github.com/WaschbaerImHaus/ssh-easy/releases/latest/download/ssh-easy-setup-amd64.exe">
    <span class="dl-platform">Windows x86_64</span>
    <span class="dl-filename">ssh-easy-setup-amd64.exe</span>
    <span class="dl-type">NSIS Installer · Start Menu entry</span>
  </a>
  <a class="dl-card" href="https://github.com/WaschbaerImHaus/ssh-easy/releases/latest/download/ssh-easy-setup-arm64.exe">
    <span class="dl-platform">Windows ARM64</span>
    <span class="dl-filename">ssh-easy-setup-arm64.exe</span>
    <span class="dl-type">NSIS Installer · Start Menu entry</span>
  </a>
</div>

---

## Installation

### Linux

```bash
wget https://github.com/WaschbaerImHaus/ssh-easy/releases/latest/download/ssh-easy
chmod +x ssh-easy
./ssh-easy
```

Optional — move to PATH so it runs from anywhere:

```bash
sudo mv ssh-easy /usr/local/bin/
```

### Windows

1. Download **`ssh-easy-setup-amd64.exe`**
2. Run the installer (no admin required for user-install)
3. Open **ssh-easy** from the Start Menu

The installer silently removes any previous version and creates an optional Desktop shortcut.

---

## Requirements

| | |
|---|---|
| Runtime dependencies | **None** — fully static binary (`CGO_ENABLED=0`) |
| Linux | Any modern distribution (kernel 3.2+) |
| Windows | Windows 10 / Windows 11 |
| Disk space | ~10 MB |

---

## All Releases

Browse all previous versions on the [GitHub Releases page](https://github.com/WaschbaerImHaus/ssh-easy/releases) — each release includes all 6 binaries.
