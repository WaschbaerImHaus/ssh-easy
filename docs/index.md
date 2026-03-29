---
layout: default
title: Home
---

<div class="hero">

<h1>ssh-easy</h1>
<p style="font-size:1.05rem; color:#7a9aac; max-width:520px; margin-bottom:0.5rem;">
  A terminal UI for managing SSH connections, tunnels and remote shells — on Linux and Windows.
</p>

<div class="hero-terminal">
  <div class="terminal-titlebar">
    <span class="dot dot-r"></span>
    <span class="dot dot-y"></span>
    <span class="dot dot-g"></span>
    <span class="terminal-title">ssh-easy — build 32</span>
  </div>
  <div class="terminal-body">
    <span class="t-prompt">❯</span> <span class="t-cmd">ssh-easy</span><br>
    <span class="t-out"></span>
    <span class="t-out">  <span class="t-conn">● Webserver</span>       <span class="t-sep">│</span> <span class="t-port">:8080</span></span>
    <span class="t-out">    Homelab            <span class="t-sep">│</span> <span class="t-port">:3306  :5432</span></span>
    <span class="t-out">    Dev-VM             <span class="t-sep">│</span></span>
    <span class="t-out"></span>
    <span class="t-out">  <span style="color:#3d5566">─────────────────────────────────</span></span>
    <span class="t-out">  n:neu  e:bearb.  d:lösch  Enter:verbinden  q:beenden</span>
    <span class="t-out"></span>
    <span class="t-prompt">❯</span> <span class="t-cursor"></span>
  </div>
</div>

<div style="display:flex; gap:0.75rem; flex-wrap:wrap; margin-bottom:2rem;">
  <a href="/ssh-easy/download/" style="display:inline-flex;align-items:center;gap:0.4rem;background:#00ff410f;border:1px solid #00ff4144;color:#00ff41;text-decoration:none;padding:0.5rem 1.1rem;border-radius:4px;font-family:'Share Tech Mono',monospace;font-size:0.9rem;transition:all 0.15s;" onmouseover="this.style.background='#00ff411a';this.style.boxShadow='0 0 16px #00ff4133'" onmouseout="this.style.background='#00ff410f';this.style.boxShadow='none'">↓ Download</a>
  <a href="https://github.com/WaschbaerImHaus/ssh-easy" target="_blank" rel="noopener" style="display:inline-flex;align-items:center;gap:0.4rem;background:#111820;border:1px solid #1e2d3d;color:#7a9aac;text-decoration:none;padding:0.5rem 1.1rem;border-radius:4px;font-family:'Share Tech Mono',monospace;font-size:0.9rem;transition:all 0.15s;" onmouseover="this.style.color='#c9d6df'" onmouseout="this.style.color='#7a9aac'">⌥ GitHub</a>
  <a href="/ssh-easy/changelog/" style="display:inline-flex;align-items:center;gap:0.4rem;background:#111820;border:1px solid #1e2d3d;color:#7a9aac;text-decoration:none;padding:0.5rem 1.1rem;border-radius:4px;font-family:'Share Tech Mono',monospace;font-size:0.9rem;transition:all 0.15s;" onmouseover="this.style.color='#c9d6df'" onmouseout="this.style.color='#7a9aac'">§ Changelog</a>
</div>

</div>

## Features

<div class="features-grid">

<div class="feature-card">
<span class="feature-icon">⚡ Auto-Auth</span>
<h4>Zero-config authentication</h4>
<p>Tries SSH agent → all keys in <code>~/.ssh/</code> → password. Remembers which key worked.</p>
</div>

<div class="feature-card">
<span class="feature-icon">⇄ Tunnels</span>
<h4>Local port forwarding</h4>
<p>Define tunnel ports per connection. Conflict detection when two connections share a port.</p>
</div>

<div class="feature-card">
<span class="feature-icon">▸ Terminal</span>
<h4>Interactive shell</h4>
<p>Open a full PTY session on any connected host with <code>t</code>. Resize-aware.</p>
</div>

<div class="feature-card">
<span class="feature-icon">↺ Reconnect</span>
<h4>Auto-reconnect</h4>
<p>Up to 5 automatic reconnect attempts on connection loss, with SSH keepalive every 30 s.</p>
</div>

<div class="feature-card">
<span class="feature-icon">🔑 Key deploy</span>
<h4>Passwordless future logins</h4>
<p>After a password login, ssh-easy generates an Ed25519 key and deploys it automatically.</p>
</div>

<div class="feature-card">
<span class="feature-icon">✦ 41 langs</span>
<h4>Multilingual UI</h4>
<p>Interface available in 41 languages — selected automatically based on system locale.</p>
</div>

</div>

---

## Key Bindings

### Connection List

| Key | Action |
|-----|--------|
| `n` | Add new connection |
| `e` | Edit selected connection |
| `d` | Delete selected connection |
| `Enter` | Connect and open status view |
| `j` / `k` or `↑` / `↓` | Navigate |
| `q` / `Ctrl+C` | Quit |

### Status View

| Key | Action |
|-----|--------|
| `t` | Open interactive remote shell |
| `r` | Remove deployed key from server + locally, reset to password auth |
| `x` | Disconnect and return to list |
| `q` | Return to list (stay connected) |

---

## Authentication

Authentication runs fully automatically — no manual selection needed:

1. **SSH agent** — checked first if a running agent is detected
2. **SSH keys** — all keys in `~/.ssh/` are tried individually; the one that works is cached
3. **Password** — fallback; prompts once, never stored on disk

After a successful password login, ssh-easy offers to deploy an Ed25519 key so future logins are passwordless. The key can be removed later with `r` in the status view.

---

## Tunnels

Tunnel ports are specified as a comma-separated list when adding or editing a connection:

```
3306,8080,5432
```

Each port becomes a local-to-remote forward bound to `127.0.0.1`:

```
localhost:3306  →  remote:3306
localhost:8080  →  remote:8080   ⚡ Port in use by: Homelab
localhost:5432  →  remote:5432
```

If another active connection is already using a port, the status view shows which connection is occupying it.

---

## Configuration

Connections are stored in JSON — no manual editing required:

| Platform | Path |
|----------|------|
| Linux / macOS | `~/.ssh-easy/connections.json` |
| Windows | `%USERPROFILE%\.ssh-easy\connections.json` |

Auth cache: `~/.ssh-easy/auth-cache.json`
Log file: `~/.ssh-easy/ssh-easy.log`

Passwords are **never** written to disk.

---

## Security

- Tunnels bind exclusively to `127.0.0.1`
- Host keys verified against `~/.ssh/known_hosts` — no `InsecureIgnoreHostKey`
- Unknown hosts require explicit confirmation before connecting
- Changed host keys are rejected with a MITM warning
- Config file permissions enforced at `0600`
- Atomic config writes (write-to-temp + rename)

---

## Building from Source

Requirements: **Go 1.24+**, `CGO_ENABLED=0`

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

The resulting binaries have **no runtime dependencies** — no glibc, no CGO.
