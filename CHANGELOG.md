# Changelog – ssh-easy

All notable changes to this project are documented here.
Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [v0.33] – Build 33
- **Fixed** app icon not visible in Windows taskbar for non-admin users: added `SetCurrentProcessExplicitAppUserModelID("KurtIngwer.ssh-easy")` so Windows assigns the taskbar button to the app instead of the console host (conhost.exe / Windows Terminal)

## [v0.29] – Build 29
- **Fixed** connection list alignment: selected and unselected entries now share the same indent, eliminating the 2-character offset
- **Removed** red `-` dash for disconnected entries; only connected entries show the green `●`

## [v0.28] – Build 28
- **Fixed** double-keypress required after closing SSH terminal: the trailing `\r` from `exit↵` was left in the kernel tty buffer and consumed by Bubbletea as the first keypress. Fixed by flushing stdin after `session.Wait()` (Unix: `SetNonblock` + drain; Windows: `FlushConsoleInputBuffer`)
- **Changed** status view: ESC no longer disconnects, only `x` does
- **Removed** manual key-gen (`g`) from main list – SSH key is generated automatically on first password login

## [v0.27] – Build 27
- **Changed** connection list now shows only `● Name  [ports]` – host, user and auth type removed (still visible in status view)
- **Added** duplicate name prevention: saving a connection with an already-used name is rejected (case-insensitive; own name excluded when editing)

## [v0.26] – Build 26
- **Added** green `●` indicator for connected entries in the connection list
- **Changed** after a successful connect the app now jumps directly to the status view instead of the list, so `t` for terminal is immediately available

## [v0.25] – Build 25
- **Fixed** auth cache failure counter: network errors (I/O timeout, connection refused, etc.) no longer increment the counter – only genuine SSH authentication rejections do

## [v0.24] – Build 24
- **Added** persistent auth method cache (`~/.ssh-easy/auth-cache.json`): remembers which SSH key or agent worked per connection and tries it first on the next connect
- **Added** `r` key in status view to manually clear the cached auth key for a connection
- After 2 consecutive auth failures the cached entry is deleted and full rediscovery runs

## [v0.23] – Build 23
- **Changed** connection list is now sorted alphabetically by name (case-insensitive)
- **Changed** ESC in status view now disconnects (same as `x`); `q` returns without disconnecting
- Updated help text in all 41 languages

## [v0.22] – Build 22
- **Fixed** Windows taskbar icon: the `.syso` resource files set the Explorer icon but the console host showed its own icon in the taskbar. Fixed by calling `GetConsoleWindow` + `LoadImage` + `SendMessage(WM_SETICON)` at startup via `init()`

## [v0.21] – Build 21
- **Fixed** `removeKnownHost`: hostname is now normalised (lowercase, explicit port stripped) before comparison with `known_hosts` entries

## [v0.20] – Build 20
- **Fixed** host-key change error message: German umlauts (Ä, ü, ö) were garbled due to encoding issue

## [v0.19] – Build 19
- **Changed** all binaries built with `CGO_ENABLED=0` for fully static linking – fixes startup failure on older Linux distributions (e.g. Linux Mint: `CXXABI_1.3.15 not found`)

## [v0.18] – Build 18
- **Fixed** goroutine leak caused by `time.Sleep` in keepalive/reconnect loops; replaced with select-based cancellable waits
- Security improvements

## [v0.17] – Build 17
- **Fixed** language list layout issues
- **Changed** English is now the default language on first start

## [v0.16] – Build 16
- **Added** scrollable language selection window (8 visible entries with arrow navigation)

## [v0.15] – Build 15
- **Added** i18n expanded to 41 languages; language selection list now two-column

## [v0.14] – Build 14
- **Fixed** missing translations for placeholders and tunnel labels

## [v0.13] – Build 13
- **Added** multilingual UI (i18n) with 14 languages and language selection on first start

## [v0.12] – Build 12
- **Fixed** terminal resize: window enlargement is now forwarded to remote programs (mc, vim, …)
- **Fixed** Windows app icon: ICO now uses BMP format instead of PNG
- **Added** Windows installer automatically uninstalls previous version
- **Fixed** ANSI/VT colour rendering in SSH terminal on Windows

## [v0.11] – Build 11 *(Windows hotfix)*
- **Fixed** ANSI/VT escape sequences shown as raw text in Windows SSH terminal

## [v0.10] – Build 10
- **Added** Windows NSIS installer with Start Menu entry and optional desktop icon
- **Fixed** Windows Alt-Tab causing SSH terminal to lose raw mode; terminal resize now works on Windows
- **Added** app icon embedded in Windows executable via `goversioninfo`/`.syso`

## [v0.9] – Build 9
- **Changed** port field now defaults to `22`
- **Changed** Enter key navigates between form fields (no longer requires Tab)

## [v0.8] – Build 8
- **Added** interactive remote terminal session via `t` key (PTY over existing SSH connection)
- **Improved** `known_hosts` cleanup on host-key mismatch

## [v0.7] – Build 7
- **Removed** auth-type and key-path fields from connection form; authentication is fully automatic

## [v0.6] – Build 6
- **Fixed** German umlauts in all UI texts and code comments

## [v0.5] – Build 5
- **Added** host-key-changed warning dialog: alerts the user when a server's host key has changed (possible MITM), with option to accept or abort

## [v0.4] – Build 4
- **Added** automatic SSH key authentication: tries SSH agent and all unencrypted keys from `~/.ssh/` without asking the user
- **Added** auto key-deploy: on first password login a new SSH key is generated and added to `authorized_keys` on the server

## [v0.3] – Build 3 *(initial public release)*
- SSH connection manager with terminal UI (Bubbletea)
- Save, edit, delete connections (stored in `~/.ssh-easy/connections.json`)
- Local port forwarding tunnels per connection
- Password and SSH-key authentication
- `known_hosts` verification (no `InsecureIgnoreHostKey`)
- Tunnels bound to `127.0.0.1` only
- Cross-platform: Linux x64/ARM64, Windows x64/ARM64
