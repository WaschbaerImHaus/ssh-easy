---
layout: default
title: Changelog
permalink: /changelog/
---

# Changelog

<p style="color:#7a9aac; margin-bottom:2rem;">
  All notable changes — format follows <a href="https://keepachangelog.com/en/1.0.0/">Keep a Changelog</a>.
</p>

## [v0.32] – Build 32
- <span class="badge badge-red">Fixed</span> Port conflict check order: the conflict indicator (`⚡ Port in use by: ...`) now takes priority over the raw `listen tcp...` error that the SSH manager records when binding fails

## [v0.31] – Build 31
- <span class="badge badge-green">Added</span> Port conflict detection in status view: when a local tunnel port is occupied by another active connection, shows `⚡ Port in use by: [Name]`

## [v0.30] – Build 30
- <span class="badge badge-green">Added</span> `r` key fully removes a deployed SSH key: removes from server's `authorized_keys`, deletes local key files, resets to password auth, disconnects
- <span class="badge badge-amber">Changed</span> Help text updated to `r:Schlüssel entfernen` / `r:Remove key`

## [v0.29] – Build 29
- <span class="badge badge-red">Fixed</span> Connection list alignment: selected and unselected entries now share the same indent
- <span class="badge badge-amber">Removed</span> Red `-` dash for disconnected entries; only connected entries show `●`

## [v0.28] – Build 28
- <span class="badge badge-red">Fixed</span> Double-keypress required after closing SSH terminal: trailing `\r` from `exit↵` flushed from tty buffer after `session.Wait()` (Unix: `SetNonblock` + drain; Windows: `FlushConsoleInputBuffer`)
- <span class="badge badge-amber">Changed</span> Status view: ESC no longer disconnects, only `x` does
- <span class="badge badge-amber">Removed</span> Manual key-gen (`g`) — SSH key is generated automatically on first password login

## [v0.27] – Build 27
- <span class="badge badge-amber">Changed</span> Connection list shows only `● Name [ports]` — host/user/auth removed (still in status view)
- <span class="badge badge-green">Added</span> Duplicate name prevention (case-insensitive)

## [v0.26] – Build 26
- <span class="badge badge-green">Added</span> Green `●` indicator for connected entries
- <span class="badge badge-amber">Changed</span> After connecting, jumps directly to status view so `t` for terminal is immediately available

## [v0.25] – Build 25
- <span class="badge badge-red">Fixed</span> Auth cache failure counter: network errors (I/O timeout, connection refused) no longer increment the counter — only genuine SSH auth rejections do

## [v0.24] – Build 24
- <span class="badge badge-green">Added</span> Persistent auth method cache (`~/.ssh-easy/auth-cache.json`): remembers which SSH key worked per connection
- <span class="badge badge-green">Added</span> After 2 consecutive auth failures the cache entry is cleared and full rediscovery runs

## [v0.23] – Build 23
- <span class="badge badge-amber">Changed</span> Connection list sorted alphabetically by name (case-insensitive)
- <span class="badge badge-amber">Changed</span> ESC in status view disconnects (same as `x`); `q` returns without disconnecting

## [v0.22] – Build 22
- <span class="badge badge-red">Fixed</span> Windows taskbar icon: `.syso` sets Explorer icon; taskbar icon now also set via `GetConsoleWindow` + `LoadImage` + `SendMessage(WM_SETICON)` at `init()`

## [v0.21] – Build 21
- <span class="badge badge-red">Fixed</span> `removeKnownHost`: hostname normalised (lowercase, explicit port stripped) before `known_hosts` comparison

## [v0.20] – Build 20
- <span class="badge badge-red">Fixed</span> Host-key change error message: German umlauts (Ä, ü, ö) garbled due to encoding issue

## [v0.19] – Build 19
- <span class="badge badge-amber">Changed</span> All binaries built with `CGO_ENABLED=0` — fixes startup failure on older Linux distributions (`CXXABI_1.3.15 not found`)

## [v0.18] – Build 18
- <span class="badge badge-red">Fixed</span> Goroutine leak in keepalive/reconnect loops: `time.Sleep` replaced with select-based cancellable waits

## [v0.17] – Build 17
- <span class="badge badge-red">Fixed</span> Language list layout issues
- <span class="badge badge-amber">Changed</span> English is the default language on first start

## [v0.16] – Build 16
- <span class="badge badge-green">Added</span> Scrollable language selection (8 visible entries with arrow navigation)

## [v0.15] – Build 15
- <span class="badge badge-green">Added</span> i18n expanded to 41 languages; two-column layout

## [v0.14] – Build 14
- <span class="badge badge-red">Fixed</span> Missing translations for placeholders and tunnel labels

## [v0.13] – Build 13
- <span class="badge badge-green">Added</span> Multilingual UI (i18n) with 14 languages and language selection on first start

## [v0.12] – Build 12
- <span class="badge badge-red">Fixed</span> Terminal resize: window enlargement forwarded to remote programs (mc, vim, …)
- <span class="badge badge-red">Fixed</span> Windows app icon: ICO now uses BMP instead of PNG
- <span class="badge badge-green">Added</span> Windows installer automatically uninstalls previous version
- <span class="badge badge-red">Fixed</span> ANSI/VT colour rendering in SSH terminal on Windows

## [v0.11] – Build 11 *(Windows hotfix)*
- <span class="badge badge-red">Fixed</span> ANSI/VT escape sequences shown as raw text in Windows SSH terminal

## [v0.10] – Build 10
- <span class="badge badge-green">Added</span> Windows NSIS installer with Start Menu entry and optional desktop icon
- <span class="badge badge-red">Fixed</span> Windows Alt-Tab causing SSH terminal to lose raw mode
- <span class="badge badge-green">Added</span> App icon embedded in Windows executable via `goversioninfo`/`.syso`

## [v0.9] – Build 9
- <span class="badge badge-amber">Changed</span> Port field defaults to `22`
- <span class="badge badge-amber">Changed</span> Enter navigates between form fields

## [v0.8] – Build 8
- <span class="badge badge-green">Added</span> Interactive remote terminal session via `t` key (PTY over existing SSH connection)

## [v0.7] – Build 7
- <span class="badge badge-amber">Removed</span> Auth-type and key-path fields from connection form; authentication is fully automatic

## [v0.6] – Build 6
- <span class="badge badge-red">Fixed</span> German umlauts in all UI texts and code comments

## [v0.5] – Build 5
- <span class="badge badge-green">Added</span> Host-key-changed warning dialog with option to accept or abort

## [v0.4] – Build 4
- <span class="badge badge-green">Added</span> Automatic SSH key authentication (agent + all unencrypted keys in `~/.ssh/`)
- <span class="badge badge-green">Added</span> Auto key-deploy on first password login

## [v0.3] – Build 3 *(initial public release)*
- SSH connection manager with Bubbletea TUI
- Save, edit, delete connections (JSON storage)
- Local port forwarding tunnels per connection
- Password and SSH-key authentication
- `known_hosts` verification
- Tunnels bound to `127.0.0.1`
- Cross-platform: Linux x64/ARM64, Windows x64/ARM64
